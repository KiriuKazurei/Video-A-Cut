"""Phase-7 isolated acceptance: raw recording -> ingest -> fixed profile -> export.

Real Go control plane over HTTP, a real ``python -m vac_worker`` ingester
process over MCP, real ffmpeg, SAPI and the Node exporter. Content models are
a local mock server; only synthetic recordings generated here are used.
Human acceptance is never recorded. Evidence stays in .run-data/phase7-e2e-<id>.
"""
from __future__ import annotations

import base64, hashlib, io, json, os, shutil, socket, sqlite3, subprocess, sys, threading, time, uuid, zipfile
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import Request, urlopen

repo = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(repo / "workers/python"))
sys.path.insert(0, str(Path(__file__).resolve().parent))
from phase7_fixtures import SCENE_CUTS, SCENE_DURATION, plain, scenes  # noqa: E402
from vac_worker.mcp import Client  # noqa: E402
from vac_worker.preparation import local_capability  # noqa: E402
from vac_worker.worker import Config, process_task  # noqa: E402

ASSET = "phase7_synthetic"
CANCEL_ASSET = "phase7_cancel"
INGEST_FILES = {"source-map.json", "ingest-provenance.json", "ingest-timeline.json"}


def free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


class Http:
    def __init__(self, url: str, secrets: list[str]):
        self.url, self.secrets, self.bodies = url, secrets, []

    def call(self, path: str, body=None, method=None, expect=None):
        req = Request(self.url + path, data=json.dumps(body).encode() if body is not None else None,
                      headers={"Content-Type": "application/json"}, method=method)
        try:
            with urlopen(req, timeout=30) as r:
                raw, status = r.read(), r.status
        except HTTPError as error:
            raw, status = error.read(), error.code
        text = raw.decode("utf-8", errors="replace")
        self.bodies.append(text)
        if expect is not None:
            if status != expect:
                raise RuntimeError(f"{req.method} {path}: expected HTTP {expect}, got {status}: {text}")
        elif status >= 400:
            raise RuntimeError(f"{req.method} {path}: HTTP {status}: {text}")
        return json.loads(text) if text else None

    def raw(self, path: str) -> tuple[bytes, str]:
        with urlopen(self.url + path, timeout=30) as r:
            return r.read(), r.headers.get("Content-Type", "")

    def leaks(self) -> list[str]:
        found = []
        for secret in self.secrets:
            variants = {secret, secret.replace("\\", "/"), secret.replace("\\", "\\\\")}
            if any(v.lower() in b.lower() for b in self.bodies for v in variants):
                found.append(secret)
        return found


