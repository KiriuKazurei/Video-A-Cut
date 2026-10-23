import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { DRAW_HANDOFF_ACTIONS, DRAW_HANDOFF_STUB, DRAW_ZONES, PHASE1_COMPOSITIONS, drawAgentContext, drawRoute, requestDrawHandoff, zoneForFocus } from './src/drawContracts.ts';
import { WORKSPACE_KEYS, WORKSPACES, buildWorkspaceHash, parseWorkspaceHash } from './src/navigation.ts';
import { locationCrumbs, stageOfRoute } from './src/pipeline.ts';

const page = readFileSync(new URL('./src/workspaces/DrawWorkspace.tsx', import.meta.url), 'utf8');
const app = readFileSync(new URL('./src/App.tsx', import.meta.url), 'utf8');
const shortcuts = readFileSync(new URL('./src/hooks/useKeyboardShortcuts.ts', import.meta.url), 'utf8');

test('draw is one top-level workspace and digit 6 opens it', () => {
  const draw = WORKSPACES.filter((item) => item.id === 'draw');
  assert.equal(draw.length, 1);
  assert.equal(draw[0].label, '绘制');
  assert.equal(WORKSPACE_KEYS['6'], 'draw');
  assert.deepEqual(Object.values(WORKSPACE_KEYS), ['resources', 'editing', 'export', 'settings', 'tts', 'draw']);
  assert.match(shortcuts, /WORKSPACE_KEYS\[e\.key\]/);
  assert.match(app, /workspace !== 'draw'/);
  assert.doesNotMatch(app, /draw\/lint|workspace: 'draw-lint'/);
});

test('draw deep links use query fields and ignore a fake sub-tab path', () => {
  const state = parseWorkspaceHash('#/draw/lint?project=p1&run=run-9&slot=slot-title&engine=remotion&focus=render-queue');
  assert.equal(state.workspace, 'draw');
  assert.equal(state.editingSection, undefined);
  assert.equal(state.drawProject, 'p1');
  assert.equal(state.drawRun, 'run-9');
  assert.equal(state.drawSlot, 'slot-title');
  assert.equal(state.drawEngine, 'remotion');
  assert.equal(state.drawFocus, 'render-queue');
  assert.equal(zoneForFocus(state.drawFocus), 'inspector');
  const hash = buildWorkspaceHash(state);
  assert.match(hash, /^\/draw\?/);
  assert.doesNotMatch(hash, /\/draw\/lint/);
  assert.equal(parseWorkspaceHash('#' + hash).drawFocus, 'render-queue');
  assert.equal(parseWorkspaceHash('#/draw?engine=shell&focus=nope').drawEngine, undefined);
  assert.equal(parseWorkspaceHash('#/draw?engine=shell&focus=nope').drawFocus, undefined);
  assert.equal(stageOfRoute({ workspace: 'draw' }), undefined);
  assert.deepEqual(locationCrumbs({ workspace: 'draw' }, 'a1').map((item) => item.label), ['绘制']);
});

test('entry points build a draw route with project, run, slot, engine, and focus', () => {
  assert.deepEqual(drawRoute({ project: 'p', run: 'r', slot: 'slot-title', engine: 'hyperframes', focus: 'source' }), {
    workspace: 'draw', drawProject: 'p', drawRun: 'r', drawSlot: 'slot-title', drawEngine: 'hyperframes', drawFocus: 'source'
  });
  const script = readFileSync(new URL('./src/components/ScriptWorkbench.tsx', import.meta.url), 'utf8');
  const skill = readFileSync(new URL('./src/components/SkillRunModule.tsx', import.meta.url), 'utf8');
  const timeline = readFileSync(new URL('./src/components/SequenceTimeline.tsx', import.meta.url), 'utf8');
  assert.match(script, /drawRoute\(\{ project: projectId, slot: scriptId/);
  assert.match(skill, /drawRoute\(\{ project: projectId, run: runId/);
  assert.match(timeline, /打开绘制/);
});

test('three handoffs are typed, stubbed, and free of a shell', () => {
  assert.deepEqual([...DRAW_HANDOFF_ACTIONS], ['import_into_editing', 'composite_preview', 'merge_render']);
  for (const action of DRAW_HANDOFF_ACTIONS) {
    const result = requestDrawHandoff({ action, project_id: 'p', slot_ids: ['slot-title'], engine: 'hyperframes', sequence_id: 'seq' });
    assert.equal(result.status, 'stub');
    assert.equal(result.message, '方案接线中');
    assert.equal(result.message, DRAW_HANDOFF_STUB);
    assert.equal(result.transport, 'unwired');
    assert.equal(result.action, action);
  }
  const context = drawAgentContext({ project_id: 'p', engine: 'hyperframes', slot_id: 'slot-title' });
  assert.match(context, /shell=unavailable/);
  assert.match(context, /import_into_editing/);
  for (const marker of ['npx ', 'exec.Command', 'child_process', 'os/exec', 'bash -c']) {
    assert.equal(context.includes(marker), false, marker);
    assert.equal(page.includes(marker), false, marker);
  }
});

test('draw page landmarks are the four studio zones, not a second timeline product', () => {
  DRAW_ZONES.forEach((zone, index) => {
    assert.match(page, new RegExp(`data-draw-zone="${zone.id}"`));
    assert.match(page, new RegExp(`aria-label=\\{DRAW_ZONES\\[${index}\\]\\.label\\}`));
  });
  assert.match(page, /构图时间轴/);
  assert.match(page, /不是 Premiere 式多轨剪辑时间线/);
  assert.match(page, /方案接线中/);
  assert.equal(PHASE1_COMPOSITIONS.every((item) => item.rendered === false), true);
  assert.match(readFileSync(new URL('./src/styles.css', import.meta.url), 'utf8'), /\.draw-studio \{/);
  assert.doesNotMatch(page, /linear-gradient|backdrop-filter|box-shadow:\s*(?!none)/);
});
