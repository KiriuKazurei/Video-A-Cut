import assert from 'node:assert/strict';
import { mkdir, rm } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { describe, test } from 'node:test';
import { fileURLToPath, pathToFileURL } from 'node:url';
import * as esbuild from 'esbuild';
import { Window } from 'happy-dom';
import { createElement, useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { renderToString } from 'react-dom/server';
import { parseWorkspaceHash } from './src/navigation.ts';

const webuiRoot = dirname(fileURLToPath(import.meta.url));
const bundlePath = join(webuiRoot, 'node_modules/.cache/editor-right-dock-under-test.mjs');
const entry = join(webuiRoot, 'src/components/EditorRightDock.tsx');

const stubSource = new Map([
  ['antd', `
    import React from 'react';
    export function Button(props) {
      return React.createElement('button', {
        type: 'button',
        className: props.className,
        title: props.title,
        'aria-expanded': props['aria-expanded'],
        'aria-label': props['aria-label'],
        'aria-pressed': props['aria-pressed'],
        onClick: props.onClick,
      }, props.children);
    }
    export function Badge(props) {
      return React.createElement('span', { 'aria-label': props['aria-label'] }, props.count);
    }
    export function Table() { return null; }
    export function Typography() { return null; }
    Typography.Text = function Text(props) { return React.createElement('span', null, props.children); };
  `],
  ['@tanstack/react-query', 'export function useQuery() { return { data: undefined }; }'],
  ['./TimelineInspector', componentStub('TimelineInspector')],
  ['./SkillRunModule', componentStub('SkillRunModule')],
  ['./CodeClipWorkbench', componentStub('CodeClipWorkbench')],
  ['../workspaces/ScriptWorkspace', `
    import React from 'react';
    export function ScriptWorkspace(props) {
      return React.createElement('div', { 'data-script-workspace': 'true', 'data-active': String(Boolean(props.active)) });
    }
  `],
  ['../sequenceEditor', 'export function useSequenceEditor() { return { projectId: "", view: undefined, selected: [] }; }'],
  ['../skillWorkspace', 'export function useSkillWorkspace() { return { runId: "" }; }'],
  ['../skillApi', 'export const skillApi = { runs: async () => [] };'],
  ['../skillContracts', 'export const skillQueryKeys = { runs: (id) => ["vu-runs", id] };'],
  ['../skillView', 'export function protectedQueryRetry() { return false; }'],
]);

function componentStub(name) {
  return `import React from 'react'; export function ${name}() { return null; }`;
}

let EditorRightDock;
let happy;
const priorGlobals = new Map();

function assignGlobal(key, value) {
  if (!priorGlobals.has(key)) priorGlobals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
  Object.defineProperty(globalThis, key, { configurable: true, enumerable: true, writable: true, value });
}

describe('EditorRightDock script route', { concurrency: false }, () => {
test.before(async () => {
  await mkdir(dirname(bundlePath), { recursive: true });
  await esbuild.build({
    entryPoints: [entry],
    bundle: true,
    format: 'esm',
    target: 'es2022',
    jsx: 'automatic',
    outfile: bundlePath,
    plugins: [{
      name: 'dock-unit-stubs',
      setup(build) {
        build.onResolve({ filter: /.*/ }, (args) => {
          if (args.path === 'react' || args.path === 'react/jsx-runtime' || args.path === 'react/jsx-dev-runtime') {
            return { path: args.path, external: true };
          }
          if (stubSource.has(args.path)) return { path: args.path, namespace: 'dock-stub' };
          return null;
        });
        build.onLoad({ filter: /.*/, namespace: 'dock-stub' }, (args) => ({
          contents: stubSource.get(args.path),
          loader: 'js',
        }));
      },
    }],
  });
  ({ EditorRightDock } = await import(pathToFileURL(bundlePath).href));

  happy = new Window({ url: 'http://127.0.0.1/app#/script' });
  for (const key of ['window', 'document', 'HTMLElement', 'Element', 'Node', 'DocumentFragment', 'navigator', 'MutationObserver', 'Event', 'CustomEvent']) {
    assignGlobal(key, key === 'window' ? happy : happy[key]);
  }
  assignGlobal('getComputedStyle', happy.getComputedStyle.bind(happy));
  assignGlobal('requestAnimationFrame', (callback) => happy.requestAnimationFrame(callback));
  assignGlobal('cancelAnimationFrame', (handle) => happy.cancelAnimationFrame(handle));
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

test.after(async () => {
  for (const [key, descriptor] of priorGlobals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else delete globalThis[key];
  }
  delete globalThis.IS_REACT_ACT_ENVIRONMENT;
  happy?.close();
  await rm(bundlePath, { force: true });
});

function dockProps(route, extra = {}) {
  return {
    route,
    navigate: () => {},
    active: true,
    assetId: 'asset-1',
    onExpand: () => {},
    onCollapse: () => {},
    ...extra,
  };
}

function scriptSection(html) {
  const section = html.split('<section ').find((part) => part.includes('is-script'));
  assert.ok(section, html);
  return section.split('</section>')[0];
}

function assertScriptModule(html, open) {
  const section = scriptSection(html);
  assert.equal(/class="[^"]*\bis-open\b/.test(section), open, section);
  assert.match(section, /aria-expanded="(true|false)"/);
  assert.equal(/aria-expanded="true"/.test(section), open, section);
  const body = section.match(/<div class="editor-dock-body"([^>]*)>/);
  assert.ok(body, section);
  assert.equal(/\bhidden\b/.test(body[1]), !open, section);
  if (open) assert.match(section, /data-script-workspace="true"/);
}

test('fresh #/script and #/editing/content mounts open the script module before any dock event', () => {
  const hashes = ['#/script', '#/script?script=s1&version=3&segment=0', '#/editing/content', '#/editing/content?script=s1&version=3'];
  for (const hash of hashes) {
    window.dispatchEvent(new window.Event('vac:open-script-dock'));
    const html = renderToString(createElement(EditorRightDock, dockProps(parseWorkspaceHash(hash))));
    assertScriptModule(html, true);
  }
  const html = renderToString(createElement(EditorRightDock, dockProps(parseWorkspaceHash('#/editing/import'))));
  assertScriptModule(html, false);
  const byScriptId = renderToString(createElement(EditorRightDock, dockProps({ workspace: 'editing', editingSection: 'import', scriptId: 's1' })));
  assertScriptModule(byScriptId, true);
  const blankScript = renderToString(createElement(EditorRightDock, dockProps({ workspace: 'editing', editingSection: 'import', scriptId: '' })));
  assertScriptModule(blankScript, false);
});

test('a same-commit open event is dropped, and the script route still mounts open', async () => {
  const { act } = await import('react');
  const calls = [];
  const route = parseWorkspaceHash('#/editing/content');
  function EarlyDispatch() {
    useEffect(() => { window.dispatchEvent(new window.Event('vac:open-script-dock')); }, []);
    return null;
  }
  function Tree() {
    return createElement('div', null,
      createElement(EarlyDispatch),
      createElement(EditorRightDock, dockProps(route, { onExpand: () => calls.push('expand') })));
  }
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  try {
    await act(async () => { root.render(createElement(Tree)); });
    assertScriptModule(container.innerHTML, true);
    assert.ok(calls.length >= 1);
  } finally {
    await act(async () => { root.unmount(); });
    container.remove();
  }
});

test('route updates keep the script module in sync and still honor the toolbar event', async () => {
  const { act } = await import('react');
  const calls = [];
  const timeline = parseWorkspaceHash('#/editing/import');
  const content = parseWorkspaceHash('#/script');
  let onExpand = () => calls.push('expand');
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  const render = (route) => root.render(createElement(EditorRightDock, dockProps(route, { onExpand })));
  try {
    await act(async () => { render(timeline); });
    assertScriptModule(container.innerHTML, false);
    assert.equal(calls.length, 0);

    await act(async () => { window.dispatchEvent(new window.Event('vac:open-script-dock')); });
    assertScriptModule(container.innerHTML, true);
    assert.equal(calls.length, 1);

    onExpand = () => calls.push('expand-again');
    await act(async () => { render(timeline); });
    assertScriptModule(container.innerHTML, true);

    await act(async () => { render(content); });
    assertScriptModule(container.innerHTML, true);
    assert.ok(calls.includes('expand-again'));

    await act(async () => { render(timeline); });
    assertScriptModule(container.innerHTML, false);
  } finally {
    await act(async () => { root.unmount(); });
    container.remove();
  }
});

test('a collapsed script dock expands when a fresh content route needs it', async () => {
  const { act } = await import('react');
  const calls = [];
  function Host() {
    const [stripOnly, setStripOnly] = useState(true);
    return createElement(EditorRightDock, dockProps(parseWorkspaceHash('#/script'), {
      stripOnly,
      onExpand: () => { calls.push('expand'); setStripOnly(false); },
    }));
  }
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  try {
    await act(async () => { root.render(createElement(Host)); });
    assert.ok(calls.length >= 1);
    assertScriptModule(container.innerHTML, true);
  } finally {
    await act(async () => { root.unmount(); });
    container.remove();
  }
});

test('an inactive dock does not expand the shell for a script route', async () => {
  const { act } = await import('react');
  const calls = [];
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  try {
    await act(async () => {
      root.render(createElement(EditorRightDock, dockProps(parseWorkspaceHash('#/editing/content'), {
        active: false,
        onExpand: () => calls.push('expand'),
      })));
    });
    assertScriptModule(container.innerHTML, true);
    assert.equal(calls.length, 0);
  } finally {
    await act(async () => { root.unmount(); });
    container.remove();
  }
});
});