def main(recovery=False, sealed=False, browser_fixtures=False, revised=False) -> int:
    work = repo / ".run-data" / (("phase7-recovery-" if recovery else "phase7-e2e-") + uuid.uuid4().hex[:8])
    delivery, recordings = work / "delivery", work / "recordings"
    delivery.mkdir(parents=True)
    recordings.mkdir()
    server = plane = ingester = None
    logs = []
    fault_proxy = None
    counters = {"verified_images": 0}

    class Mock(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_POST(self):
            data = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            if self.path == "/vision":
                out = []
                for clip in data["clips"]:
                    for frame in clip["frames"]:
                        assert hashlib.sha256(base64.b64decode(frame["image_base64"])).hexdigest() == frame["sha256"]
                        counters["verified_images"] += 1
                    out.append({"clip_index": clip["clip_index"], "label": "测试画面", "confidence": 0.99, "sequence_rank": clip["clip_index"],
                                "evidence_frames": [f["sha256"] for f in clip["frames"]]})
                response = {"scenes": out}
            else:
                response = {"model_version": "local-mock-v1", "narrations": [
                    {"clip_index": c["clip_index"], "text": "测试画面。", "start": c["timeline_in"] + 0.2, "end": c["timeline_out"] - 0.2,
                     "needs_review": True, "source_scene_label": c["scene_label"]} for c in data["clips"]]}
            raw = json.dumps(response, ensure_ascii=False).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(raw)

    try:
        ffmpeg = shutil.which("ffmpeg")
        assert ffmpeg and shutil.which("ffprobe"), "ffmpeg/ffprobe must be on PATH"
        recording = scenes(ffmpeg, recordings / "session 01.mkv")
        spare = plain(ffmpeg, recordings / "spare.mp4", "30", seconds=60.0 if browser_fixtures else 8.0)
        originals = {p.name: sha(p) for p in (recording, spare)}

        server = ThreadingHTTPServer(("127.0.0.1", 0), Mock)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        mock = f"http://127.0.0.1:{server.server_port}"
        port = free_port()
        url = f"http://127.0.0.1:{port}"
        http = Http(url, [str(recordings), str(delivery), str(work)])

        def write(path: Path, data) -> None:
            path.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")

        def snapshot(name):
            with sqlite3.connect(work/'control.db') as original:
                with sqlite3.connect(work/f'browser-{name}.db') as copy:
                    original.backup(copy)

        roles = ("ingester", "recognizer", "narrator", "exporter")
        tokens = {role: uuid.uuid4().hex + uuid.uuid4().hex for role in roles}
        roots = [{"root_id": "rec", "name": "合成录像", "path": str(recordings)}]
        write(work / "agents.json", {"agents": [{"agent_id": r, "role": r, "token_sha256": hashlib.sha256(t.encode()).hexdigest()} for r, t in tokens.items()]})
        write(work / "control.json", {"http_addr": f"127.0.0.1:{port}", "delivery_root": str(delivery), "web_root": str(repo / "webui/dist"),
                                      "mcp_agents_file": str(work / "agents.json"), "lease_seconds": 60, "ingest_roots": roots})
        env = os.environ.copy()
        env.update(GOCACHE=str(repo / ".run-data/go-cache"), TEMP=str(work), TMP=str(work))
        binary = work / "control-plane.exe"
        subprocess.run(["go", "build", "-o", str(binary), "."], cwd=repo / "control-plane", env=env, check=True, timeout=180)
        log = (work / "control.log").open("w", encoding="utf-8")
        logs.append(log)
        plane = subprocess.Popen([str(binary), "-config", str(work / "control.json"), "-db", str(work / "control.db")], stdout=log, stderr=log)
        for _ in range(100):
            if plane.poll() is not None:
                raise RuntimeError("control plane startup failed; see control.log")
            try:
                http.call("/api/assets")
                break
            except OSError:
                time.sleep(0.1)
        else:
            raise RuntimeError("control plane startup timed out")

        if recovery:
            from recovery_faults import RecoveryFaultProxy
            fault_proxy = RecoveryFaultProxy(url)
        # Ingester worker: a real process with its own token in the environment only.
        write(work / "ingester.json", {"role": "ingester", "mcp_url": fault_proxy.url if recovery else url + "/mcp", "delivery_root": str(delivery), "token_env": "VAC_E2E_INGESTER_TOKEN",
                                       "poll_interval_ms": 500, "heartbeat_interval_ms": 2000, "ingest_roots": roots})
        ing_env = env.copy()
        ing_env["VAC_E2E_INGESTER_TOKEN"] = tokens["ingester"]
        ing_log = (work / "ingester.log").open("w", encoding="utf-8")
        logs.append(ing_log)
        ingester = subprocess.Popen([sys.executable, "-m", "vac_worker", "--config", str(work / "ingester.json")], cwd=repo / "workers/python",
                                    env=ing_env, stdout=ing_log, stderr=ing_log, creationflags=getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0))
        for _ in range(120):
            info = http.call("/api/ingest-roots")
            if info["ingesters"]:
                break
            if ingester.poll() is not None:
                raise RuntimeError("ingester exited; see ingester.log")
            time.sleep(0.5)
        else:
            raise RuntimeError("ingester capability never reported")
        assert info["configured"] and info["roots"] == [{"root_id": "rec", "name": "合成录像"}], info

        def wait_run(run_id: str, want: str, timeout: float = 300) -> dict:
            deadline = time.monotonic() + timeout
            while time.monotonic() < deadline:
                view = http.call(f"/api/ingest-runs/{run_id}")
                run = view["run"]
                if f"{run['state']}/{run['stage']}" == want:
                    return view
                if run["state"] in ("failed", "cancelled"):
                    raise RuntimeError(f"ingest run {run['state']}: {run.get('error_code')} {run.get('error_message')}")
                if ingester.poll() is not None:
                    raise RuntimeError("ingester exited; see ingester.log")
                time.sleep(0.5)
            raise RuntimeError(f"timed out waiting for {want}")

        # Register: path traversal is refused, a replay returns the same source.
        http.call("/api/recordings", {"asset_id": ASSET, "root_id": "rec", "relative_path": "../control.json", "idempotency_key": "bad"}, expect=400)
        reg_body = {"asset_id": ASSET, "root_id": "rec", "relative_path": "session 01.mkv", "idempotency_key": "reg-1"}
        reg = http.call("/api/recordings", reg_body, expect=201)
        assert http.call("/api/recordings", reg_body, expect=201)["source"]["source_id"] == reg["source"]["source_id"]
        src = reg["source"]
        start_body = {"source_id": src["source_id"], "expected_source_version": reg["source_version"], "idempotency_key": "start-1"}
        http.call(f"/api/assets/{ASSET}/ingest-runs", start_body, expect=403)
        http.call(f"/api/assets/{ASSET}", {"agent_visible": True, "allowed_agents": list(roles[1:]) + ["ingester"]}, "PATCH")
        run_id = http.call(f"/api/assets/{ASSET}/ingest-runs", start_body, expect=202)["run"]["run_id"]

        view = wait_run(run_id, "awaiting_review/probe")
        if browser_fixtures: snapshot('probe')
        probe = http.call(f"/api/ingest-runs/{run_id}/files/probe")
        assert probe == view["probe"]
        video = next(s for s in probe["streams"] if s["type"] == "video")
        audios = [s for s in probe["streams"] if s["type"] == "audio"]
        assert len(audios) == 2 and "multiple_audio_streams" in probe["limitations"], probe
        assert view["source"]["has_snapshot"] and view["source"]["sha256"] == originals[recording.name]

        plan = {"expected_version": view["run"]["version"], "video_stream_index": video["index"], "game_audio_stream_index": audios[0]["index"],
                "source_range_us": [0, probe["duration_us"]],
                "segmentation": {"method": "scene_change", "threshold": 0.3, "min_segment_us": 2_000_000, "max_segment_us": 60_000_000},
                "idempotency_key": "plan-1"}
        if recovery:
            fault_proxy.armed_scan = True
        http.call(f"/api/ingest-runs/{run_id}/analysis-plans", plan, expect=202)
        if recovery:
            assert fault_proxy.scan_event.wait(60), "scan checkpoint fault never reached"
            former = dict(fault_proxy.scan_args)
            ingester.kill(); ingester.wait(timeout=10)
            if sealed: fault_proxy.armed_sealed = True
            ingester = subprocess.Popen([sys.executable, "-m", "vac_worker", "--config", str(work / "ingester.json")], cwd=repo / "workers/python", env=ing_env, stdout=ing_log, stderr=ing_log)
            if sealed:
                assert fault_proxy.sealed_event.wait(240), 'sealed package fault never reached'
                sealed_scope = dict(fault_proxy.sealed_args)
                old_receipt_path = delivery/sealed_scope['package_dir']/'worker-receipt.json'
                sealed_receipt = old_receipt_path.read_bytes()
                ingester.kill(); ingester.wait(timeout=10)
                ingester = subprocess.Popen([sys.executable, "-m", "vac_worker", "--config", str(work / "ingester.json")], cwd=repo / "workers/python", env=ing_env, stdout=ing_log, stderr=ing_log)
        view = wait_run(run_id, "awaiting_review/segment_review")
        if browser_fixtures: snapshot('segment')
        if recovery:
            segment_task = next(t for t in view['tasks'] if t['task']['type']=='segment')
            assert segment_task["binding"]["execution_seq"] >= 2, view
            assert "reusing 1 verified scan chunks" in (work / "ingester.log").read_text(encoding="utf-8"), "completed local scan was redone"
            committed = fault_proxy.dropped_commit
            assert committed is not None, "committed-response fault missing"
            query_args={k:committed[k] for k in ('task_id','runtime_instance_id','execution_id','generation','request_id')}
            actual=Client(url+'/mcp',tokens['ingester']).call('get_execution_result_status',query_args)
            assert actual['status']=='committed',actual
            if sealed:
                assert old_receipt_path.read_bytes()==sealed_receipt, 'historical receipt changed'
                assert view['execution']['execution_id'] != sealed_scope['execution_id']
                assert 'recovered sealed package' in (work/'ingester.log').read_text(encoding='utf-8')
            write(work/'recovery-evidence.json',{'scan_interrupted_before_registration':True,'former_execution_id':former['execution_id'],
                'new_execution_id':view['execution']['execution_id'],'local_completed_scan_reused':True,
                'committed_request_id':committed['request_id'],'actual_result_status':actual['status'],
                'sealed_cross_execution_adopted':sealed,'historical_receipt_unchanged':sealed,
                'requests_without_secrets':fault_proxy.requests})
        page = http.call(f"/api/ingest-runs/{run_id}/segments?limit=2&offset=0")
        rest = http.call(f"/api/ingest-runs/{run_id}/segments?limit=50&offset=2")
        cands = page["items"] + rest["items"]
        assert page["total"] == len(cands) == len(SCENE_CUTS) + 1, cands
        for cand, cut in zip(cands[1:], SCENE_CUTS):
            assert abs(cand["start_us"] / 1e6 - cut) <= 0.05, (cand, cut)
        thumb, ctype = http.raw(f"/api/ingest-runs/{run_id}/files/{cands[0]['thumbnail_key']}")
        assert ctype == "image/jpeg" and thumb[:2] == b"\xff\xd8", ctype
        http.call(f"/api/ingest-runs/{run_id}/files/..%2Fsegments", expect=400)

        # The fixture starts at 1.5 s: the range must end at the video's last frame, not the container end.
        assert probe["duration_us"] == video["duration_us"] and cands[-1]["end_us"] == probe["duration_us"], (probe, cands[-1])
        assert abs(probe["duration_us"] / 1e6 - SCENE_DURATION) <= 0.05, probe["duration_us"]
        # Human selection: three segments including the tail one, the first trimmed by hand.
        chosen = [dict(segment_id=c["segment_id"], start_us=c["start_us"], end_us=c["end_us"]) for c in cands[:2] + cands[-1:]]
        chosen[0]["start_us"] += 500_000
        if '--manual-edits' in sys.argv:
            # Same coverage after merging the first adjacent pair and splitting at a new boundary.
            assert chosen[0]['end_us'] == chosen[1]['start_us']
            split_us = (chosen[0]['start_us'] + chosen[1]['end_us']) // 2
            chosen[0].update(segment_id='manual_left', end_us=split_us)
            chosen[1].update(segment_id='manual_right', start_us=split_us)
        sel = http.call(f"/api/ingest-runs/{run_id}/selection-revisions", {"expected_version": view["run"]["version"], "base_plan_revision": view["run"]["analysis_revision"],
                        "selected_segments": chosen, "output": {"fps": 30, "sample_rate": 48000}, "idempotency_key": "sel-1"}, expect=201)

        voices = [v for v in local_capability(Config(url + "/mcp", tokens["narrator"], delivery, role="narrator", profile_sha256="a" * 64,
                                                     processing_profile={"narration": {"adapter": "builtin"}}))["tts_voices"] if "Huihui" in v]
        assert voices, "Chinese SAPI voice unavailable; no test-tone fallback"
        os.environ["VAC_E2E_MODEL_TOKEN"] = "local-fixture-placeholder"
        profile = {"schema_version": 1, "profile_id": "phase7_fixture", "revision": 1, "name": "七阶段 mock 预设", "content_mode": "configured",
                   "vision": {"adapter": "vision", "endpoint": mock + "/vision", "model": "local-mock-v1", "token_env": "VAC_E2E_MODEL_TOKEN"},
                   "narration": {"adapter": "narration", "endpoint": mock + "/narration", "model": "local-mock-v1", "token_env": "VAC_E2E_MODEL_TOKEN"},
                   "sampling": {"max_frames": 6, "max_bytes": 12582912, "timeout_seconds": 60}, "tts_voice": voices[0], "export_target": "premiere"}
        http.call("/api/processing-profiles", {"profile": profile, "expected_revision": 0, "idempotency_key": "profile-1"})
        profile_sha = http.call("/api/processing-profiles/phase7_fixture/revisions/1")["profile_sha256"]
        http.call(f"/api/ingest-runs/{run_id}/prepare", {"expected_version": sel["version"], "plan_revision": sel["selection_revision"],
                  "profile_id": "phase7_fixture", "profile_revision": 1, "idempotency_key": "prep-1"}, expect=202)
        view = wait_run(run_id, "ready/ready", timeout=600)
        assert view["asset"]["input_kind"] == "edl_package" and view["asset"]["ingest_run_id"] == run_id, view["asset"]
        media = [k for k in view["files"] if k.startswith("media_")]
        assert sorted(media) == sorted("media_" + c["segment_id"] for c in chosen), media
        clip, ctype = http.raw(f"/api/ingest-runs/{run_id}/files/{media[0]}")
        assert ctype == "video/mp4" and len(clip) > 1000, ctype

        # Cancel path on a second asset: the in-flight task is invalidated.
        reg2 = http.call("/api/recordings", {"asset_id": CANCEL_ASSET, "root_id": "rec", "relative_path": "spare.mp4", "idempotency_key": "reg-2"}, expect=201)
        http.call(f"/api/assets/{CANCEL_ASSET}", {"agent_visible": True, "allowed_agents": ["ingester"]}, "PATCH")
        run2 = http.call(f"/api/assets/{CANCEL_ASSET}/ingest-runs", {"source_id": reg2["source"]["source_id"], "expected_source_version": reg2["source_version"],
                         "idempotency_key": "start-2"}, expect=202)["run"]
        cancelled = http.call(f"/api/ingest-runs/{run2['run_id']}/cancel", {"expected_version": run2["version"], "reason": "verification cancel"})
        assert cancelled["state"] == "cancelled", cancelled
        time.sleep(3)
        view2 = http.call(f"/api/ingest-runs/{run2['run_id']}")
        assert view2["run"]["state"] == "cancelled" and all(t["binding"]["invalidated"] or t["task"]["status"] != "succeeded" for t in view2["tasks"]), view2

        # Phase-6 fixed-profile workflow on the ingested package.
        clients = {role: Client(url + "/mcp", tokens[role]) for role in roles[1:]}

        def make_cfg(role):
            return Config(url + "/mcp", tokens[role], delivery, role=role, profile_sha256=profile_sha, processing_profile=profile,
                          content_provider="vision" if role == "recognizer" else "narration",
                          content_provider_config={"endpoint": mock + ("/vision" if role == "recognizer" else "/narration"), "allow_external": True,
                                                   "token_env": "VAC_E2E_MODEL_TOKEN", "model": "local-mock-v1"})
        for role in ("recognizer", "narrator"):
            clients[role].call("heartbeat", {"capability": local_capability(make_cfg(role))})
        node_env = env.copy()
        node_env["VAC_E2E_EXPORT_TOKEN"] = tokens["exporter"]
        export_cfg = work / "export.json"
        write(export_cfg, {"mcp_url": url + "/mcp", "delivery_root": str(delivery), "cli_path": str(repo / "workers/node/timeline-cli/src/cli.mjs"),
                           "adapters_path": str(repo / "workers/node/timeline-cli/adapters/index.mjs"), "token_env": "VAC_E2E_EXPORT_TOKEN", "profile_sha256": profile_sha})
        subprocess.run(["node", str(repo / "workers/node/export-worker/src/main.mjs"), "--config", str(export_cfg), "--once"], env=node_env, check=True,
                       capture_output=True, timeout=60)
        readiness = http.call(f"/api/assets/{ASSET}/prepared-preflight", {"profile_id": "phase7_fixture", "revision": 1})
        assert readiness["can_start"], readiness
        wf = http.call(f"/api/assets/{ASSET}/prepared-workflows", {"profile_id": "phase7_fixture", "revision": 1,
                       "expected_asset_version": readiness["expected_asset_version"], "idempotency_key": "wf-1"})
        base = f"/api/workflows/{wf['run_id']}"

        def step(role):
            claim = clients[role].call("claim_task", {})
            assert claim["claimed"], claim
            cfg = make_cfg(role)
            clients[role].call("heartbeat", {"capability": local_capability(cfg)})
            result = process_task(clients[role], cfg, claim["task"], lambda _: None)
            assert result["outcome"] == "succeeded", result

        def review(key, pending_of, post):
            while True:
                rv = http.call(base + "/review")
                pending = pending_of(rv)
                if not pending:
                    return
                http.call(base + key, post(rv, pending[0]))

        step("recognizer")
        review("/scene-reviews", lambda rv: [s for s in rv["scenes"] if s.get("decision") != "confirmed"],
               lambda rv, s: {"expected_version": rv["run"]["version"], "revision_id": rv["revision"]["revision_id"], "scene_id": s["scene_id"], "decision": "confirmed"})
        step("recognizer")
        step("narrator")
        review("/narration-reviews", lambda rv: [n for n in rv["narration"] if not n.get("approved_hash")],
               lambda rv, n: {"expected_version": rv["run"]["version"], "revision_id": rv["revision"]["revision_id"], "narration_id": n["id"]})
        for _ in range(3):
            step("narrator")
        done = subprocess.run(["node", str(repo / "workers/node/export-worker/src/main.mjs"), "--config", str(export_cfg), "--once"], env=node_env,
                              capture_output=True, text=True, encoding="utf-8", timeout=180)
        (work / "export.log").write_text(done.stdout + done.stderr, encoding="utf-8")
        assert done.returncode == 0, done.stdout + done.stderr
        assert http.call(base)["run"]["status"] == "ready_for_acceptance"
        raw, _ = http.raw(base + "/delivery.zip")
        (work / "delivery.zip").write_bytes(raw)
        with zipfile.ZipFile(io.BytesIO(raw)) as archive:
            names = set(archive.namelist())
            assert INGEST_FILES <= names and {"edit.xml", "edl.json", "samples/evidence-manifest.json"} <= names, names
            timeline = json.loads(archive.read("ingest-timeline.json"))["segments"]
            assert {r["segment_id"] for r in timeline} == {c["segment_id"] for c in chosen}, timeline
            by_id = {c["segment_id"]: c for c in chosen}
            for row in timeline:
                assert by_id[row["segment_id"]]["start_us"] <= row["source_start_us"] < row["source_end_us"] <= by_id[row["segment_id"]]["end_us"], row
            edl = json.loads(archive.read("edl.json"))
            quota = {i: v["ingest"]["frame_quota"] for i, v in enumerate(edl["video"])}
            assert sum(quota.values()) <= profile["sampling"]["max_frames"], quota
            evidence = json.loads(archive.read("samples/evidence-manifest.json"))
            per_clip: dict[int, int] = {}
            for f in evidence["frames"]:
                assert hashlib.sha256(archive.read(f["path"])).hexdigest() == f["sha256"]
                per_clip[f["clip_index"]] = per_clip.get(f["clip_index"], 0) + 1
            assert all(n <= quota[i] for i, n in per_clip.items()), (per_clip, quota)
        assert http.call(base + "/acceptance") == [], "engineering verification must not record human acceptance"
        assert counters["verified_images"] > 0
        assert {p.name: sha(p) for p in (recording, spare)} == originals, "original recordings changed"
        leaks = http.leaks()
        assert not leaks, f"REST responses exposed local paths: {leaks}"

        revised_evidence = None
        if revised:
            first_run, first_workflow, first_zip_sha = run_id, wf['run_id'], hashlib.sha256(raw).hexdigest()
            run_id=http.call(f'/api/assets/{ASSET}/ingest-runs',dict(start_body,idempotency_key='start-revised'),expect=202)['run']['run_id']
            view=wait_run(run_id,'awaiting_review/probe')
            assert view['source']['sha256']==originals[recording.name]
            http.call(f'/api/ingest-runs/{run_id}/analysis-plans',dict(plan,expected_version=view['run']['version'],
                game_audio_stream_index=audios[1]['index'],idempotency_key='plan-revised'),expect=202)
            view=wait_run(run_id,'awaiting_review/segment_review')
            revised_cands=http.call(f'/api/ingest-runs/{run_id}/segments?limit=50')['items']
            choice=revised_cands[2]
            changed=[{'segment_id':choice['segment_id'],'start_us':choice['start_us']+500_000,'end_us':choice['end_us']-500_000}]
            selection=http.call(f'/api/ingest-runs/{run_id}/selection-revisions',{'expected_version':view['run']['version'],
                'base_plan_revision':view['run']['analysis_revision'],'selected_segments':changed,'output':{'fps':60,'sample_rate':48000},
                'idempotency_key':'sel-revised'},expect=201)
            http.call(f'/api/ingest-runs/{run_id}/prepare',{'expected_version':selection['version'],'plan_revision':selection['selection_revision'],
                'profile_id':'phase7_fixture','profile_revision':1,'idempotency_key':'prep-revised'},expect=202)
            wait_run(run_id,'ready/ready')
            for role in ('recognizer','narrator'):clients[role].call('heartbeat',{'capability':local_capability(make_cfg(role))})
            subprocess.run(['node',str(repo/'workers/node/export-worker/src/main.mjs'),'--config',str(export_cfg),'--once'],env=node_env,check=True,capture_output=True,timeout=60)
            readiness=http.call(f'/api/assets/{ASSET}/prepared-preflight',{'profile_id':'phase7_fixture','revision':1});assert readiness['can_start'],readiness
            wf=http.call(f'/api/assets/{ASSET}/prepared-workflows',{'profile_id':'phase7_fixture','revision':1,
                'expected_asset_version':readiness['expected_asset_version'],'idempotency_key':'wf-revised'})
            assert wf['run_id']!=first_workflow and run_id!=first_run
            base=f"/api/workflows/{wf['run_id']}"
            step('recognizer')
            review('/scene-reviews',lambda rv:[s for s in rv['scenes'] if s.get('decision')!='confirmed'],
                lambda rv,s:{'expected_version':rv['run']['version'],'revision_id':rv['revision']['revision_id'],'scene_id':s['scene_id'],'decision':'confirmed'})
            step('recognizer');step('narrator')
            review('/narration-reviews',lambda rv:[n for n in rv['narration'] if not n.get('approved_hash')],
                lambda rv,n:{'expected_version':rv['run']['version'],'revision_id':rv['revision']['revision_id'],'narration_id':n['id']})
            for _ in range(3):step('narrator')
            done=subprocess.run(['node',str(repo/'workers/node/export-worker/src/main.mjs'),'--config',str(export_cfg),'--once'],env=node_env,
                capture_output=True,text=True,encoding='utf-8',timeout=180)
            assert done.returncode==0,done.stdout+done.stderr
            new_raw,_=http.raw(base+'/delivery.zip');(work/'revised-delivery.zip').write_bytes(new_raw)
            with zipfile.ZipFile(io.BytesIO(new_raw)) as archive:
                revised_map=json.loads(archive.read('ingest-timeline.json'))
                provenance=json.loads(archive.read('ingest-provenance.json'))
                source_map=json.loads(archive.read('source-map.json'))
                assert provenance['run_id']==run_id and source_map['game_audio_stream_index']==audios[1]['index']
                assert len(revised_map['segments'])==1 and json.loads(archive.read('edl.json'))['timeline']['fps']==60
                row=revised_map['segments'][0]
                assert row['source_start_us']==changed[0]['start_us'] and row['source_end_us']<=changed[0]['end_us']
            old_raw,_=http.raw(f'/api/workflows/{first_workflow}/delivery.zip')
            assert hashlib.sha256(old_raw).hexdigest()==first_zip_sha,'new selection changed historical delivery'
            assert {p.name:sha(p) for p in (recording,spare)}==originals
            revised_evidence={'first_ingest_run':first_run,'second_ingest_run':run_id,'first_workflow':first_workflow,'second_workflow':wf['run_id'],
                'changed_selection':changed,'new_fps':60,'new_audio_stream':audios[1]['index'],'old_delivery_unchanged':True,
                'new_delivery_sha256':hashlib.sha256(new_raw).hexdigest()}
            write(work/'revision-evidence.json',revised_evidence)

        if browser_fixtures:
            # Save actual backend states for isolated browser interaction.
            snapshot('ready')
            bad = recordings/'broken.mkv'
            bad.write_bytes(b'not a media container')
            body={'asset_id':'phase7_failure','root_id':'rec','relative_path':bad.name,'idempotency_key':'bad-reg'}
            failreg=http.call('/api/recordings',body,expect=201)
            http.call('/api/assets/phase7_failure',{'agent_visible':True,'allowed_agents':['ingester']},'PATCH')
            failrun=http.call('/api/assets/phase7_failure/ingest-runs',{'source_id':failreg['source']['source_id'],
                'expected_source_version':failreg['source_version'],'idempotency_key':'bad-start'},expect=202)['run']
            deadline=time.monotonic()+60
            while time.monotonic()<deadline:
                failed=http.call('/api/ingest-runs/'+failrun['run_id'])
                if failed['run']['state']=='failed': break
                time.sleep(.3)
            else: raise AssertionError('damaged media did not enter failed state')
            assert failed['run']['stage']=='probe' and not failed['source']['has_snapshot']
            snapshot('failed')

            # 60 real candidates make the UI cross its 50-row page boundary.
            paging=http.call(f'/api/assets/{CANCEL_ASSET}/ingest-runs',{'source_id':reg2['source']['source_id'],
                'expected_source_version':reg2['source_version'],'idempotency_key':'paging-start'},expect=202)['run']
            paging_view=wait_run(paging['run_id'],'awaiting_review/probe')
            pvid=next(s for s in paging_view['probe']['streams'] if s['type']=='video')
            paud=next(s for s in paging_view['probe']['streams'] if s['type']=='audio')
            http.call(f"/api/ingest-runs/{paging['run_id']}/analysis-plans",{'expected_version':paging_view['run']['version'],
                'video_stream_index':pvid['index'],'game_audio_stream_index':paud['index'],'source_range_us':[0,60_000_000],
                'segmentation':{'method':'scene_change','threshold':.95,'min_segment_us':500_000,'max_segment_us':1_000_000},
                'idempotency_key':'paging-plan'},expect=202)
            paging_view=wait_run(paging['run_id'],'awaiting_review/segment_review')
            assert paging_view['segments']['count']==60,paging_view
            snapshot('paging')

        write(work / "report.json", {
            "engineering": "passed", "content_provider": "mock", "human_acceptance": "pending",
            "recovery_e2e": recovery, "sealed_recovery": sealed, "browser_fixture_snapshots": browser_fixtures,
            "revised_ingest_workflow":revised_evidence,
            "ingest_run_id": run_id, "workflow_run_id": wf["run_id"], "cancel_run_id": run2["run_id"],
            "candidates": [{k: c[k] for k in ("segment_id", "start_us", "end_us", "reason")} for c in cands],
            "selected": chosen, "frame_quota": list(quota.values()), "evidence_frames_per_clip": per_clip,
            "verified_images": counters["verified_images"], "originals_unchanged": True, "path_leaks": [],
            "profile_sha256": profile_sha, "delivery_zip": str(work / "delivery.zip"),
            "notes": ["synthetic recordings only", "content is mock; human acceptance must still be done in the WebUI"]})
        print(f"PASS: HTTP register/governance/probe/segment/select/prepare via real ingester process, cancel path, "
              f"phase-6 fixed profile, Node export with ingest provenance. Evidence: {work}")
        return 0
    finally:
        if fault_proxy is not None:
            fault_proxy.close()
        if ingester is not None and ingester.poll() is None:
            subprocess.run(["taskkill", "/T", "/F", "/PID", str(ingester.pid)], capture_output=True)
            ingester.wait(timeout=10)
        if plane is not None:
            plane.terminate()
            try:
                plane.wait(timeout=10)
            except subprocess.TimeoutExpired:
                plane.kill()
                plane.wait()
        if server is not None:
            server.shutdown()
            server.server_close()
        for handle in logs:
            handle.close()


if __name__ == "__main__":
    try:
        sys.exit(main(recovery='--recovery' in sys.argv or '--sealed' in sys.argv,
            sealed='--sealed' in sys.argv, browser_fixtures='--browser-fixtures' in sys.argv, revised='--revised' in sys.argv))
    except Exception as error:
        print(f"FAIL: {error}", file=sys.stderr)
        sys.exit(1)
