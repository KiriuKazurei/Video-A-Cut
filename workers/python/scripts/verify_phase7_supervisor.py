"""Crash only the ingester created by our own pipeline session and verify restart.

No image-name termination, no unrelated server or browser. Tokens stay in the
pipeline process environment; the result records identities and state only.
"""
import json, os, subprocess, sys, time, uuid
from pathlib import Path
from urllib.request import urlopen
from verify_phase7_ingest import free_port
repo=Path(__file__).resolve().parents[3]
sys.path.insert(0,str(repo/'workers/python'))
from vac_worker.ingest.runtime import process_identity, matches_process

work=repo/'.run-data'/('phase7-supervisor-'+uuid.uuid4().hex[:8]);work.mkdir()
recordings=work/'recordings';recordings.mkdir()
roots=work/'roots.json';roots.write_text(json.dumps([{'root_id':'rec','name':'隔离验收目录','path':str(recordings)}]),encoding='utf-8')
port=free_port();start=time.time();pipeline=None;observed=[]
with (work/'pipeline.log').open('w',encoding='utf-8') as log:
    try:
        pipeline=subprocess.Popen(['pwsh','-NoProfile','-File',str(repo/'scripts/Start-LocalPipeline.ps1'),'-Port',str(port),'-RunSeconds','22',
            '-IngestRootsFile',str(roots)],stdout=log,stderr=log,creationflags=subprocess.CREATE_NO_WINDOW)
        deadline=time.monotonic()+100
        manifest=None
        while time.monotonic()<deadline:
            if pipeline.poll() is not None:raise AssertionError('pipeline exited before workers were available')
            candidates=[p for p in (repo/'.run-data/local-pipeline').glob('*/owned-processes.json') if p.stat().st_mtime>=start]
            for p in candidates:
                cfg=json.loads((p.parent/'control.json').read_text(encoding='utf-8-sig'))
                if cfg['http_addr']==f'127.0.0.1:{port}':manifest=p;break
            if manifest:break
            time.sleep(.2)
        assert manifest,'owned pipeline manifest absent'
        entries=json.loads(manifest.read_text(encoding='utf-8-sig'));old=next(e for e in entries if e['role']=='ingester')
        old_identity=process_identity(old['pid']);assert old_identity
        observed.extend(process_identity(e['pid']) for e in entries)
        unrelated={e['role']:process_identity(e['pid']) for e in entries if e['role']!='ingester'}
        assert all(unrelated.values())
        # Limit the signal to the freshly started PID whose OS creation identity
        # was just captured; this harness never accepts a user-supplied PID.
        assert matches_process(old_identity)
        subprocess.run(['taskkill','/F','/PID',str(old_identity['pid'])],check=True,capture_output=True)
        deadline=time.monotonic()+15
        new=None
        while time.monotonic()<deadline:
            entries=json.loads(manifest.read_text(encoding='utf-8-sig'))
            candidate=next(e for e in entries if e['role']=='ingester')
            if candidate['pid']!=old['pid'] and process_identity(candidate['pid']):new=candidate;break
            time.sleep(.2)
        assert new and new['generation']==2,'ingester did not restart independently'
        new_identity=process_identity(new['pid']);observed.append(new_identity)
        assert all(matches_process(identity) for identity in unrelated.values()),'other services restarted or exited'
        with urlopen(f'http://127.0.0.1:{port}/api/assets',timeout=5) as response:assert response.status==200
        pipeline.wait(timeout=45);assert pipeline.returncode==0
        assert (manifest.parent/'session.stop').exists()
        assert not any(matches_process(e) for e in observed if e),'owned process survived session stop'
        text=(work/'pipeline.log').read_text(encoding='utf-8')
        assert 'Restarted ingester as instance generation 2.' in text and 'Owned pipeline processes stopped.' in text
        (work/'report.json').write_text(json.dumps({'engineering':'passed','human_acceptance':'pending','runtime_dir':str(manifest.parent),
            'old_ingester_pid':old_identity['pid'],'new_ingester_pid':new_identity['pid'],'ingester_generation':2,
            'unrelated_roles_preserved':list(unrelated),'control_healthy_after_worker_crash':True,'session_stop_clean':True},indent=2),encoding='utf-8')
        print('PASS:',work)
    finally:
        if pipeline and pipeline.poll() is None:
            if manifest:(manifest.parent/'session.stop').touch()
            try:pipeline.wait(timeout=45)
            except subprocess.TimeoutExpired:
                subprocess.run(['taskkill','/T','/F','/PID',str(pipeline.pid)],capture_output=True);pipeline.wait(timeout=10)
