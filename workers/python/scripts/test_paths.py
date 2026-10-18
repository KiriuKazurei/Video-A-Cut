"""Versioned generated test output; source and fixed fixtures stay outside it."""
import json
import os
import re
import stat
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
STORAGE = REPO / '.run-data' / 'tests'


def test_directory(name=''):
    version = json.loads((REPO / 'desktop' / 'tauri.conf.json').read_text(encoding='utf-8'))['version']
    base = Path(os.environ.get('VAC_TEST_ROOT', str(STORAGE / version / 'artifacts'))).absolute()
    output = (base / name).absolute()
    if not base.is_relative_to(STORAGE) or not output.is_relative_to(base) or '..' in output.parts:
        raise ValueError('Test output must remain in versioned test storage')
    for parent in (output, *output.parents):
        try:
            entry = parent.lstat()
        except FileNotFoundError:
            continue
        if stat.S_ISLNK(entry.st_mode) or getattr(entry, 'st_file_attributes', 0) & stat.FILE_ATTRIBUTE_REPARSE_POINT:
            raise ValueError('Linked test output is not allowed')
    version_root = STORAGE / base.relative_to(STORAGE).parts[0]
    if not re.fullmatch(r'\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?', version_root.name):
        raise ValueError('Invalid output version')
    marker = version_root / '.test-workspace.json'
    if marker.exists():
        owner = json.loads(marker.read_text(encoding='utf-8-sig'))
        if owner != {'schema': 1, 'version': version_root.name}:
            raise ValueError('Invalid test workspace marker')
    else:
        if version_root.exists() and any(version_root.iterdir()):
            raise ValueError('Unmarked test workspace')
        version_root.mkdir(parents=True, exist_ok=True)
        marker.write_text(json.dumps({'schema': 1, 'version': version_root.name}), encoding='utf-8')
    base.mkdir(parents=True, exist_ok=True)
    return output


def go_cache():
    version = json.loads((REPO / 'desktop' / 'tauri.conf.json').read_text(encoding='utf-8'))['version']
    return os.environ.get('GOCACHE', str(STORAGE / version / 'cache' / 'go'))
