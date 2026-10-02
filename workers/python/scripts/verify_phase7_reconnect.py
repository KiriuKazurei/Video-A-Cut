"""Actual startup outage and control-plane restart during a durable scan unit."""
import hashlib,json,os,subprocess,sys,threading,time,uuid
from pathlib import Path
from verify_phase7_ingest import Http,free_port,sha
from phase7_fixtures import scenes
from recovery_faults import RecoveryFaultProxy
repo=Path(__file__).resolve().parents[3]
sys.path.insert(0,str(repo/'workers/python/tests'))
from test_ingest import policy

def main(source):
    work=repo/'.run-data'/('phase7-reconnect-'+uuid.uuid4().hex[:8]);work.mkdir()
    delivery=work/'delivery';delivery.mkdir();rec=work/'recordings';rec.mkdir()
    media=scenes('ffmpeg',rec/'restart.mkv');original=sha(media)
    token=uuid.uuid4().hex+uuid.uuid4().hex
    def write(p,v):p.write_text(json.dumps(v,ensure_ascii=False,indent=2),encoding='utf-8')
    write(work/'agents.json',{'agents':[{'agent_id':'ingester','role':'ingester','token_sha256':hashlib.sha256(token.encode()).hexdigest()}]})
    port=free_port();url=f'http://127.0.0.1:{port}';roots=[{'root_id':'rec','name':'重连合成素材','path':str(rec)}]
    write(work/'control.json',{'http_addr':f'127.0.0.1:{port}','delivery_root':str(delivery),'ingest_roots':roots,
        'ingest_policy':policy(chunk_us=5_000_000),'mcp_agents_file':str(work/'agents.json'),'lease_seconds':60})
    # Discovery really reaches an unavailable TCP endpoint first.
    write(work/'ingester.json',{'role':'ingester','mcp_url':url+'/mcp','delivery_root':str(delivery),'ingest_roots':roots,
        'token_env':'VAC_RECONNECT_TEST_TOKEN','heartbeat_interval_ms':500,'poll_interval_ms':200})
    env=os.environ.copy();env['VAC_RECONNECT_TEST_TOKEN']=token;env['PYTHONIOENCODING']='utf-8'
    http=Http(url,[str(work)]);plane=worker=proxy=None
    with (work/'control.log').open('w',encoding='utf-8') as cp,(work/'worker.log').open('w',encoding='utf-8') as wp:
        def start_plane():
            process=subprocess.Popen([str(source/'control-plane.exe'),'-config',str(work/'control.json'),'-db',str(work/'control.db')],stdout=cp,stderr=cp)
            for _ in range(100):
                if process.poll() is not None:raise AssertionError('control plane failed to restart')
                try:http.call('/api/assets');return process
                except OSError:time.sleep(.1)
            raise AssertionError('control readiness timeout')
        def wait(run,state):
            deadline=time.monotonic()+60
            while time.monotonic()<deadline:
                assert worker.poll() is None,'worker exited during reconnect'
                v=http.call('/api/ingest-runs/'+run)
                if v['run']['state']==state:return v
                assert v['run']['state']!='failed',v
                time.sleep(.2)
            raise AssertionError('reconnect state timeout')
        try:
            worker=subprocess.Popen([sys.executable,'-m','vac_worker','--config',str(work/'ingester.json')],cwd=repo/'workers/python',env=env,stdout=wp,stderr=wp)
            deadline=time.monotonic()+15
            while time.monotonic()<deadline:
                assert worker.poll() is None
                if 'startup connection transient' in (work/'worker.log').read_text(encoding='utf-8'):break
                time.sleep(.2)
            else:raise AssertionError('startup outage did not reach the real TCP failure boundary')
            plane=start_plane()
            deadline=time.monotonic()+15
            while time.monotonic()<deadline:
                if http.call('/api/ingest-roots')['ingesters']:break
                time.sleep(.2)
            else:raise AssertionError('worker failed to register after startup outage')
            original_worker_pid=worker.pid
            # Switch only this owned worker to the loopback fault proxy before
            # a task exists; the startup-outage evidence is already persisted.
            worker.kill();worker.wait(timeout=10)
            proxy=RecoveryFaultProxy(url)
            config=json.loads((work/'ingester.json').read_text(encoding='utf-8'));config['mcp_url']=proxy.url;write(work/'ingester.json',config)
            worker=subprocess.Popen([sys.executable,'-m','vac_worker','--config',str(work/'ingester.json')],cwd=repo/'workers/python',env=env,stdout=wp,stderr=wp)
            reg=http.call('/api/recordings',{'asset_id':'reconnect','root_id':'rec','relative_path':media.name,'idempotency_key':'reg'},expect=201)
            http.call('/api/assets/reconnect',{'agent_visible':True,'allowed_agents':['ingester']},'PATCH')
            run=http.call('/api/assets/reconnect/ingest-runs',{'source_id':reg['source']['source_id'],'expected_source_version':reg['source_version'],'idempotency_key':'start'},expect=202)['run']['run_id']
            v=wait(run,'awaiting_review');assert v['run']['stage']=='probe'
            proxy.armed_scan=True
            http.call('/api/ingest-runs/'+run+'/analysis-plans',{'expected_version':v['run']['version'],'video_stream_index':0,'game_audio_stream_index':1,
                'source_range_us':[0,v['probe']['duration_us']],'segmentation':{'method':'scene_change','threshold':.3,'min_segment_us':1_000_000,'max_segment_us':60_000_000},'idempotency_key':'plan'},expect=202)
            assert proxy.scan_event.wait(30)
            former=dict(proxy.scan_args);worker_pid=worker.pid;plane.terminate();plane.wait(timeout=10)
            time.sleep(2);assert worker.poll() is None
            plane=start_plane()
            v=wait(run,'awaiting_review');assert v['run']['stage']=='segment_review'
            assert worker.pid==worker_pid and v['execution']['execution_id']==former['execution_id']
            assert v['tasks'][-1]['task']['status']!='failed'
            text=(work/'worker.log').read_text(encoding='utf-8')
            assert 'startup connection transient' in text and 'save_ingest_checkpoint transient error' in text
            assert sha(media)==original and not http.leaks()
            # Persist cancellation and governance while the worker's entire MCP
            # connection is unavailable. It must drain/ack the old scope before
            # beginning the new source run, without submitting obsolete results.
            proxy.scan_event.clear();proxy.armed_scan=True
            http.call('/api/ingest-runs/'+run+'/analysis-plans',{'expected_version':v['run']['version'],'video_stream_index':0,'game_audio_stream_index':2,
                'source_range_us':[0,v['probe']['duration_us']],'segmentation':{'method':'scene_change','threshold':.35,'min_segment_us':1_000_000,'max_segment_us':60_000_000},'idempotency_key':'new-plan'},expect=202)
            assert proxy.scan_event.wait(30);old_scope=dict(proxy.scan_args);proxy.offline=True
            http.call('/api/assets/reconnect',{'locked':True},'PATCH')
            current=http.call('/api/ingest-runs/'+run)['run']
            http.call('/api/ingest-runs/'+run+'/cancel',{'expected_version':current['version'],'reason':'cancel while MCP disconnected'})
            http.call('/api/assets/reconnect',{'locked':False},'PATCH')
            next_run=http.call('/api/assets/reconnect/ingest-runs',{'source_id':reg['source']['source_id'],'expected_source_version':reg['source_version'],
                'idempotency_key':'after-cancel'},expect=202)['run']['run_id']
            # Exceed the request retry budget but stay inside the original lease,
            # reproducing the boundary where a deferred stop ACK is necessary.
            time.sleep(24);assert worker.poll() is None
            proxy.offline=False
            new=wait(next_run,'awaiting_review');assert new['run']['stage']=='probe'
            old=http.call('/api/ingest-runs/'+run)
            assert old['run']['state']=='cancelled' and new['execution']['execution_id']!=old_scope['execution_id']
            assert new['execution']['status']=='succeeded'
            write(work/'report.json',{'engineering':'passed','human_acceptance':'pending','startup_tcp_outage_recovered':True,
                'startup_worker_pid':original_worker_pid,'control_restarted_mid_checkpoint':True,'active_worker_pid_preserved':True,
                'same_execution_resumed':True,'execution_id':former['execution_id'],'checkpoints_done':v['execution']['checkpoints_done'],
                'new_plan_then_disconnected_lock_and_cancel':True,'obsolete_run_remains_cancelled':True,'replacement_run_id':next_run,
                'original_unchanged':True})
            print('PASS:',work)
        finally:
            if proxy:proxy.close()
            for process in (worker,plane):
                if process and process.poll() is None:
                    subprocess.run(['taskkill','/T','/F','/PID',str(process.pid)],capture_output=True);process.wait(timeout=10)

if __name__=='__main__':main(Path(sys.argv[1]).resolve())
