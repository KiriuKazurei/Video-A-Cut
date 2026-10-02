"""Synthetic measurements through preparation and the final Node audio stem.

Uses real ffmpeg and Node adapters, no models or user recordings. The measured
180-second scan is a bounded benchmark, not a claim about six-hour recordings.
"""
import array, ctypes, hashlib, json, os, subprocess, sys, threading, time, uuid
from pathlib import Path

repo = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(repo/'workers/python'))
sys.path.insert(0, str(repo/'workers/python/tests'))
sys.path.insert(0, str(Path(__file__).resolve().parent))
import phase7_fixtures as fx
from test_ingest import IngestMediaTest, make_exec, policy
from vac_worker.ingest import prepare, segment
from vac_worker.ingest.common import IngestFailure, Runner
from unittest.mock import patch


def peak_memory(proc):
    if os.name != 'nt': return None
    from ctypes import wintypes as w
    class Counters(ctypes.Structure):
        _fields_=[('cb',w.DWORD),('faults',w.DWORD)]+[(name,ctypes.c_size_t) for name in
            ('peak_ws','ws','peak_paged','paged','peak_nonpaged','nonpaged','pagefile','peak_pagefile')]
    counters=Counters();counters.cb=ctypes.sizeof(counters)
    psapi=ctypes.WinDLL('psapi');psapi.GetProcessMemoryInfo.argtypes=[w.HANDLE,ctypes.c_void_p,w.DWORD]
    return counters.peak_ws if psapi.GetProcessMemoryInfo(w.HANDLE(int(proc._handle)),ctypes.byref(counters),counters.cb) else None


def beeps(path):
    done = subprocess.run(['ffmpeg','-v','error','-i',str(path),'-f','s16le','-ac','1','-ar','48000','-'],capture_output=True,check=True)
    samples, result, last = array.array('h', done.stdout), [], -10**9
    for i, value in enumerate(samples):
        if abs(value)>8000:
            if i-last>9600: result.append(i/48000)
            last=i
    return result


