import test from 'node:test';
import assert from 'node:assert/strict';
import { scriptTokens, initialScriptSegment } from './src/scriptWorkbench.ts';
import { handleSSEEvent, REFRESH_DOMAINS, SUPPORTED_SSE_EVENTS } from './src/queryRefresh.ts';
import { rebaseReferences, insertReference, candidateLabel, synchronizeReferenceNames } from './src/scriptReferences.ts';
import { linkedSelection, omitsLinkedPartner, snapFrame, frameLabel, frameSeconds, parseTimecode, trackGaps } from './src/timelineContracts.ts';

test('script highlighting preserves all text, escaped braces and quoted closing brace', () => {
  const source = '自然语言\\{文本\\} {@片段.mp4}\n\n{"括号 } 与 \\"引号\\"", 字号:32px,}{白屏}';
  const tokens = scriptTokens(source);
  assert.equal(tokens.map(t => t.text).join(''), source);
  assert.equal(tokens.filter(t => t.kind === 'directive').length, 3);
  for (const invalid of ['{未知}', '{黑屏', '{{白屏}}']) assert.equal(scriptTokens(invalid)[0].kind, 'invalid');
});
test('forced media has an empty narration until a person or Agent actually writes it', () => {
  const media = [{ id: 'm', name: '片段.mp4', asset_id: 'a', in: 1, out: 3 }];
  const paragraph = { index: 0, instruction: '介绍抽卡', video: '片段.mp4' };
  assert.deepEqual(initialScriptSegment(paragraph, media), { index: 0, narration: '', media_id: 'm', start: 1, end: 3 });
  assert.equal(initialScriptSegment({ ...paragraph, video: '隐藏素材.mp4' }, media).media_id, '');
});
test('script events and reconnect refresh authoritative script data', async () => {
  const keys = [];
  await handleSSEEvent({ invalidateQueries: async ({queryKey}) => { keys.push(queryKey[0]); } }, 'script.changed', { script_id: 's' });
  for (const key of ['scripts', 'script-workbench', 'script-revisions', 'script-media', 'script-delivery', 'visible-set']) {
    assert.ok(keys.includes(key)); assert.ok(REFRESH_DOMAINS.includes(key));
  }
  assert.ok(SUPPORTED_SSE_EVENTS.includes('script.changed'));
  assert.ok(SUPPORTED_SSE_EVENTS.includes('script.rendered'));
});

test('typed duplicate references preserve UTF16 positions and replace only the chosen binding',()=>{
 const candidate={name:'重名',kind:'sequence_marker',target_id:'one',sequence_id:'seq',owner:'序列',frame:7,fps:{num:30,den:1}};
 const first=insertReference('😀 ',[],3,3,candidate);assert.equal(first.references[0].start,3);assert.equal(first.body,'😀 {@重名}');
 const replacement=insertReference(first.body,first.references,3,8,{...candidate,target_id:'two'});assert.equal(replacement.references.length,1);assert.equal(replacement.references[0].target_id,'two');
 assert.equal(rebaseReferences(first.body,'前言 '+first.body,first.references)[0].start,6);
 assert.equal(rebaseReferences(first.body,'😀 {@其他}',first.references).length,0);
 assert.match(candidateLabel(candidate),/时间线标记点.*第 7 帧/);
});
test('timeline snapping and linked selection retain independent clip identities',()=>{
 const sequence={tracks:[{clips:[{id:'v',link_id:'link'},{id:'a',link_id:'link'},{id:'another',link_id:'other'}]}]};
 assert.deepEqual(linkedSelection(sequence,['v']),['v','a']);assert.deepEqual(linkedSelection(sequence,['v'],true),['v']);
 assert.equal(omitsLinkedPartner(sequence,['v']),true);assert.equal(omitsLinkedPartner(sequence,['v','a']),false);assert.equal(omitsLinkedPartner(sequence,['another']),false);
 assert.equal(snapFrame(101,[100],1,true),100);assert.equal(snapFrame(101,[100],1,false),101);assert.equal(snapFrame(-1,[],1,true),0);
 assert.equal(frameLabel(1799,{num:30000,den:1001}),'00:00:59:29');assert.equal(frameLabel(1800,{num:30000,den:1001}),'00:01:00:00');assert.equal(frameSeconds(1800,{num:30000,den:1001}),60.06);
});

test('timecode entry accepts frames or seconds and gaps are the holes between clips', () => {
  const rate = { num: 30, den: 1 };
  const ntsc = { num: 30000, den: 1001 };
  assert.equal(parseTimecode('00:00:01:15', rate), 45);
  assert.equal(parseTimecode('00:01:00:00', rate), 1800);
  assert.equal(parseTimecode('00:00:01;15', rate), 45);
  assert.equal(parseTimecode('1.5', rate), 45);
  assert.equal(parseTimecode('1.5s', rate), 45);
  assert.equal(parseTimecode('00:00:02', rate), 60);
  assert.equal(parseTimecode('01:02.5', rate), Math.round(62.5 * 30));
  assert.equal(parseTimecode('00:00:59:29', ntsc), 1799);
  assert.equal(parseTimecode(frameLabel(1800, ntsc), ntsc), 1800);
  for (const bad of ['', 'nope', '00:00:00:30', '00:00:60:00', '-1', '01:60']) assert.equal(parseTimecode(bad, rate), null);
  assert.deepEqual(trackGaps([{ at: 30, duration: 10 }, { at: 50, duration: 5 }, { at: 10, duration: 10 }]), [{ start: 0, end: 10 }, { start: 20, end: 30 }, { start: 40, end: 50 }]);
  assert.deepEqual(trackGaps([]), []);
});

test('remote marker rename preserves an unsaved draft and distinct namesake bindings',()=>{
 const one={name:'重名',kind:'source_marker',target_id:'one',resource_id:'r',owner:'源素材',fps:{num:30,den:1}};
 let draft=insertReference('😀 未保存正文 ',[],9,9,one);
 draft=insertReference(draft.body,draft.references,draft.body.length,draft.body.length,{...one,target_id:'two'});
 const next=synchronizeReferenceNames(draft.body,draft.references,[{...one,name:'新的识别名'},{...one,target_id:'two'}]);
 assert.equal(next.body,'😀 未保存正文 {@新的识别名}{@重名}');
 assert.equal(next.references[0].id,draft.references[0].id);assert.equal(next.references[1].id,draft.references[1].id);
 for(const ref of next.references)assert.equal(next.body.slice(ref.start,ref.end),`{@${ref.name}}`);
 assert.equal(synchronizeReferenceNames(next.body,next.references,[]).body,next.body,'missing anchors never bind a namesake');
});
