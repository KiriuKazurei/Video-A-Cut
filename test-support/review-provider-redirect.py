"""Read-only review probe: two loopback servers, dummy credential, no external traffic."""
from pathlib import Path
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json, os, sys, threading

source=Path(sys.argv[1]); output=Path(os.environ['VAC_TEST_OUTPUT'])/'docs-review-20261016'
output.mkdir(parents=True,exist_ok=True)
sys.path.insert(0,str(source/'workers/python'))
from vac_worker.content import ClipEvidence, SceneDecision, make_content_provider

seen=[]
class Destination(BaseHTTPRequestHandler):
    def log_message(self,*args):pass
    def do_GET(self):
        seen.append({'method':self.command,'authorization_forwarded':self.headers.get('Authorization')=='Bearer review-dummy-token'})
        body=json.dumps({'narrations':[{'clip_index':0,'text':'测试解说','start':0.1,'end':0.8,'source_scene_label':'测试场景'}]}).encode()
        self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(body)
target=ThreadingHTTPServer(('127.0.0.1',0),Destination)
class Redirect(BaseHTTPRequestHandler):
    def log_message(self,*args):pass
    def do_POST(self):
        self.rfile.read(int(self.headers.get('Content-Length','0')))
        self.send_response(302);self.send_header('Location',f'http://127.0.0.1:{target.server_port}/not-the-configured-provider');self.end_headers()
origin=ThreadingHTTPServer(('127.0.0.1',0),Redirect)
threads=[threading.Thread(target=s.serve_forever,daemon=True) for s in (target,origin)]
for t in threads:t.start()
os.environ['VAC_REVIEW_DUMMY']='review-dummy-token'
error=None
try:
    provider=make_content_provider('narration',endpoint=f'http://127.0.0.1:{origin.server_port}/narration',token_env='VAC_REVIEW_DUMMY',allow_external=True)
    provider.narrate([ClipEvidence(0,'clip.mp4',1,0,1,0)],[SceneDecision(0,'测试场景','model',0.9)])
except Exception as exc:error=type(exc).__name__+': '+str(exc)
finally:
    for s in (origin,target):s.shutdown();s.server_close()
    os.environ.pop('VAC_REVIEW_DUMMY',None)
result={'probe':'custom-provider-redirect','unexpected_destination_requests':seen,'provider_error':error,'reproduced':bool(seen),'scope':'two loopback servers and a dummy credential only'}
(output/'provider-redirect.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
print(json.dumps(result,ensure_ascii=False,indent=2))
