import base64
import hashlib
import io
import json
import os
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

from vac_worker.content import ClipEvidence, ContentError, SceneDecision, make_content_provider
from vac_worker.preparation import configured_task
from vac_worker.provider_http import api_base, response_text
from vac_worker.worker import Config


class ProviderHTTPTests(unittest.TestCase):
    def test_real_http_three_formats_vision_and_narration(self):
        for format in ('openai', 'anthropic', 'gemini'):
            with self.subTest(format=format), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                # Payload integrity, framing and transport are tested; production sampling tests verify JPEG decoding.
                image = b'controlled-image-bytes'
                (root / 'frame.jpg').write_bytes(image)
                digest = hashlib.sha256(image).hexdigest()
                clips = [ClipEvidence(0, 'clip.mp4', 2, 0, 2, 0,
                         ({'path': 'frame.jpg', 'sha256': digest, 'timestamp': .5},), str(root))]
                calls = []
                outer = self
                class Handler(BaseHTTPRequestHandler):
                    def log_message(self, *args): pass
                    def do_POST(self):
                        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                        calls.append((self.path, body))
                        if format == 'openai':
                            outer.assertEqual(self.headers['Authorization'], 'Bearer test-key')
                            outer.assertEqual(self.path, '/v1/chat/completions')
                            parts = body['messages'][0]['content']
                            images = [base64.b64decode(p['image_url']['url'].split(',', 1)[1]) for p in parts if p['type'] == 'image_url']
                            text = parts[0]['text']
                        elif format == 'anthropic':
                            outer.assertEqual(self.headers['x-api-key'], 'test-key')
                            outer.assertEqual(self.headers['anthropic-version'], '2023-06-01')
                            outer.assertEqual(self.path, '/v1/messages')
                            parts = body['messages'][0]['content']
                            images = [base64.b64decode(p['source']['data']) for p in parts if p['type'] == 'image']
                            text = parts[0]['text']
                        else:
                            outer.assertEqual(self.headers['x-goog-api-key'], 'test-key')
                            outer.assertEqual(self.path, '/v1beta/models/test-model:generateContent')
                            parts = body['contents'][0]['parts']
                            images = [base64.b64decode(p['inlineData']['data']) for p in parts if 'inlineData' in p]
                            text = parts[0]['text']
                        outer.assertNotIn(str(root), text)
                        if images:
                            outer.assertEqual(images, [image]); outer.assertIn(digest, text)
                            answer = {'scenes': [{'clip_index': 0, 'label': '游戏场景', 'confidence': .8, 'sequence_rank': 0}]}
                        else:
                            outer.assertIn('游戏场景', text)
                            answer = {'narrations': [{'clip_index': 0, 'text': '继续前进', 'start': .2, 'end': 1.2, 'needs_review': False}]}
                        answer = json.dumps(answer, ensure_ascii=False)
                        if format == 'openai': response = {'choices': [{'message': {'content': answer}, 'finish_reason': 'stop'}], 'model': 'resolved-model'}
                        elif format == 'anthropic': response = {'content': [{'type': 'text', 'text': answer}], 'model': 'resolved-model', 'stop_reason': 'end_turn'}
                        else: response = {'candidates': [{'content': {'parts': [{'text': answer}]}, 'finishReason': 'STOP'}], 'modelVersion': 'resolved-model'}
                        self.send_response(200); self.send_header('Content-Type', 'application/json'); self.end_headers()
                        self.wfile.write(json.dumps(response).encode())
                server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
                thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
                try:
                    kwargs = dict(api_format=format, endpoint=f'http://127.0.0.1:{server.server_port}',
                                  model='models/test-model' if format == 'gemini' else 'test-model',
                                  token_env='TEST_VAC_STANDARD_KEY', allow_external=True)
                    with patch.dict(os.environ, {'TEST_VAC_STANDARD_KEY': 'test-key'}):
                        scenes = make_content_provider('vision', **kwargs).recognize(clips)
                        lines = make_content_provider('narration', **kwargs).narrate(clips, scenes)
                    self.assertEqual(scenes[0].evidence_frames, (digest,))
                    self.assertEqual(scenes[0].method, format + '_vision')
                    self.assertEqual(lines[0].model_version, 'resolved-model')
                    self.assertTrue(lines[0].needs_review)
                    self.assertEqual(len(calls), 2)
                finally:
                    server.shutdown(); server.server_close(); thread.join()

    def test_transport_errors_redact_key_and_do_not_redirect(self):
        requests = []
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args): pass
            def do_POST(self):
                requests.append(self.path)
                self.send_response(307); self.send_header('Location', '/secret-key-destination'); self.end_headers()
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
        try:
            with patch.dict(os.environ, {'TEST_VAC_STANDARD_KEY': 'secret-key'}):
                provider = make_content_provider('narration', api_format='openai', endpoint=f'http://127.0.0.1:{server.server_port}',
                                                model='m', token_env='TEST_VAC_STANDARD_KEY', allow_external=True)
                with self.assertRaises(ContentError) as ctx:
                    provider.narrate([ClipEvidence(0, 'c.mp4', 2, 0, 2, 0)], [SceneDecision(0, 'scene', 'model')])
                self.assertNotIn('secret-key', str(ctx.exception))
                self.assertIn('redirect', str(ctx.exception))
            self.assertEqual(requests, ['/v1/chat/completions'])
        finally:
            server.shutdown(); server.server_close(); thread.join()

    def test_model_output_invalid_truncated_or_blocked_fails_closed(self):
        for format, payload in [
            ('openai', {'choices': [{'message': {'content': 'partial'}, 'finish_reason': 'length'}]}),
            ('anthropic', {'content': [{'type': 'text', 'text': 'partial'}], 'stop_reason': 'max_tokens'}),
            ('gemini', {'candidates': [{'finishReason': 'SAFETY', 'content': {'parts': [{'text': 'blocked'}]}}]}),
            ('gemini', {'candidates': []}), ('openai', {'choices': [{'message': {'content': None}}]})]:
            with self.subTest(format=format), self.assertRaises(ContentError): response_text(format, payload)
        provider = make_content_provider('narration', api_format='openai', endpoint='http://127.0.0.1:1234',
                    model='m', token_env='TEST_VAC_STANDARD_KEY', allow_external=True,
                    opener=lambda *a, **kw: io.BytesIO(json.dumps({'choices': [{'message': {'content': json.dumps(
                        {'narrations': [{'clip_index': 0, 'text': 'bad timing', 'start': 1.8, 'end': 9}]})}}]}).encode()))
        with patch.dict(os.environ, {'TEST_VAC_STANDARD_KEY': 'test-key'}), self.assertRaises(ContentError):
            provider.narrate([ClipEvidence(0, 'c.mp4', 2, 0, 2, 0)], [SceneDecision(0, 'scene', 'model')])

    def test_permission_and_fixed_profile_format_mapping(self):
        cfg = Config('http://local/mcp', 'test', Path('.'), role='recognizer', profile_sha256='a'*64)
        p = {'vision': {'adapter': 'vision', 'api_format': 'anthropic', 'endpoint': 'https://example.com/v1',
                       'model': 'm', 'token_env': 'TEST_VAC_STANDARD_KEY'},
             'sampling': {'max_frames': 3, 'max_bytes': 1024, 'timeout_seconds': 2}, 'tts_voice': 'selected'}
        mapped = configured_task(cfg, {'processing_profile': p, 'profile_sha256': 'a'*64})
        self.assertEqual(mapped.content_provider_config['api_format'], 'anthropic')
        provider = make_content_provider('vision', api_format='gemini', endpoint='https://example.com/v1beta',
                                        allow_external=False, opener=lambda *a, **k: self.fail('unauthorized request'))
        with self.assertRaises(ContentError): provider.recognize([])
        for endpoint in ('http://remote.test', 'https://user:secret@host/v1', 'https://host/v1?key=secret'):
            with self.assertRaises(ContentError): api_base(endpoint, 'openai')
        self.assertEqual(api_base('https://host/proxy/v1/chat/completions', 'openai'), 'https://host/proxy/v1')

    def test_openai_legacy_token_parameter_retries_only_rejected_request(self):
        import urllib.error
        calls = []
        def opener(request, **kwargs):
            body = json.loads(request.data); calls.append(body)
            if len(calls) == 1:
                raise urllib.error.HTTPError(request.full_url, 400, 'Bad Request', {},
                    io.BytesIO(b'{"error":{"param":"max_completion_tokens"}}'))
            return io.BytesIO(json.dumps({'choices': [{'message': {'content': json.dumps(
                {'narrations': [{'clip_index': 0, 'text': 'test', 'start': .2, 'end': 1.2}]})}}]}).encode())
        provider = make_content_provider('narration', api_format='openai', endpoint='http://127.0.0.1:1234',
                       model='m', token_env='TEST_VAC_STANDARD_KEY', allow_external=True, opener=opener)
        with patch.dict(os.environ, {'TEST_VAC_STANDARD_KEY': 'test-key'}):
            self.assertEqual(len(provider.narrate([ClipEvidence(0, 'c.mp4', 2, 0, 2, 0)], [SceneDecision(0, 'scene', 'model')])), 1)
        self.assertEqual(len(calls), 2)
        self.assertIn('max_completion_tokens', calls[0]); self.assertNotIn('max_completion_tokens', calls[1])
        self.assertEqual(calls[0]['max_completion_tokens'], calls[1]['max_tokens'])
