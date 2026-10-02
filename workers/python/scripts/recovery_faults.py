"""Loopback-only MCP fault proxy; never persists authorization headers."""
import json, socket, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.request import Request, urlopen
from urllib.error import HTTPError


class RecoveryFaultProxy:
    def __init__(self, target):
        self.target = target
        self.scan_event = threading.Event()
        self.sealed_event = threading.Event()
        self.armed_sealed = False
        self.sealed_args = None
        self.armed_scan = False
        self.offline = False
        self.dropped_commit = None
        self.scan_args = None
        self.requests = []
        self.guard = threading.Lock()
        fixture = self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self,*args): pass
            def disconnect(self):
                self.close_connection=True
                try:self.connection.shutdown(socket.SHUT_RDWR)
                except OSError:pass
                self.connection.close()
            def do_POST(self):
                raw = self.rfile.read(int(self.headers['Content-Length']))
                message = json.loads(raw)
                params = message.get('params',{})
                name, args = params.get('name'), params.get('arguments',{})
                with fixture.guard:
                    fixture.requests.append({'name':name,'execution_id':args.get('execution_id'),'kind':args.get('kind'),'item_index':args.get('item_index')})
                    interrupted = name=='save_ingest_checkpoint' and args.get('kind')=='scan_chunk' and fixture.armed_scan
                    if interrupted:
                        fixture.armed_scan=False
                        fixture.scan_args=dict(args)
                        fixture.scan_event.set()
                    sealed = name=='submit_ingest_result' and fixture.armed_sealed
                    if sealed:
                        fixture.armed_sealed=False
                        fixture.sealed_args=dict(args)
                        fixture.sealed_event.set()
                if interrupted or sealed or fixture.offline:
                    self.disconnect()
                    return
                headers = {k:v for k,v in self.headers.items() if k.lower() not in ('host','content-length','connection')}
                try:
                    with urlopen(Request(fixture.target+'/mcp',data=raw,headers=headers),timeout=10) as response:
                        output,status,content_type=response.read(),response.status,response.headers.get('Content-Type','application/json')
                except HTTPError as err:
                    output,status,content_type=err.read(),err.code,'application/json'
                except OSError:
                    self.disconnect()
                    return
                if fixture.offline:
                    self.disconnect()
                    return
                with fixture.guard:
                    committed = name=='submit_ingest_result' and fixture.dropped_commit is None and status==200 and not json.loads(output).get('result',{}).get('isError')
                    if committed: fixture.dropped_commit=dict(args)
                if committed:
                    self.disconnect()
                    return
                try:
                    self.send_response(status);self.send_header('Content-Type',content_type)
                    self.send_header('Content-Length',str(len(output)));self.end_headers();self.wfile.write(output)
                except (BrokenPipeError,ConnectionResetError,ConnectionAbortedError): pass
        self.server=ThreadingHTTPServer(('127.0.0.1',0),Handler)
        threading.Thread(target=self.server.serve_forever,daemon=True).start()
        self.url=f'http://127.0.0.1:{self.server.server_port}/mcp'

    def close(self):
        self.server.shutdown();self.server.server_close()
