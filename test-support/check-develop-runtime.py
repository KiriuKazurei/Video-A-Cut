"""Run the merged binary locally with disposable data and its freshly built UI."""
import argparse
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', type=Path, required=True)
    options = parser.parse_args()
    source = Path(__file__).resolve().parents[1]
    output = Path(os.environ['VAC_TEST_OUTPUT']) / 'develop-merge-20261018'
    runtime = output / 'runtime'
    delivery = runtime / 'delivery'
    delivery.mkdir(parents=True, exist_ok=True)
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        port = listener.getsockname()[1]
    config = {
        'http_addr': f'127.0.0.1:{port}',
        'delivery_root': str(delivery),
        'web_root': str(output / 'dist'),
        'tts_python': sys.executable,
        'tts_worker_dir': str(source / 'workers/python'),
    }
    config_path = runtime / 'control.json'
    config_path.write_text(json.dumps(config), encoding='utf-8')
    base = f'http://127.0.0.1:{port}'

    def request(path, body=None):
        raw = None if body is None else json.dumps(body).encode('utf-8')
        req = urllib.request.Request(base + path, data=raw)
        if body is not None:
            req.add_header('Content-Type', 'application/json')
        with urllib.request.urlopen(req, timeout=15) as response:
            return response.read()

    with (runtime / 'process.log').open('w', encoding='utf-8') as log:
        process = subprocess.Popen(
            [str(options.binary), '-config', str(config_path),
             '-db', str(runtime / 'vac.db'), '-desktop'],
            cwd=source / 'control-plane', stdin=subprocess.PIPE,
            stdout=log, stderr=log, text=True,
            creationflags=getattr(subprocess, 'CREATE_NO_WINDOW', 0),
        )
        try:
            deadline = time.monotonic() + 20
            while True:
                if process.poll() is not None:
                    raise RuntimeError(f'Control plane exited: {process.returncode}')
                try:
                    visible = json.loads(request('/api/visible-set'))
                    break
                except (urllib.error.URLError, TimeoutError):
                    if time.monotonic() >= deadline:
                        raise
                    time.sleep(0.2)
            assert isinstance(json.loads(request('/api/assets')), list)
            confirmed = json.loads(request('/api/visible-set/confirm', {
                'visible_set_sha': visible['sha256'],
            }))
            assert confirmed['confirmed'] is True
            parsed = json.loads(request('/api/script-workbench/parse', {
                'body': '{黑屏}合并后本机运行检查',
            }))
            assert parsed['paragraphs'][0]['effect'] == '黑屏'
            fonts = json.loads(request('/api/script-workbench/fonts'))
            assert isinstance(fonts, list) and fonts
            assert isinstance(json.loads(request('/api/script-workbench/media')), list)
            page = request('/').decode('utf-8')
            script = re.search(r'src="([^"]+\.js)"', page)
            assert script and len(request(script[1])) > 1000
            process.communicate('{"command":"shutdown"}\n', timeout=10)
            assert process.returncode == 0
        finally:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=10)
    result = {
        'status': 'passed', 'binary': str(options.binary),
        'checks': ['process_start_and_shutdown', 'visible_set_confirmation',
                   'script_parse', 'node_font_bridge', 'media_catalogue',
                   'built_ui_and_javascript_served'],
        'manual_full_flow': 'not_started',
    }
    (output / 'runtime-check.json').write_text(
        json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8',
    )
    print(json.dumps(result, ensure_ascii=False))


if __name__ == '__main__':
    main()
