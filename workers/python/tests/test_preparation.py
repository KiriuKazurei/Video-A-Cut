import unittest
from pathlib import Path
from vac_worker.worker import Config
from vac_worker.preparation import configured_task
from vac_worker.stages import StageFailure, _sapi_script

class PreparationTests(unittest.TestCase):
    def test_operator_timeout_reaches_native_generation_transport(self):
        import io,json,os
        from unittest.mock import patch
        from vac_worker.content import make_content_provider,ClipEvidence,SceneDecision
        cfg=Config('http://local/mcp','test',Path('.'),role='narrator',profile_sha256='a'*64,provider_timeout_seconds=120)
        p={'narration':{'adapter':'narration','api_format':'openai','endpoint':'https://example.com/v1',
                       'model':'m','token_env':'TEST_PREPARATION_KEY'},
           'sampling':{'max_frames':3,'max_bytes':1024,'timeout_seconds':2},'tts_voice':'selected'}
        mapped=configured_task(cfg,{'processing_profile':p,'profile_sha256':'a'*64})
        calls=[]
        def open_response(request,timeout):
            calls.append(timeout)
            content={'narrations':[{'clip_index':0,'text':'画面显示角色','start':0.2,'end':1.8,'source_scene_label':'角色'}]}
            return io.BytesIO(json.dumps({'choices':[{'message':{'content':json.dumps(content)},'finish_reason':'stop'}]}).encode())
        provider=make_content_provider(mapped.content_provider,opener=open_response,**mapped.content_provider_config)
        with patch.dict(os.environ,{'TEST_PREPARATION_KEY':'test-only'}):
            result=provider.narrate([ClipEvidence(0,'c.mp4',2,0,2,0)],[SceneDecision(0,'角色','model')])
        self.assertEqual(calls,[120])
        self.assertEqual(len(result),1)
        self.assertEqual(mapped.sampling_limits.max_total_time,2)
        self.assertTrue(result[0].needs_review)

    def test_provider_timeout_defaults_and_rejects_unbounded_config(self):
        import json,tempfile
        from vac_worker.worker import load_config
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'worker.json'
            base={'mcp_url':'http://local/mcp','delivery_root':str(Path(directory).resolve()),'token_env':'TEST_TOKEN'}
            path.write_text(json.dumps(base),encoding='utf-8')
            self.assertEqual(load_config(str(path),{'TEST_TOKEN':'test'}).provider_timeout_seconds,30)
            for value in (0,301,True,'120',None,float('nan'),float('inf')):
                with self.subTest(value=value),self.assertRaisesRegex(ValueError,'provider_timeout_seconds'):
                    path.write_text(json.dumps({**base,'provider_timeout_seconds':value}),encoding='utf-8')
                    load_config(str(path),{'TEST_TOKEN':'test'})
    def test_fixed_profile_maps_budget_and_voice(self):
        cfg=Config('http://local/mcp','test',Path('.'),role='narrator',profile_sha256='a'*64,tts_auto_approve=True)
        p={'narration':{'adapter':'builtin'},'sampling':{'max_frames':3,'max_bytes':1024,'timeout_seconds':2},'tts_voice':'selected'}
        mapped=configured_task(cfg,{'processing_profile':p,'profile_sha256':'a'*64})
        self.assertEqual(mapped.sampling_limits.max_total_frames,3)
        self.assertEqual(mapped.tts_voice,'selected')
        self.assertFalse(mapped.tts_auto_approve)
        self.assertIn('Selected SAPI voice unavailable',_sapi_script('测试',Path('out.wav'),'selected'))
    def test_mismatch_refuses_before_execution(self):
        cfg=Config('http://local/mcp','test',Path('.'),profile_sha256='a'*64)
        with self.assertRaises(StageFailure):configured_task(cfg,{'processing_profile':{'narration':{}},'profile_sha256':'b'*64})

    def test_idle_capability_heartbeat_recovers_transport_failure(self):
        import threading
        from unittest.mock import patch
        from vac_worker.worker import run_loop, REQUIRED_TOOLS
        from vac_worker.mcp import TransportError
        class Client:
            beats=0
            claims=0
            def list_tools(self): return list(REQUIRED_TOOLS)
            def call(self,name,args):
                if name=='heartbeat':
                    self.beats+=1
                    if self.beats==2: raise TransportError('temporary disconnect')
                    return {}
                self.claims+=1
                return {'claimed':False}
        client=Client();cfg=Config('http://local/mcp','test',Path('.'),profile_sha256='a'*64,poll_interval=0.001)
        with patch('vac_worker.preparation.local_capability',return_value={'profile_sha256':'a'*64}),patch('vac_worker.worker.time.monotonic',side_effect=[0,6,12,12]):
            result=run_loop(client,cfg,lambda _:None,threading.Event(),once=True)
        self.assertEqual(result['outcome'],'idle')
        self.assertEqual(client.beats,3)
        self.assertEqual(client.claims,1)
