"""Real Edge acceptance against isolated copies of actual E2E state snapshots.

Run verify_phase7_ingest.py --browser-fixtures first; pass its evidence directory.
No edits to the source database, no borrowed browser profile or human pass.
"""
import json, os, socket, sqlite3, subprocess, sys, time, uuid
from pathlib import Path
from urllib.request import Request, urlopen

repo=Path(__file__).resolve().parents[3]
source=Path(sys.argv[1]).resolve()
assert source.parent==repo/'.run-data' and source.name.startswith('phase7-')
work=repo/'.run-data'/('phase7-browser-'+uuid.uuid4().hex[:8]);work.mkdir()
subprocess.run(['go','build','-o',str(work/'control-plane.exe'),'.'],cwd=repo/'control-plane',check=True)

def port():
    with socket.socket() as s: s.bind(('127.0.0.1',0));return s.getsockname()[1]

def ready(url,proc):
    for _ in range(100):
        if proc.poll() not in (None,0):raise RuntimeError(f'owned process exited during startup: {proc.returncode}')
        try:urlopen(url,timeout=1).close();return
        except OSError:time.sleep(.1)
    raise RuntimeError('startup timed out')

edge=Path(os.environ['ProgramFiles(x86)'])/'Microsoft/Edge/Application/msedge.exe'
reports=[]
for mode in os.environ.get('VAC_BROWSER_MODES', 'probe,segment,paging,failed,ready,scene,draft,governance').split(','):
    owned=[];hp,dp=port(),port()
    cfg=json.loads((source/'control.json').read_text(encoding='utf-8'));cfg['http_addr']=f'127.0.0.1:{hp}'
    config=work/f'{mode}-control.json';config.write_text(json.dumps(cfg),encoding='utf-8')
    fixture = 'ready' if mode in ('scene','draft','governance') else mode
    with sqlite3.connect(f'file:{(source/f"browser-{fixture}.db").as_posix()}?mode=ro',uri=True) as original:
        with sqlite3.connect(work/f'{mode}.db') as copy:original.backup(copy)
    with (work/f'{mode}-processes.log').open('w',encoding='utf-8') as log:
        try:
            plane=subprocess.Popen([str(work/'control-plane.exe'),'-config',str(config),'-db',str(work/f'{mode}.db')],stdout=log,stderr=log,creationflags=subprocess.CREATE_NO_WINDOW);owned.append(plane)
            base=f'http://127.0.0.1:{hp}';ready(base+'/api/assets',plane)
            profile=json.loads((repo/'configs/phase6/profile.builtin.example.json').read_text(encoding='utf-8'))
            profile.update(profile_id='browser_fixture',name='浏览器自动验收')
            req=Request(base+'/api/processing-profiles',data=json.dumps({'profile':profile,'expected_revision':0,'idempotency_key':'browser-profile'}).encode(),headers={'Content-Type':'application/json'})
            urlopen(req,timeout=10).close()
            browser=subprocess.Popen([str(edge),'--headless=new','--disable-gpu','--no-first-run',f'--user-data-dir={work/(mode+"-profile")}',f'--remote-debugging-port={dp}','about:blank'],stdout=log,stderr=log,creationflags=subprocess.CREATE_NO_WINDOW);owned.append(browser)
            ready(f'http://127.0.0.1:{dp}/json/list',browser)
            subprocess.run(['node',str(repo/'webui/browser_acceptance.mjs'),base,f'http://127.0.0.1:{dp}',str(work),mode],check=True,timeout=180)
            reports.append(json.loads((work/f'{mode}-report.json').read_text()))
        finally:
            for child in reversed(owned):
                if child.poll() is None:
                    subprocess.run(['taskkill','/T','/F','/PID',str(child.pid)],capture_output=True,timeout=15);child.wait(timeout=10)
            (work/f'{mode}-cleanup.json').write_text(json.dumps([{'pid':p.pid,'exited':p.poll() is not None} for p in owned]))
(work/'report.json').write_text(json.dumps({'engineering':'passed','human_acceptance':'pending','modes':reports},ensure_ascii=False,indent=2),encoding='utf-8')
print('PASS:',work)
