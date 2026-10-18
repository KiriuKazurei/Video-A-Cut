"""Exercise the default custom-provider transport against loopback HTTP servers."""
import hashlib
import json
import os
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

from vac_worker.content import ClipEvidence, ContentError, SceneDecision, make_content_provider


class CustomProviderRedirectTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        image = b'controlled-frame-bytes'
        (root / 'frame.jpg').write_bytes(image)
        self.clips = [ClipEvidence(0, 'clip.mp4', 2, 0, 2, 0,
                      ({'path': 'frame.jpg', 'sha256': hashlib.sha256(image).hexdigest(), 'timestamp': .5},), str(root))]
        self.scenes = [SceneDecision(0, 'scene', 'model', .9)]
        self.calls = []
        calls = self.calls

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args): pass

            def respond(self):
                body = self.rfile.read(int(self.headers.get('Content-Length', '0')))
                calls.append((self.server.server_port, self.path, self.command,
                              self.headers.get('Authorization'), body))
                if self.path.startswith('/redirect/'):
                    _, _, code, destination = self.path.split('/')
                    port = self.server.server_port if destination == 'same' else target.server_port
                    self.send_response(int(code))
                    self.send_header('Location', f'http://127.0.0.1:{port}/unconfigured')
                    self.end_headers()
                    return
                response = {'scenes': [{'clip_index': 0, 'label': 'scene', 'confidence': .9}],
                            'narrations': [{'clip_index': 0, 'text': 'narration', 'start': .2, 'end': 1.2}]}
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.end_headers()
                self.wfile.write(json.dumps(response).encode())

            do_GET = respond
            do_POST = respond

        target = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.origin = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        for server in (target, self.origin):
            thread = threading.Thread(target=lambda s=server: s.serve_forever(poll_interval=.01), daemon=True)
            thread.start()
            self.addCleanup(thread.join)
            self.addCleanup(server.server_close)
            self.addCleanup(server.shutdown)
        self.env = patch.dict(os.environ, {'TEST_VAC_REDIRECT_TOKEN': 'dummy-redirect-token'})
        self.env.start()
        self.addCleanup(self.env.stop)

    def request(self, kind, path):
        provider = make_content_provider(kind, endpoint=f'http://127.0.0.1:{self.origin.server_port}{path}',
                                         token_env='TEST_VAC_REDIRECT_TOKEN', allow_external=True, timeout_seconds=2)
        return provider.recognize(self.clips) if kind == 'vision' else provider.narrate(self.clips, self.scenes)

    def test_redirects_do_not_send_credentials_or_payload_to_another_path_or_origin(self):
        for kind in ('vision', 'narration'):
            for code in (301, 302, 303, 307, 308):
                for destination in ('same', 'cross'):
                    with self.subTest(kind=kind, code=code, destination=destination):
                        self.calls.clear()
                        with self.assertRaisesRegex(ContentError, 'redirect') as error:
                            self.request(kind, f'/redirect/{code}/{destination}')
                        self.assertNotIn('dummy-redirect-token', str(error.exception))
                        self.assertEqual(len(self.calls), 1, 'redirect destination must receive zero requests')
                        self.assertEqual(self.calls[0][2:4], ('POST', 'Bearer dummy-redirect-token'))

    def test_direct_custom_provider_requests_still_succeed(self):
        for kind in ('vision', 'narration'):
            with self.subTest(kind=kind):
                self.calls.clear()
                self.assertEqual(len(self.request(kind, '/configured')), 1)
                self.assertEqual(len(self.calls), 1)
                self.assertEqual(self.calls[0][2:4], ('POST', 'Bearer dummy-redirect-token'))
                body = json.loads(self.calls[0][4])
                self.assertIn('model', body)
                if kind == 'vision':
                    self.assertIn('image_base64', body['clips'][0]['frames'][0])
