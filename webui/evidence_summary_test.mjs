import test from 'node:test';
import assert from 'node:assert/strict';
import { buildEvidenceDigest, digestPriority } from './src/evidenceSummary.ts';

const checks = [
  { item: 'content_fidelity', label: '内容是否正确' },
  { item: 'browser', label: '浏览器里能否正常操作' }
];

test('empty run: plain empty headline', () => {
  const d = buildEvidenceDigest({ checkResults: checks });
  assert.equal(d.headline, '还没有可检查的成片');
  assert.match(d.nextAction, /选择素材/);
  assert.equal(d.items.find(i => i.id === 'preview')?.status, 'na');
});

test('ready_for_acceptance without preview: next action steers to 交付', () => {
  const d = buildEvidenceDigest({
    workflowStatus: 'ready_for_acceptance',
    runId: 'r1',
    revisionId: 'rev_abc',
    checkResults: checks.map(c => ({ ...c })),
    preview: { hasPlayable: false, inProgress: false, canStart: true },
    delivery: { fileCount: 0, hasPlayable: false },
    packageDownloadReady: true
  });
  assert.equal(d.headline, '可以开始验收');
  assert.match(d.nextAction, /交付/);
  assert.equal(d.items.find(i => i.id === 'preview')?.status, 'missing');
  assert.equal(d.items.find(i => i.id === 'package')?.status, 'ready');
  assert.ok(d.techDetails.some(t => t.label === '导出版本' && t.value === 'rev_abc'));
  // default view fields must not put raw sha in headline/nextAction
  assert.doesNotMatch(d.headline, /sha|指纹|rev_/i);
  assert.doesNotMatch(d.nextAction, /sha256|fingerprint/i);
});

test('all passed + playable preview: celebrate without jargon', () => {
  const d = buildEvidenceDigest({
    workflowStatus: 'ready_for_acceptance',
    runId: 'r1',
    revisionId: 'rev',
    checkResults: checks.map(c => ({ ...c, result: 'passed' })),
    preview: { hasPlayable: true, inProgress: false, previewId: 'p1', mp4Sha256: 'deadbeef'.repeat(8) },
    delivery: { fileCount: 3, hasPlayable: true },
    packageDownloadReady: true
  });
  assert.equal(d.headline, '检查已完成，可以收工');
  assert.equal(d.allPassed, true);
  assert.equal(d.items.every(i => i.status === 'ready'), true);
  assert.ok(d.techDetails.some(t => t.label === '预览成片指纹'));
  assert.ok(digestPriority('blocked') < digestPriority('ready'));
});

test('failed items elevate next action', () => {
  const d = buildEvidenceDigest({
    workflowStatus: 'ready_for_acceptance',
    runId: 'r1',
    checkResults: [
      { ...checks[0], result: 'failed' },
      { ...checks[1], result: 'passed' }
    ],
    preview: { hasPlayable: true, inProgress: false },
    packageDownloadReady: true,
    delivery: { fileCount: 1, hasPlayable: true }
  });
  assert.equal(d.headline, '有未通过的检查项');
  assert.match(d.nextAction, /未通过/);
  assert.equal(d.items.find(i => i.id === 'acceptance')?.status, 'blocked');
  assert.equal(d.counts.failed, 1);
});

import { plainPreviewBlocker } from './src/evidenceSummary.ts';

test('English worker blocker becomes plain Chinese', () => {
  const d = buildEvidenceDigest({
    workflowStatus: 'ready_for_acceptance',
    runId: 'r1',
    checkResults: checks.map(c => ({ ...c })),
    preview: { hasPlayable: false, inProgress: false, blockedHint: 'no active worker capable of preset mp4-720p-v1' },
    packageDownloadReady: true,
    delivery: { fileCount: 1, hasPlayable: true }
  });
  const preview = d.items.find(i => i.id === 'preview');
  assert.equal(preview?.status, 'blocked');
  assert.match(preview?.detail ?? '', /渲染服务/);
  assert.doesNotMatch(preview?.detail ?? '', /preset|worker capable/i);
  assert.match(plainPreviewBlocker('no active worker capable of preset mp4-720p-v1') ?? '', /渲染服务/);
});