def main():
    work=repo/'.run-data'/('phase7-media-'+uuid.uuid4().hex[:8])
    work.mkdir(parents=True)
    os.environ['TEMP']=os.environ['TMP']=str(work)
    IngestMediaTest.setUpClass()
    fixture=IngestMediaTest()
    fixture.root=work/'delivery';fixture.root.mkdir()
    rows=[]
    measurements=[]
    normal_run=Runner._run
    def measured(self,args,timeout,stdout_sink,cwd):
        stop=threading.Event();peak=[0]
        def sample():
            while not stop.wait(.005):
                proc=getattr(self,'proc',None)
                if proc is not None:peak[0]=max(peak[0],peak_memory(proc) or 0)
        monitor=threading.Thread(target=sample);monitor.start();started=time.monotonic()
        try:return normal_run(self,args,timeout,stdout_sink,cwd)
        finally:
            stop.set();monitor.join();measurements.append({'tool':Path(args[0]).name,'peak_working_set_bytes':peak[0],'wall_seconds':time.monotonic()-started})
    sampler=patch.object(Runner,'_run',measured);sampler.start()
    try:
        spans=[(1_500_000,3_000_000),(5_000_000,9_000_000)]
        for name in ('sync_vfr','sync_cfr'):
            for fps in (30,60):
                inp=fixture.prepare_inputs(fixture.files[name].name,spans,fps,audio=1)
                ex=make_exec(fixture.root,fixture.roots,'media_prepare',inp,exec_id=f'{name}_{fps}')
                prepare.media_prepare(ex)
                mapping=fixture.assert_sync(ex.package_dir,fps,spans)
                out=work/f'final-{name}-{fps}'
                done=subprocess.run(['node',str(repo/'workers/node/timeline-cli/src/cli.mjs'),'build','--edl',str(ex.package_dir/'edl.json'),
                    '--output',str(out),'--adapters',str(repo/'workers/node/timeline-cli/adapters/index.mjs')],capture_output=True,text=True,timeout=120)
                (work/f'node-{name}-{fps}.log').write_text(done.stdout+done.stderr,encoding='utf-8')
                assert done.returncode==0,done.stderr
                observed=beeps(out/'audio_a1.wav')
                flashes=[]
                for entry in mapping['segments']:
                    marks,_=fx.measure_markers('ffmpeg',ex.package_dir/entry['media'])
                    flashes.extend(m+entry['timeline_in_frames']/fps for m in marks)
                assert len(observed)==len(flashes),(observed,flashes)
                skew=[abs(a-b) for a,b in zip(observed,flashes)]
                assert max(skew)<=1/fps+1e-6,(name,fps,skew)
                rows.append({'fixture':name,'fps':fps,'flash_seconds':flashes,'game_stem_beep_seconds':observed,
                    'max_skew_seconds':max(skew),'limit_seconds':1/fps,'source_origin_us':mapping['source_origin_us'],
                    'audio_offset_us':mapping['audio_offset_us'],'final_stem_sha256':hashlib.sha256((out/'audio_a1.wav').read_bytes()).hexdigest()})

        # Measure a genuinely longer source and bound the detector's candidates,
        # thumbnails and output JSON. CPU/working-set measurements are sampled
        # on the owned ffmpeg tree by the runner's registry when available.
        long=fx.plain('ffmpeg',fixture.roots['rec']/'long.mp4','30',seconds=180,audio=False)
        long_metrics_start=len(measurements)
        started=time.monotonic()
        src,pr=fixture.published(long.name)
        plan=fixture.analysis(src,pr,[0,pr['duration_us']],threshold=.95,min_us=1_000_000,max_us=10_000_000,audio=None)
        ex=make_exec(fixture.root,fixture.roots,'segment',{'source':src,'probe':pr,'analysis':plan,'analysis_sha256':'e'*64},
            exec_id='long',pol=policy(max_thumbnails=4,chunk_us=30_000_000))
        segment.segment(ex)
        doc=json.loads((ex.package_dir/'segments.json').read_text())
        thumbs=list((ex.package_dir/'thumbnails').glob('*.jpg'))
        assert doc['chunks_total']==6 and len(doc['candidates'])==18 and len(thumbs)<=4,doc
        written=sum(p.stat().st_size for p in ex.out_dir.rglob('*') if p.is_file())
        long_stats={'source_seconds':180,'chunks':doc['chunks_total'],'candidates':len(doc['candidates']),
            'thumbnails':len(thumbs),'wall_seconds':time.monotonic()-started,'execution_written_bytes':written,
            'detection_json_bytes':(ex.package_dir/'segments.json').stat().st_size,
            'peak_owned_media_process_bytes':max(m['peak_working_set_bytes'] for m in measurements[long_metrics_start:])}
        assert long_stats['peak_owned_media_process_bytes']>0
        over=make_exec(fixture.root,fixture.roots,'segment',{'source':src,'probe':pr,'analysis':plan,'analysis_sha256':'e'*64},
            exec_id='over-budget',pol=policy(max_candidates=10))
        try: segment.segment(over)
        except IngestFailure: pass
        else: raise AssertionError('candidate quota was silently exceeded')
        assert not over.package_dir.exists()

        # One-frame flashes can produce paired candidate cuts; length merging
        # must still cover the source once and cap the number of candidates.
        src,pr=fixture.published(fixture.files['sync_cfr'].name)
        plan=fixture.analysis(src,pr,[0,pr['duration_us']],min_us=500_000,max_us=5_000_000,audio=1)
        flash=make_exec(fixture.root,fixture.roots,'segment',{'source':src,'probe':pr,'analysis':plan,'analysis_sha256':'e'*64},exec_id='flash')
        segment.segment(flash)
        doc=json.loads((flash.package_dir/'segments.json').read_text())
        c=doc['candidates']
        assert c[0]['start_us']==0 and c[-1]['end_us']==pr['duration_us']
        assert all(a['end_us']==b['start_us'] for a,b in zip(c,c[1:]))
        assert all(row['end_us']-row['start_us']>=500_000 for row in c)
        report={'engineering':'passed','human_acceptance':'pending','sync':rows,'long_scan':long_stats,
            'over_budget_not_published':True,'flash_cut_count':len(doc['cuts']),'flash_candidate_count':len(c)}
        (work/'report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2),encoding='utf-8')
        print('PASS:',work)
    finally:
        sampler.stop();IngestMediaTest.tearDownClass()


if __name__=='__main__': main()
