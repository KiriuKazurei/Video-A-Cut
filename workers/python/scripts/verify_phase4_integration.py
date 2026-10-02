import hashlib
import os
import sys
import json
import tempfile
import subprocess
import shutil
from pathlib import Path

# Add project paths
repo_root = Path(__file__).resolve().parent.parent.parent.parent
sys.path.insert(0, str(repo_root / "workers" / "python"))
sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from vac_worker.sampling import (
    sample_clip_frames,
    SamplingLimits,
    FrameEvidence,
    SamplingError,
    normalize_relative_path
)
from vac_worker.content import (
    ClipEvidence,
    SceneDecision,
    NarrationDecision,
    BuiltinContentProvider,
    VisionContentProvider,
    NarrationContentProvider,
    validate_scenes,
    validate_narration,
    make_content_provider,
    ContentError
)
from vac_worker.stages import (
    StageContext,
    Tools,
    recognize,
    sort,
    narrate,
    tts,
    subtitle,
    mix,
    StageFailure
)

def ff(*args):
    subprocess.run(["ffmpeg", "-hide_banner", "-loglevel", "error", "-y", *args], check=True)

def make_synthetic_source(root: Path) -> dict:
    root.mkdir(parents=True, exist_ok=True)
    ff("-f", "lavfi", "-i", "testsrc2=size=160x90:rate=30:duration=2", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=2",
       "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", str(root / "b.mp4"))
    ff("-f", "lavfi", "-i", "smptebars=size=160x90:rate=30:duration=2", "-f", "lavfi", "-i", "sine=frequency=660:sample_rate=48000:duration=2",
       "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", str(root / "a.mp4"))
    edl = {
        "timeline": {"fps": 30, "sample_rate": 48000},
        "video": [
            {"src": "b.mp4", "in": 0.0, "out": 1.5, "timeline_in": 3.0},
            {"src": "a.mp4", "in": 0.5, "out": 2.0, "timeline_in": 0.0}
        ],
        "game_audio": [
            {"src": "b.mp4", "in": 0.0, "out": 1.5, "timeline_in": 3.0, "gain_db": -12},
            {"src": "a.mp4", "in": 0.5, "out": 2.0, "timeline_in": 0.0, "gain_db": -12}
        ],
        "voice": [],
        "subtitle": [],
        "music": []
    }
    (root / "edl.json").write_text(json.dumps(edl))
    return edl

