"""Check current API inventory and workbench handoff links against repository files."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re


def main():
    args = argparse.ArgumentParser()
    args.add_argument('--local-docs', type=Path)
    options = args.parse_args()
    root = Path(__file__).resolve().parents[1]
    inventory = json.loads((root / 'docs/frontend-api-inventory.json').read_text(encoding='utf-8'))
    registered = set()
    for file in (root / 'control-plane/internal/api').glob('*.go'):
        if file.name.endswith('_test.go'):
            continue
        registered.update(re.findall(r'\.Handle(?:Func)?\("([A-Z]+) ([^"]+)"', file.read_text(encoding='utf-8')))
    expanded = registered - {('POST', '/api/providers/{operation}')}
    expanded.update({('POST', '/api/providers/models'), ('POST', '/api/providers/test')})
    documented = {(e['method'], e['path']) for e in inventory['endpoints']}
    assert expanded == documented, f'API inventory drift: missing={expanded-documented}, stale={documented-expanded}'
    assert len(registered) == inventory['registered_route_count']
    assert len(documented) == inventory['expanded_endpoint_count'] == len(inventory['endpoints'])
    assert len({e['id'] for e in inventory['endpoints']}) == len(inventory['endpoints'])
    for entry in inventory['endpoints']:
        source = entry['backend_source']
        line = (root / source['file']).read_text(encoding='utf-8').splitlines()[source['line'] - 1]
        assert re.search(r'func \(s \*Server\) ' + re.escape(entry['backend_handler']) + r'\(', line), (entry['id'], source, line)
    assert hashlib.sha256((root / inventory['routes_source']).read_bytes()).hexdigest() == inventory['routes_source_sha256']
    documents = [root / p for p in (
        'docs/flow-optimize-handoff.md', 'docs/flow-optimize-stages.md',
        'docs/flow-optimize-progress.md', 'docs/script-workbench-syntax.md',
        'docs/前端接口清单与重构交接.md', 'webui/README.md',
        'skills/vac-script-syntax/SKILL.md',
    )]
    if options.local_docs:
        documents += [options.local_docs / p for p in ('README.md', '当前项目状态.md', '当前工作与待办.md')]
    links = 0
    for document in documents:
        text = document.read_text(encoding='utf-8-sig')
        for target in re.findall(r'\[[^\]]+\]\(([^)]+)\)', text):
            target = target.strip().strip('<>')
            if re.match(r'^(https?://|app://|#)', target):
                continue
            target = target.split('#', 1)[0]
            if not target:
                continue
            file = Path(target)
            if not file.is_absolute():
                file = document.parent / file
            assert file.exists(), f'Broken link in {document}: {target}'
            links += 1
    destination = os.environ.get('VAC_TEST_OUTPUT')
    assert destination, 'Run through scripts/TestWorkspace.ps1'
    report = {'status': 'passed', 'registered_routes': len(registered), 'documented_endpoints': len(documented),
              'documents': len(documents), 'local_links': links, 'handler_locations': len(documented)}
    output = Path(destination) / 'script-workbench-20261018' / 'doc-check.json'
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(report, ensure_ascii=False))


if __name__ == '__main__':
    main()
