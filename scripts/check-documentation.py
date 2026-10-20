"""Check authored documentation links and the current HTTP inventory, without reading secrets."""
from pathlib import Path
import argparse, hashlib, json, os, re, subprocess
from urllib.parse import unquote

parser=argparse.ArgumentParser()
parser.add_argument('--root',type=Path,action='append',required=True)
args=parser.parse_args()
excluded={'.git','.run-data','node_modules','target','dist','build','release','__pycache__','.agents','.codex','.hermes','.venv','venv','legacy','upstream','builtin'}
primary={'项目文档.md','计划文档.md','接口文档.md','架构与功能详解.md'}
errors=[]; counts={'documents':0,'historical':0,'links':0,'routes':0}
current={}
for root in args.root:
    authored=[]
    for folder,dirs,names in os.walk(root,followlinks=False):
        dirs[:]=[d for d in dirs if d not in excluded and not (Path(folder)/d).is_symlink() and not (Path(folder)/d).is_junction()]
        authored += [Path(folder)/n for n in names if Path(n).suffix.lower() in {'.md','.mdx','.txt'} and not n.startswith('requirements') and n != 'CMakeLists.txt']
    for path in authored:
        body=path.read_text(encoding='utf-8-sig');counts['documents']+=1
        historical=body.startswith('<!-- documentation-history-status:')
        if historical:
            counts['historical']+=1; checked=body.split('\n\n',1)[0]
        else:
            rel=path.relative_to(root).as_posix()
            if rel in current and current[rel]!=body:errors.append('Current document differs between workspaces: '+rel)
            current[rel]=body
            if body.count('```')%2:errors.append('Unbalanced code fence: '+str(path))
            checked=re.sub(r'```.*?```','',body,flags=re.S)
        for match in re.finditer(r'\[[^\]\n]*\]\((<[^>]+>|[^\s)]+)(?:\s+"[^"]*")?\)',checked):
            target=match[1].strip('<>')
            if re.match(r'^(?:https?|mailto|codex|app):',target) or target.startswith('#'):continue
            target, _, fragment=target.partition('#')
            target=unquote(target)
            fragment=unquote(fragment)
            # Codex local source links support a one-based :line suffix.
            location=re.fullmatch(r'(.*):([1-9][0-9]*)',target)
            if location:target=location[1]
            dest=Path(target) if Path(target).is_absolute() else path.parent/target
            counts['links']+=1
            if not dest.exists():errors.append('Missing link: '+str(path)+' -> '+target)
            elif fragment and dest.is_file() and dest.suffix.lower() == '.md':
                linked=dest.read_text(encoding='utf-8-sig')
                anchors=set(re.findall(r'<a\s+id=["\x27]([^"\x27]+)',linked))
                for heading in re.findall(r'^#{1,6}\s+(.+)',linked,re.M):
                    anchors.add(re.sub(r'[^\w\-\s]','',heading.lower()).replace(' ','-'))
                if fragment not in anchors:errors.append('Missing anchor: '+str(path)+' -> '+target+'#'+fragment)
    inv=json.loads((root/'docs/frontend-api-inventory.json').read_text(encoding='utf-8'))
    # Only the latest source checkout is authoritative; the old root is an explicit mirror.
    if root.resolve()!=args.root[-1].resolve():continue
    actual={path.name for path in (root/'docs').glob('*.md')}
    if actual!=primary:errors.append('Primary documents differ: missing='+str(primary-actual)+' unexpected='+str(actual-primary))
    try:
        generated=subprocess.run(['node',str(root/'scripts/update-interface-documentation.mjs'),'--check'],
            cwd=root,capture_output=True,text=True,encoding='utf-8')
        if generated.returncode:
            errors.append('Generated interface contract differs: '+generated.stderr.strip())
        else:
            generated_counts=json.loads(generated.stdout)
            counts['contract_models']=generated_counts['models']
            counts['sse_events']=generated_counts['sse_events']
    except (OSError,ValueError) as error:
        errors.append('Cannot verify generated interface contract: '+str(error))
    patterns=[]
    for source in inv['routes_sources']:
        raw=(root/source).read_bytes();text=raw.decode('utf-8-sig')
        patterns += re.findall(r'HandleFunc\("([A-Z]+) ([^"]+)"',text)
        hash_input=raw.replace(b'\r\n',b'\n') if inv.get('source_hash_normalization')=='lf' else raw
        if hashlib.sha256(hash_input).hexdigest()!=inv['source_hashes'][source]:errors.append('Stale source hash: '+source)
    endpoints=inv['endpoints'];keys=[(e['method'],e['path']) for e in endpoints]
    if len(keys)!=len(set(keys)):errors.append('Duplicate HTTP inventory entries')
    expected=set(patterns)-{('POST','/api/providers/{operation}')}
    expected.update({('POST','/api/providers/models'),('POST','/api/providers/test')})
    if expected!=set(keys):errors.append('Exact route inventory differs: missing='+str(expected-set(keys))+' stale='+str(set(keys)-expected))
    def matches(pattern,entry):
        return pattern[0]==entry[0] and re.fullmatch(re.sub(r'\\\{[^}]+\\\}',r'[^/]+',re.escape(pattern[1])),entry[1])
    for pattern in patterns:
        if not any(matches(pattern,e) for e in keys):errors.append('Missing documented route: '+str(pattern))
    for endpoint in endpoints:
        key=(endpoint['method'],endpoint['path'])
        if not any(matches(p,key) for p in patterns):errors.append('Unregistered documented endpoint: '+str(key))
        ref=endpoint['backend_source'];lines=(root/ref['file']).read_text(encoding='utf-8-sig').splitlines()
        if not re.search(r'\b'+re.escape(endpoint['backend_handler'])+r'\(',lines[ref['line']-1]):errors.append('Stale handler location: '+str(key))
    if len(patterns)!=inv['registered_route_count'] or len(keys)!=inv['expanded_endpoint_count']:errors.append('Route counts differ')
    counts['routes']=len(keys)
    desktop=inv.get('desktop_commands',[])
    shell=(root/'desktop/src/shell.rs').read_text(encoding='utf-8')
    handler=re.search(r'invoke_handler\(tauri::generate_handler!\[(.*?)\]\)',shell,re.S)
    names={name.strip().split('::')[-1] for name in handler[1].split(',') if name.strip()} if handler else set()
    if names!={entry['command'] for entry in desktop}:errors.append('Desktop IPC inventory differs')
    for entry in desktop:
        ref=entry['source'];line=(root/ref['file']).read_text(encoding='utf-8').splitlines()[ref['line']-1]
        if not re.search(r'\bfn\s+'+re.escape(entry['command'])+r'\(',line):errors.append('Stale desktop command location: '+entry['command'])
    counts['desktop_commands']=len(desktop)
result={'counts':counts,'errors':errors,'scope':'Local current links and historical authority links; latest-source HTTP registration, hashes and handler locations. External URLs not fetched.'}
if os.getenv('VAC_TEST_OUTPUT'):
    output=Path(os.environ['VAC_TEST_OUTPUT'])/'documentation'
    output.mkdir(parents=True,exist_ok=True)
    (output/'documentation-check.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
print(json.dumps(result,ensure_ascii=False,indent=2))
raise SystemExit(bool(errors))
