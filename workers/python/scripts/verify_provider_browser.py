"""Provider editor in a real isolated Edge session, with synthetic provider APIs."""
import base64
import io
import json
import os
import socket
import sqlite3
import subprocess
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.request import urlopen

repo = Path(__file__).resolve().parents[3]
work = repo / '.run-data' / ('provider-integration-' + uuid.uuid4().hex[:8])
work.mkdir()
source = repo / '.run-data/phase7-recovery-7cc511c1'

def port():
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0)); return s.getsockname()[1]

def ready(url, process):
    for _ in range(100):
        if process.poll() is not None: raise RuntimeError('owned process exited')
        try: urlopen(url, timeout=1).close(); return
        except OSError: time.sleep(.1)
    raise RuntimeError('startup timeout')

requests = []
class Mock(BaseHTTPRequestHandler):
    def log_message(self, *_): pass
    def do_GET(self):
        if self.path.startswith('/v1beta/models'):
            response = {'models': [{'name': 'models/test-model', 'displayName': 'Synthetic model', 'supportedGenerationMethods': ['generateContent']}, {'name': 'models/other-model', 'supportedGenerationMethods': ['generateContent']}]}
        else: response = {'data': [{'id': 'test-model', 'display_name': 'Synthetic model'}, {'id': 'other-model'}], 'has_more': False}
        self.send_response(200); self.end_headers(); self.wfile.write(json.dumps(response).encode())
        requests.append({'operation': 'models', 'path': self.path})
    def do_POST(self):
        data = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        key = self.headers.get('Authorization', '').removeprefix('Bearer ') or self.headers.get('x-api-key') or self.headers.get('x-goog-api-key')
        if key != 'browser-test-key':
            self.send_response(401); self.end_headers(); self.wfile.write(b'{"error":"invalid-browser-key"}'); return
        if data.get('model') == 'slow-model' or '/slow-model:' in self.path: time.sleep(.7)
        if self.path.endswith('/chat/completions'):
            response = {'choices': [{'message': {'content': 'green'}, 'finish_reason': 'stop'}]}
        elif self.path.endswith('/messages'):
            response = {'content': [{'type': 'text', 'text': 'green'}], 'stop_reason': 'end_turn'}
        else: response = {'candidates': [{'content': {'parts': [{'text': 'green'}]}, 'finishReason': 'STOP'}]}
        requests.append({'operation': 'test', 'path': self.path})
        self.send_response(200); self.end_headers()
        try: self.wfile.write(json.dumps(response).encode())
        except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError): pass

if __name__ == '__main__':
    owned = []; server = ThreadingHTTPServer(('127.0.0.1', 0), Mock)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        env = os.environ.copy(); env['GOCACHE'] = str(repo / '.run-data/go-cache')
        binary = work / 'control-plane.exe'
        subprocess.run(['go', 'build', '-o', str(binary), '.'], cwd=repo/'control-plane', env=env, check=True, timeout=120)
        hp, dp = port(), port()
        cfg = json.loads((source/'control.json').read_text(encoding='utf-8')); cfg['http_addr'] = f'127.0.0.1:{hp}'
        (work/'browser-control.json').write_text(json.dumps(cfg), encoding='utf-8')
        db = work / ('browser-' + str(hp) + '.db')
        with sqlite3.connect(f'file:{(source/"browser-ready.db").as_posix()}?mode=ro', uri=True) as original:
            with sqlite3.connect(db) as copy: original.backup(copy)
        with (work/'browser-processes.log').open('w', encoding='utf-8') as log:
            flags = getattr(subprocess, 'CREATE_NO_WINDOW', 0)
            plane = subprocess.Popen([str(binary), '-config', str(work/'browser-control.json'), '-db', str(db)], stdout=log, stderr=log, creationflags=flags); owned.append(plane)
            base = f'http://127.0.0.1:{hp}'; ready(base+'/api/assets', plane)
            edge = Path(os.environ['ProgramFiles(x86)'])/'Microsoft/Edge/Application/msedge.exe'
            # The temporary browser loads only this isolated localhost fixture.
            # Avoid nested Chromium sandboxes inside the managed Windows token.
            browser = subprocess.Popen([str(edge), '--headless=new', '--no-sandbox', '--disable-gpu', '--in-process-gpu', '--disable-extensions', '--disable-background-networking', '--no-first-run', f'--user-data-dir={work/("edge-"+str(dp))}', f'--remote-debugging-port={dp}', 'about:blank'], stdout=log, stderr=log, creationflags=flags); owned.append(browser)
            ready(f'http://127.0.0.1:{dp}/json/list', browser)
            subprocess.run(['node', str(repo/'webui/provider_browser_acceptance.mjs'), base, f'http://127.0.0.1:{dp}', str(work), f'http://127.0.0.1:{server.server_port}'], check=True, timeout=180)
        (work/'browser-requests.json').write_text(json.dumps(requests, indent=2), encoding='utf-8')
        assert b'browser-test-key' not in db.read_bytes()
        print('PASS: real Edge provider editor, three formats, four widths, saved secret-free profile.')
    finally:
        server.shutdown(); server.server_close()
        for child in reversed(owned):
            if child.poll() is None:
                child.terminate()
                try: child.wait(timeout=10)
                except subprocess.TimeoutExpired: child.kill(); child.wait(timeout=10)
        (work/'browser-cleanup.json').write_text(json.dumps([{'pid':p.pid,'exited':p.poll() is not None} for p in owned]), encoding='utf-8')
