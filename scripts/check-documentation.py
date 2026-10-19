"""Check authored documentation links and the current HTTP inventory, without reading secrets."""
from pathlib import Path
import argparse, hashlib, json, os, re
from urllib.parse import unquote

parser=argparse.ArgumentParser()
parser.add_argument('--root',type=Path,action='append',required=True)
args=parser.parse_args()
excluded={'.git','.run-data','node_modules','target','dist','build','release','__pycache__','.agents','.codex','.venv','venv','legacy','upstream','builtin'}
errors=[]; counts={'documents':0,'historical':0,'links':0,'routes':0}
current={}
for root in args.root:
    authored=[]
    for folder,dirs,names in os.walk(root,followlinks=False):
        dirs[:]=[d for d in dirs if d not in excluded and not (Path(folder)/d).is_symlink() and not (Path(folder)/d).is_junction()]
        authored += [Path(folder)/n for n in names if Path(n).suffix.lower() in {'.md','.mdx','.txt'} and n not in {'requirements.txt','CMakeLists.txt'}]
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
            target=unquote(target.split('#',1)[0])
            # Codex local source links support a one-based :line suffix.
            location=re.fullmatch(r'(.*):([1-9][0-9]*)',target)
            if location:target=location[1]
            dest=Path(target) if Path(target).is_absolute() else path.parent/target
            counts['links']+=1
            if not dest.exists():errors.append('Missing link: '+str(path)+' -> '+target)
    inv=json.loads((root/'docs/frontend-api-inventory.json').read_text(encoding='utf-8'))
    # Only the latest source checkout is authoritative; the old root is an explicit mirror.
    if root.resolve()!=args.root[-1].resolve():continue
    patterns=[]
    for source in inv['routes_sources']:
        raw=(root/source).read_bytes();text=raw.decode('utf-8-sig')
        patterns += re.findall(r'HandleFunc\("([A-Z]+) ([^"]+)"',text)
        if hashlib.sha256(raw).hexdigest()!=inv['source_hashes'][source]:errors.append('Stale source hash: '+source)
    endpoints=inv['endpoints'];keys=[(e['method'],e['path']) for e in endpoints]
    if len(keys)!=len(set(keys)):errors.append('Duplicate HTTP inventory entries')
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
result={'counts':counts,'errors':errors,'scope':'Local current links and historical authority links; latest-source HTTP registration, hashes and handler locations. External URLs not fetched.'}
if os.getenv('VAC_TEST_OUTPUT'):
    output=Path(os.environ['VAC_TEST_OUTPUT'])/'docs-review-20261016'
    output.mkdir(parents=True,exist_ok=True)
    (output/'documentation-check.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
print(json.dumps(result,ensure_ascii=False,indent=2))
raise SystemExit(bool(errors))
