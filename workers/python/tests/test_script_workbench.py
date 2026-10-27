import io
import json
import os
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest import mock

from vac_worker.content import ContentError
from vac_worker.script_workbench import call_model, generate, research


def request():
    return {'profile': {'sampling': {'max_frames': 1, 'max_bytes': 1000}, 'vision': {}, 'narration': {}},
            'document': {'paragraphs': [{'index': 0, 'instruction': '介绍抽卡机制', 'video': 'forced.mp4'}]},
            'media': [{'id': 'other', 'asset_id': 'a', 'name': 'other.mp4', 'path': 'private-other', 'in': 0, 'out': 1},
                      {'id': 'forced', 'asset_id': 'b', 'name': 'forced.mp4', 'path': 'private-forced', 'in': 0, 'out': 3}],
            'allow_web_research': True}


class ScriptWorkbenchTest(unittest.TestCase):
    def test_incomplete_or_oversized_stream_is_rejected_without_retry(self):
        class Response(io.BytesIO):
            headers={'Content-Type':'text/event-stream'}
        incomplete='data: '+json.dumps({'choices':[{'delta':{'content':'{}'},'finish_reason':None}]})+'\n\n'
        for payload in [incomplete.encode(),b'data: '+b'x'*65537+b'\n\n']:
            with self.subTest(size=len(payload)):
                seen=[]
                def opener(req,timeout):
                    seen.append(req);return Response(payload)
                with mock.patch.dict(os.environ,{'VAC_SCRIPT_TEST_TOKEN':'test-secret'}):
                    with self.assertRaises(ContentError):
                        call_model({'token_env':'VAC_SCRIPT_TEST_TOKEN','endpoint':'http://127.0.0.1:1234/v1','model':'step-5-preview','api_format':'openai'},'draw',[],opener=opener,stream=True)
                self.assertEqual(len(seen),1)

    def test_provider_timeout_and_failure_do_not_echo_credentials_or_response(self):
        for failure in [TimeoutError('test-secret private response'),RuntimeError('test-secret private response')]:
            with self.subTest(kind=type(failure).__name__):
                opener=mock.Mock(side_effect=failure)
                with mock.patch.dict(os.environ,{'VAC_SCRIPT_TEST_TOKEN':'test-secret'}):
                    with self.assertRaises(ContentError) as caught:
                        call_model({'token_env':'VAC_SCRIPT_TEST_TOKEN','endpoint':'http://127.0.0.1:1234/v1','model':'step-5-preview'},'draw',[],opener=opener)
                self.assertNotIn('test-secret',str(caught.exception))
                self.assertNotIn('private response',str(caught.exception))
                opener.assert_called_once()

    def test_provider_output_limit_is_used_unless_caller_overrides_it(self):
        seen=[]
        def opener(req, timeout):
            seen.append(json.loads(req.data))
            return io.BytesIO(json.dumps({'choices':[{'message':{'content':'{}'},'finish_reason':'stop'}]}).encode())
        spec={'token_env':'VAC_SCRIPT_TEST_TOKEN','endpoint':'http://127.0.0.1:1234/v1','model':'step-5-preview','api_format':'openai','max_output_tokens':24000}
        with mock.patch.dict(os.environ,{'VAC_SCRIPT_TEST_TOKEN':'test-secret'}):
            call_model(spec,'draw',[],opener=opener)
            call_model(spec,'draw',[],opener=opener,max_tokens=48000)
        self.assertEqual([x['max_tokens'] for x in seen],[24000,48000])

    def test_draw_model_streams_real_output_progress_and_preserves_high_effort(self):
        class Response(io.BytesIO):
            headers = {'Content-Type': 'text/event-stream'}
        chunks = [
            {'choices': [{'delta': {'reasoning_content': 'private reasoning'}, 'finish_reason': None}]},
            {'choices': [{'delta': {'content': '{"tool":"draw.'}, 'finish_reason': None}]},
            {'choices': [{'delta': {'content': 'list","arguments":{}}'}, 'finish_reason': None}]},
            {'choices': [{'delta': {}, 'finish_reason': 'stop'}]},
        ]
        stream = ''.join('data: '+json.dumps(x)+'\n\n' for x in chunks)+'data: [DONE]\n\n'
        seen=[];progress=[]
        def opener(req, timeout):
            seen.append((json.loads(req.data),timeout)); return Response(stream.encode())
        with mock.patch.dict(os.environ, {'VAC_SCRIPT_TEST_TOKEN': 'test-secret'}):
            result=call_model({'token_env':'VAC_SCRIPT_TEST_TOKEN','endpoint':'http://127.0.0.1:1234/v1','model':'step-5-preview','api_format':'openai','reasoning_effort':'high'},'draw',[],opener=opener,max_tokens=48000,timeout=600,stream=True,progress=progress.append)
        self.assertEqual(result,{'tool':'draw.list','arguments':{}})
        self.assertEqual(seen[0][0]['reasoning_effort'],'high')
        self.assertEqual(seen[0][0]['max_tokens'],48000)
        self.assertTrue(seen[0][0]['stream'])
        self.assertEqual(seen[0][1],600)
        self.assertGreater(progress[-1],0)
        self.assertTrue(all(isinstance(count,int) for count in progress))

    def test_truncated_stream_is_not_treated_as_complete_or_retried(self):
        class Response(io.BytesIO):
            headers={'Content-Type':'text/event-stream'}
        response='data: '+json.dumps({'choices':[{'delta':{'content':'{"tool":'},'finish_reason':'length'}]})+'\n\ndata: [DONE]\n\n'
        seen=[]
        def opener(req,timeout):
            seen.append(req);return Response(response.encode())
        with mock.patch.dict(os.environ,{'VAC_SCRIPT_TEST_TOKEN':'test-secret'}):
            with self.assertRaisesRegex(ContentError,'额度'):
                call_model({'token_env':'VAC_SCRIPT_TEST_TOKEN','endpoint':'http://127.0.0.1:1234/v1','model':'step-5-preview','api_format':'openai'},'draw',[],opener=opener,stream=True)
        self.assertEqual(len(seen),1)

    def test_generation_inspects_forced_visible_media_and_uses_actual_sources(self):
        calls = []
        sampled = []
        source = {'url': 'https://zh.wikipedia.org/wiki/抽卡', 'title': '抽卡', 'excerpt': '随机获得虚拟物品'}

        def model(spec, prompt, images):
            calls.append((prompt, images))
            self.assertNotIn('private-', prompt)
            self.assertNotIn('other.mp4', prompt)
            if images:
                return {'media': [{'id': 'forced', 'description': '展示抽卡结果'}]}
            self.assertIn('介绍抽卡机制', prompt)
            self.assertIn(source['url'], prompt)
            self.assertIn('actual generated Chinese narration', prompt)
            return {'segments': [{'index': 0, 'narration': '抽卡会随机获得角色或道具。', 'media_id': 'forced',
                                  'start': 0, 'end': 2, 'source_urls': [source['url']]}], 'questions': [],
                    'sources': [{'url': 'https://fabricated.invalid'}]}

        result = generate(request(), model=model, sampler=lambda m: sampled.append(m['id']) or 'aW1hZ2U=',
                          researcher=lambda q: [source])
        self.assertEqual(sampled, ['forced'])
        self.assertEqual(len(calls), 2)
        self.assertEqual(result['sources'], [source])
        self.assertNotEqual(result['segments'][0]['narration'], '介绍抽卡机制')

    def test_research_off_and_unknown_game_can_return_questions_without_fake_script(self):
        req = request()
        req['allow_web_research'] = False
        req['document']['paragraphs'][0]['instruction'] = '介绍这个游戏的限定卡池概率'
        researcher = mock.Mock(side_effect=AssertionError('research must stay off'))
        def model(spec, prompt, images):
            if images:
                return {'media': [{'id': 'forced', 'description': '无法确定具体游戏'}]}
            return {'segments': [], 'questions': ['请说明具体游戏和所需卡池规则。']}
        result = generate(req, model=model, sampler=lambda m: 'aW1hZ2U=', researcher=researcher)
        self.assertEqual(result['segments'], [])
        self.assertEqual(result['sources'], [])
        self.assertEqual(len(result['questions']), 1)
        researcher.assert_not_called()

    def test_narration_cannot_select_visible_but_uninspected_footage(self):
        def model(spec, prompt, images):
            if images:
                return {'media': [{'id': 'forced', 'description': '已查看画面'}]}
            return {'segments': [{'media_id': 'other'}], 'questions': []}
        with self.assertRaisesRegex(ContentError, '未查看'):
            generate(request(), model=model, sampler=lambda _: 'aW1hZ2U=', researcher=lambda _: [])

    def test_sampling_and_vision_mapping_are_enforced_before_narration(self):
        for result in [{'media': [{'id': 'forged', 'description': 'hidden'}]},
                       {'media': [{'id': 'forced', 'description': 'x'}] * 2}]:
            with self.subTest(result=result), self.assertRaises(ContentError):
                generate(request(), model=lambda *_: result, sampler=lambda _: 'x')
        req = request()
        req['profile']['sampling']['max_bytes'] = 1
        with self.assertRaises(ContentError):
            generate(req, model=mock.Mock(side_effect=AssertionError('no model call')), sampler=lambda _: 'x' * 20)

    def test_research_sends_only_query_and_no_provider_credentials(self):
        seen = []
        def opener(req, timeout):
            seen.append(req)
            return io.BytesIO(json.dumps({'query': {'pages': {'1': {'fullurl': 'https://zh.wikipedia.org/wiki/抽卡', 'title': '抽卡', 'extract': '知识'}}}}).encode())
        self.assertEqual(len(research('介绍抽卡机制', opener)), 1)
        self.assertEqual(seen[0].get_header('Authorization'), None)
        self.assertIn('gsrsearch=', seen[0].full_url)
        self.assertIsNone(seen[0].data)

    def test_provider_redirect_never_reaches_target(self):
        hits = []
        class Target(BaseHTTPRequestHandler):
            def do_GET(self): hits.append(self.path); self.send_response(200); self.end_headers()
            do_POST = do_GET
            def log_message(self, *_): pass
        target = ThreadingHTTPServer(('127.0.0.1', 0), Target)
        class Redirect(BaseHTTPRequestHandler):
            def do_POST(self):
                self.send_response(307)
                self.send_header('Location', f'http://127.0.0.1:{target.server_port}/stolen')
                self.end_headers()
            def log_message(self, *_): pass
        origin = ThreadingHTTPServer(('127.0.0.1', 0), Redirect)
        for server in (target, origin):
            threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            with mock.patch.dict(os.environ, {'VAC_SCRIPT_TEST_TOKEN': 'test-secret'}), self.assertRaises(ContentError):
                call_model({'token_env': 'VAC_SCRIPT_TEST_TOKEN', 'endpoint': f'http://127.0.0.1:{origin.server_port}', 'model': 'test'}, 'test', [])
            self.assertEqual(hits, [])
        finally:
            for server in (origin, target): server.shutdown(); server.server_close()