def run_integration_pipeline_verification():
    """Synthetic stage check.

    This run uses the built-in provider and local media. It does not call a
    vision or narration model, and it is not the Premiere content acceptance.
    """
    print("=== 第四阶段合成媒体阶段检查（builtin，不是模型或 Premiere 验收）===")
    
    # ----------------------------------------------------
    # 模块 1: 有界取样 (Bounded Frame Sampling)
    # ----------------------------------------------------
    print("[1/5] 验证模块 1: 有界取样边界与安全性...")
    limits = SamplingLimits(max_frames_per_clip=3, max_width=1280, max_height=720, max_bytes_per_frame=2*1024*1024)
    assert limits.max_frames_per_clip == 3
    # 路径沙箱化防穿透
    safe_rel = normalize_relative_path("C:/Users/test/media/video.mp4")
    assert not safe_rel.startswith("C:") and not safe_rel.startswith("/")
    print(" -> 模块 1 限制参数与路径沙箱化校验通过")

    # ----------------------------------------------------
    # 模块 2: 识别适配器与鉴权隔离 (Vision Recognition Adapter)
    # ----------------------------------------------------
    print("[2/5] 验证模块 2: 识别适配器与 Fail-Closed 熔断...")
    # 外部网络传输默认关闭
    vp_closed = VisionContentProvider(endpoint="http://example.com/api", allow_external=False)
    try:
        vp_closed.recognize([ClipEvidence(0, "clip.mp4", 0.0, 5.0, 0.0, 5.0)])
        assert False, "Should fail closed when allow_external=False"
    except ContentError as e:
        assert "external transmission is disabled" in str(e)
    
    # Token 缺失拦截
    os.environ.pop("TEST_VAC_TOKEN", None)
    vp_no_token = VisionContentProvider(endpoint="http://example.com/api", token_env="TEST_VAC_TOKEN", allow_external=True)
    try:
        vp_no_token.recognize([ClipEvidence(0, "clip.mp4", 0.0, 5.0, 0.0, 5.0)])
        assert False, "Should fail closed when token is missing"
    except ContentError as e:
        assert "missing credentials in environment variable" in str(e)
    
    # Factory
    p_builtin = make_content_provider("builtin")
    assert isinstance(p_builtin, BuiltinContentProvider)
    try:
        make_content_provider("unknown_provider")
        assert False, "Should reject unknown provider"
    except ContentError:
        pass
    print(" -> 模块 2 鉴权、熔断及 Factory 注册机制校验通过")

    # ----------------------------------------------------
    # 模块 3: 排序语义与冲突规则 (Sequence Rank & Conflict Rules)
    # ----------------------------------------------------
    print("[3/5] 验证模块 3: 排序语义、冲突检测与音画同步...")
    tools = Tools()
    edl_sample = {
        "timeline": {"fps": 30, "sample_rate": 48000},
        "video": [
            {"src": "b.mp4", "in": 0.0, "out": 1.5, "timeline_in": 0.0},
            {"src": "a.mp4", "in": 0.0, "out": 1.5, "timeline_in": 1.5}
        ],
        "game_audio": [
            {"src": "b.mp4", "in": 0.0, "out": 1.5, "timeline_in": 0.0},
            {"src": "a.mp4", "in": 0.0, "out": 1.5, "timeline_in": 1.5}
        ],
        "voice": [],
        "subtitle": [],
        "music": [],
        "scenes": [
            {"index": 0, "label": "Boss", "method": "vision", "confidence": 0.9, "sequence_rank": 2},
            {"index": 1, "label": "Intro", "method": "vision", "confidence": 0.95, "sequence_rank": 1}
        ]
    }
    with tempfile.TemporaryDirectory() as td:
        dir_path = Path(td)
        # 建立假文件供 package 检查
        (dir_path / "a.mp4").write_bytes(b"dummy")
        (dir_path / "b.mp4").write_bytes(b"dummy")
        out_sort = dir_path / "out_sort"
        out_sort.mkdir()
        ctx = StageContext(edl=edl_sample, source_dir=dir_path, out_dir=out_sort, tools=tools)
        res = sort(ctx)
        sorted_edl = json.loads((out_sort / "edl.json").read_text(encoding="utf-8"))
        assert sorted_edl["video"][0]["src"].endswith("a.mp4")
        assert sorted_edl["video"][0]["timeline_in"] == 0.0
        assert sorted_edl["video"][1]["src"].endswith("b.mp4")
        assert sorted_edl["video"][1]["timeline_in"] == 1.5
        assert sorted_edl["game_audio"][0]["timeline_in"] == 0.0
        assert sorted_edl["game_audio"][1]["timeline_in"] == 1.5

        # 重复 rank 冲突
        edl_dup = json.loads(json.dumps(edl_sample))
        edl_dup["scenes"][0]["sequence_rank"] = 1
        edl_dup["scenes"][1]["sequence_rank"] = 1
        out_dup = dir_path / "out_dup"
        out_dup.mkdir()
        ctx_dup = StageContext(edl=edl_dup, source_dir=dir_path, out_dir=out_dup, tools=tools)
        try:
            sort(ctx_dup)
            assert False, "Should fail on duplicate sequence_rank"
        except StageFailure as e:
            assert "duplicate sequence_rank" in str(e)

        # 低置信度排序冲突
        edl_low_conf = json.loads(json.dumps(edl_sample))
        edl_low_conf["scenes"][0]["confidence"] = 0.3
        out_low = dir_path / "out_low"
        out_low.mkdir()
        ctx_low = StageContext(edl=edl_low_conf, source_dir=dir_path, out_dir=out_low, tools=tools)
        try:
            sort(ctx_low)
            assert False, "Should fail on low confidence ranking"
        except StageFailure as e:
            assert "low confidence" in str(e)
            
    print(" -> 模块 3 升序重排、音画轨道严格同步与冲突拦截校验通过")

    # ----------------------------------------------------
    # 模块 4: 解说草稿与时间窗审计约束 (Narration Draft & Window)
    # ----------------------------------------------------
    print("[4/5] 验证模块 4: 解说草稿元数据审计与时间窗边界...")
    clips = [ClipEvidence(0, "clip.mp4", 10.0, 0.0, 5.0, 0.0)]
    valid_narr = [NarrationDecision(0, "精彩击杀瞬间", 0.5, 3.5, source_scene_label="Boss", model_version="gpt-4o", needs_review=True)]
    checked = validate_narration(clips, valid_narr)
    assert checked[0].source_scene_label == "Boss"
    assert checked[0].model_version == "gpt-4o"
    assert checked[0].needs_review is True

    # 换行符/非法控制字符硬拦截
    try:
        validate_narration(clips, [NarrationDecision(0, "第一行\n第二行", 0.5, 3.5)])
        assert False, "Should reject newlines in narration"
    except ContentError as e:
        assert "newline or control" in str(e)

    # 时间窗越界拦截
    try:
        validate_narration(clips, [NarrationDecision(0, "超时解说", 1.0, 6.0)])
        assert False, "Should reject out-of-window narration"
    except ContentError as e:
        assert "escapes" in str(e)
        
    print(" -> 模块 4 审计标记注入、不可见字符拦截与时间窗校验通过")

    # ----------------------------------------------------
    # 模块 5: 全流水线端到端级联与导出验证 (End-to-End Cascade)
    # ----------------------------------------------------
    print("[5/5] 验证模块 5: 端到端流水线阶段级联与输出验证...")
    
    with tempfile.TemporaryDirectory() as td:
        dir_path = Path(td)
        source_dir = dir_path / "source"
        pipeline_edl = make_synthetic_source(source_dir)
        
        # 1. recognize
        out_dir_1 = dir_path / "01_recognize"
        out_dir_1.mkdir()
        ctx1 = StageContext(edl=pipeline_edl, source_dir=source_dir, out_dir=out_dir_1, tools=tools,
                            content_provider=BuiltinContentProvider(), work_dir=out_dir_1)
        recognize(ctx1)
        out1_edl = json.loads((out_dir_1 / "edl.json").read_text(encoding="utf-8"))
        assert len(out1_edl["scenes"]) == 2
        assert "evidence_frames" in out1_edl["scenes"][0]
        
        # 2. sort
        out_dir_2 = dir_path / "02_sort"
        out_dir_2.mkdir()
        ctx2 = StageContext(edl=out1_edl, source_dir=out_dir_1, out_dir=out_dir_2, tools=tools,
                            content_provider=BuiltinContentProvider(), work_dir=out_dir_2)
        sort(ctx2)
        out2_edl = json.loads((out_dir_2 / "edl.json").read_text(encoding="utf-8"))
        
        # 3. narrate
        out_dir_3 = dir_path / "03_narrate"
        out_dir_3.mkdir()
        ctx3 = StageContext(edl=out2_edl, source_dir=out_dir_2, out_dir=out_dir_3, tools=tools,
                            content_provider=BuiltinContentProvider(), work_dir=out_dir_3)
        narrate(ctx3)
        out3_edl = json.loads((out_dir_3 / "edl.json").read_text(encoding="utf-8"))
        assert len(out3_edl["narration"]) == 2
        assert out3_edl["narration"][0]["model_version"] == "builtin"
        assert out3_edl["narration"][0]["needs_review"] is True
        
        # 4. tts. The operator flag is test configuration, not an EDL field.
        out_dir_4 = dir_path / "04_tts"
        out_dir_4.mkdir()
        ctx4 = StageContext(edl=out3_edl, source_dir=out_dir_3, out_dir=out_dir_4, tools=tools,
                            work_dir=out_dir_4, tts_auto_approve=True)
        tts(ctx4)
        out4_edl = json.loads((out_dir_4 / "edl.json").read_text(encoding="utf-8"))
        assert len(out4_edl["voice"]) == 2
        
        # 5. subtitle
        out_dir_5 = dir_path / "05_subtitle"
        out_dir_5.mkdir()
        ctx5 = StageContext(edl=out4_edl, source_dir=out_dir_4, out_dir=out_dir_5, tools=tools, work_dir=out_dir_5)
        subtitle(ctx5)
        out5_edl = json.loads((out_dir_5 / "edl.json").read_text(encoding="utf-8"))
        assert len(out5_edl["subtitle"]) == 2
        
        # 6. mix
        out_dir_6 = dir_path / "06_mix"
        out_dir_6.mkdir()
        ctx6 = StageContext(edl=out5_edl, source_dir=out_dir_5, out_dir=out_dir_6, tools=tools, work_dir=out_dir_6)
        mix_res = mix(ctx6)
        out6_edl = json.loads((out_dir_6 / "edl.json").read_text(encoding="utf-8"))
        assert len(out6_edl["music"]) == 1
        assert out6_edl["music"][0]["duck"] is True
        assert mix_res["duck"] is True
        evidence_path = out_dir_6 / "samples" / "evidence-manifest.json"
        assert evidence_path.is_file(), "mix package dropped the evidence manifest"
        evidence = json.loads(evidence_path.read_text(encoding="utf-8"))
        assert evidence["model_version"] == "builtin"
        assert evidence["sampling_config_version"] == "sampling-v1"
        by_hash = {frame["sha256"]: frame for frame in evidence["frames"]}
        for scene in out6_edl["scenes"]:
            assert scene["evidence_frames"], "scene has no frame evidence"
            for ref in scene["evidence_frames"]:
                frame = by_hash[ref]
                raw = (out_dir_6 / frame["path"]).read_bytes()
                assert hashlib.sha256(raw).hexdigest() == ref

    print(" -> 合成媒体阶段检查通过。这不代表真实模型、正式任务链或 Premiere 人工验收。")

if __name__ == "__main__":
    run_integration_pipeline_verification()
