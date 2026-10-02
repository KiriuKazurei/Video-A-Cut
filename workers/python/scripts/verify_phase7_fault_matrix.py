"""Real HTTP/MCP states with explicit local disk/timeout/process fault injection.

Only the failing boundary is injected. Recovery uses the real media handlers,
kernel locks, control plane and checkpoint registration. No human pass.
"""
import json, os, socket, subprocess, sys, threading, time, uuid
from pathlib import Path
from unittest.mock import patch

repo=Path(__file__).resolve().parents[3]
sys.path.insert(0,str(repo/'workers/python'));sys.path.insert(0,str(repo/'workers/python/tests'))
from verify_phase7_ingest import Http, free_port, sha
from phase7_fixtures import scenes
from test_ingest import policy
from vac_worker.mcp import Client, ToolError
from vac_worker.worker import Config
from vac_worker.ingest.common import Exec, Runner, IngestFailure
from vac_worker.ingest.loop import process_ingest, capability
from vac_worker.ingest.isolation import ProcessRegistry
from vac_worker.ingest.runtime import process_identity
from vac_worker.ingest.supervise import Halt


def main(source):
    work=repo/'.run-data'/('phase7-faults-'+uuid.uuid4().hex[:8]);work.mkdir()
    delivery=work/'delivery';delivery.mkdir();recordings=work/'recordings';recordings.mkdir()
    media=scenes('ffmpeg',recordings/'fault.mkv');original=sha(media)
    token=uuid.uuid4().hex+uuid.uuid4().hex
    import hashlib
    def write(p,v):p.write_text(json.dumps(v,ensure_ascii=False,indent=2),encoding='utf-8')
    write(work/'agents.json',{'agents':[{'agent_id':'ingester','role':'ingester','token_sha256':hashlib.sha256(token.encode()).hexdigest()}]})
    port=free_port();url=f'http://127.0.0.1:{port}'
    roots=[{'root_id':'rec','name':'故障合成素材','path':str(recordings)}]
    write(work/'control.json',{'http_addr':f'127.0.0.1:{port}','delivery_root':str(delivery),'mcp_agents_file':str(work/'agents.json'),
        'lease_seconds':60,'ingest_roots':roots,'ingest_policy':policy(chunk_us=5_000_000)})
    cfg=Config(url+'/mcp',token,delivery,role='ingester',ingest_roots={'rec':recordings},heartbeat_interval=.5)
    events=[]
    class ObservedClient(Client):
        def call(self,name,args=None,**kwargs):
            value=super().call(name,args,**kwargs)
            if name in ('begin_execution','ack_execution_stopped'):
                events.append({'tool':name,'execution_id':args.get('execution_id'),'monotonic':time.monotonic(),
                    'owned_entries':len(registry.entries),'owned_pids_live':any(process_identity(p) for p in registry.pids)})
            return value
    client=ObservedClient(url+'/mcp',token)
    instance=uuid.uuid4().hex;registry=ProcessRegistry(delivery,instance);logs=[]
    http=Http(url,[str(work),str(delivery),str(recordings)]);plane=None
    rows=[]
    with (work/'control.log').open('w',encoding='utf-8') as output:
        try:
            plane=subprocess.Popen([str(source/'control-plane.exe'),'-config',str(work/'control.json'),'-db',str(work/'control.db')],stdout=output,stderr=output)
            for _ in range(100):
                try:http.call('/api/assets');break
                except OSError:time.sleep(.1)
            client.call('heartbeat',{'ingest_capability':capability(cfg)})
            profile=json.loads((repo/'configs/phase6/profile.builtin.example.json').read_text(encoding='utf-8'))
            http.call('/api/processing-profiles',{'profile':profile,'expected_revision':0,'idempotency_key':'p'})

            def register(asset):
                reg=http.call('/api/recordings',{'asset_id':asset,'root_id':'rec','relative_path':media.name,'idempotency_key':asset},expect=201)
                http.call('/api/assets/'+asset,{'agent_visible':True,'allowed_agents':['ingester']},'PATCH')
                return reg,http.call('/api/assets/'+asset+'/ingest-runs',{'source_id':reg['source']['source_id'],
                    'expected_source_version':reg['source_version'],'idempotency_key':'start'},expect=202)['run']['run_id']
            def claim():
                response=client.call('claim_task',{'runtime_instance_id':instance,'request_id':uuid.uuid4().hex})
                assert response['claimed'],response
                tk=response['task'];tk.update(_lease_remaining_ms=response['lease_remaining_ms'],_lease_rtt=0)
                return tk,response['scope']
            def perform():
                tk,scope=claim()
                result=process_ingest(client,cfg,tk,logs.append,threading.Event(),scope=scope,registry=registry)
                return result,scope
            def view(run):return http.call('/api/ingest-runs/'+run)
            def retry(run):
                v=view(run)['run'];http.call('/api/ingest-runs/'+run+'/retry',{'expected_version':v['version'],'stage':v['stage'],'idempotency_key':uuid.uuid4().hex},expect=202)
            def analysis(run):
                v=view(run);http.call('/api/ingest-runs/'+run+'/analysis-plans',{'expected_version':v['run']['version'],
                    'video_stream_index':0,'game_audio_stream_index':1,'source_range_us':[0,v['probe']['duration_us']],
                    'segmentation':{'method':'scene_change','threshold':.3,'min_segment_us':1_000_000,'max_segment_us':60_000_000},
                    'idempotency_key':uuid.uuid4().hex},expect=202)
            def selection_prepare(run):
                v=view(run);c=http.call('/api/ingest-runs/'+run+'/segments?limit=50')['items'][:2]
                selection=http.call('/api/ingest-runs/'+run+'/selection-revisions',{'expected_version':v['run']['version'],
                    'base_plan_revision':v['run']['analysis_revision'],'selected_segments':[{k:s[k] for k in ('segment_id','start_us','end_us')} for s in c],
                    'output':{'fps':30,'sample_rate':48000},'idempotency_key':uuid.uuid4().hex},expect=201)
                http.call('/api/ingest-runs/'+run+'/prepare',{'expected_version':selection['version'],'plan_revision':selection['selection_revision'],
                    'profile_id':profile['profile_id'],'profile_revision':1,'idempotency_key':uuid.uuid4().hex},expect=202)
            def disk(self,required=0):raise IngestFailure('injected insufficient disk space')
            reg,run=register('probe_disk')
            with patch.object(Exec,'check_space',disk):result,scope=perform()
            failed=view(run);assert result['outcome']=='failed' and failed['run']['state']=='failed' and not failed['source']['has_snapshot'],failed
            retry(run);result,_=perform();assert result['outcome']=='succeeded'
            rows.append({'case':'copy_disk','failed_state':True,'retry_succeeded':True,'run_id':run})
            # Complete the asset to review, then cancel it so it cannot interfere
            # with subsequent independent cases.
            v=view(run)['run'];http.call('/api/ingest-runs/'+run+'/cancel',{'expected_version':v['version'],'reason':'fixture complete'})

            normal_run=Runner.run
            for stage in ('segment','prepare'):
                reg,run=register(stage+'_timeout');assert perform()[0]['outcome']=='succeeded';analysis(run)
                if stage=='prepare':assert perform()[0]['outcome']=='succeeded';selection_prepare(run)
                count=[0]
                def timeout(self,args,timeout,stdout_sink=None,cwd=None):
                    targeted=args[0]==cfg.ffmpeg and ("scene" in ' '.join(args) if stage=='segment' else '-filter_complex' in args)
                    if targeted:
                        count[0]+=1
                        if count[0]==2:
                            return normal_run(self,[sys.executable,'-c','import time;time.sleep(10)'],.01,stdout_sink,cwd)
                    return normal_run(self,args,timeout,stdout_sink,cwd)
                start_log=len(logs)
                with patch.object(Runner,'run',timeout):result,scope=perform()
                failed=view(run);assert result['outcome']=='failed' and failed['run']['state']=='failed',result
                assert failed['execution']['checkpoints_done']>=1,failed
                assert registry.entries==[] and not any(process_identity(p) for p in registry.pids)
                retry(run);result,new_scope=perform();assert result['outcome']=='succeeded',result
                phrase='reusing 1 verified scan chunks' if stage=='segment' else 'reusing 1 verified prepared clips'
                assert any(phrase in line for line in logs[start_log:]),logs[start_log:]
                rows.append({'case':stage+'_timeout','run_id':run,'former_execution_id':scope['execution_id'],
                    'new_execution_id':new_scope['execution_id'],'completed_unit_retained':True,'retry_reused_unit':True,'owned_tree_exited':True})
                if stage=='segment':v=view(run)['run'];http.call('/api/ingest-runs/'+run+'/cancel',{'expected_version':v['version'],'reason':'fixture complete'})

            # A real in-flight owned tree is held at the media boundary. Default
            # reject leaves the old execution running; replace requests stop,
            # waits for drain, then lets the new scope begin.
            reg,run=register('handoff');assert perform()[0]['outcome']=='succeeded';analysis(run)
            tk,old_scope=claim();pidfile=work/'handoff-grandchild.pid';entered=threading.Event();result_holder=[]
            def slow(self,args,timeout,stdout_sink=None,cwd=None):
                if args[0]==cfg.ffmpeg and 'scene' in ' '.join(args):
                    entered.set()
                    code="import subprocess,sys,time;from pathlib import Path;p=subprocess.Popen([sys.executable,'-c','import time;time.sleep(60)']);Path(sys.argv[1]).write_text(str(p.pid));time.sleep(60)"
                    return normal_run(self,[sys.executable,'-c',code,str(pidfile)],60,stdout_sink,cwd)
                return normal_run(self,args,timeout,stdout_sink,cwd)
            def held():result_holder.append(process_ingest(client,cfg,tk,logs.append,threading.Event(),scope=old_scope,registry=registry))
            stop_events=[]
            normal_stop=Halt.stop
            def observed_stop(halt,reason):
                stop_events.append({'reason':reason,'monotonic':time.monotonic()})
                return normal_stop(halt,reason)
            with patch.object(Runner,'run',slow), patch.object(Halt,'stop',observed_stop):
                thread=threading.Thread(target=held);thread.start();assert entered.wait(10)
                deadline=time.monotonic()+10
                while not pidfile.exists() and time.monotonic()<deadline:time.sleep(.05)
                grandchild=int(pidfile.read_text());assert process_identity(grandchild)
                start_body={'source_id':reg['source']['source_id'],'expected_source_version':reg['source_version'],'idempotency_key':'reject'}
                http.call('/api/assets/handoff/ingest-runs',start_body,expect=409)
                assert process_identity(grandchild), 'default reject interrupted old tree'
                replace_started=time.monotonic()
                replacement=http.call('/api/assets/handoff/ingest-runs',dict(start_body,idempotency_key='replace',resource_policy='replace_after_stop'),expect=202)
                assert replacement['disposition']=='queued_waiting_drain',replacement
                next_task,new_scope=claim()
                try:client.call('begin_execution',{k:new_scope[k] for k in ('task_id','runtime_instance_id','execution_id','generation')})
                except ToolError as error:assert error.code in ('resource_busy','conflict')
                else:raise AssertionError('replacement began before the old tree drained')
                thread.join(timeout=20);assert not thread.is_alive(), 'old execution did not drain'
            assert result_holder[0]['outcome']=='abandoned',result_holder
            stop=next(event for event in stop_events if event['monotonic']>=replace_started)
            cancel_delay=stop['monotonic']-replace_started
            assert cancel_delay<=3,('online cancellation exceeded 3 seconds',stop)
            assert process_identity(grandchild) is None
            result=process_ingest(client,cfg,next_task,logs.append,threading.Event(),scope=new_scope,registry=registry)
            assert result['outcome']=='succeeded',result
            ack=next(e for e in events if e['tool']=='ack_execution_stopped' and e['execution_id']==old_scope['execution_id'])
            begin=next(e for e in events if e['tool']=='begin_execution' and e['execution_id']==new_scope['execution_id'])
            assert not ack['owned_pids_live'] and ack['owned_entries']==0 and ack['monotonic']<begin['monotonic']
            rows.append({'case':'replace_and_reject','old_execution_id':old_scope['execution_id'],'new_execution_id':new_scope['execution_id'],
                'reject_preserved_old_tree':True,'begin_before_drain_rejected':True,'ack_after_owned_tree_exit':True,
                'replacement_to_local_cancel_seconds':cancel_delay,
                'ack_to_new_begin_seconds':begin['monotonic']-ack['monotonic']})
            assert sha(media)==original and http.leaks()==[]
            write(work/'report.json',{'engineering':'passed','human_acceptance':'pending','fault_injection':'disk boundary and deliberately stalled owned child',
                'cases':rows,'events':events,'original_unchanged':True})
            (work/'worker.log').write_text('\n'.join(logs),encoding='utf-8');print('PASS:',work)
        finally:
            if plane and plane.poll() is None:plane.terminate();plane.wait(timeout=10)


if __name__=='__main__':main(Path(sys.argv[1]).resolve())
