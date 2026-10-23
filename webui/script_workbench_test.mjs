import test from 'node:test';
import assert from 'node:assert/strict';
import { scriptTokens, initialScriptSegment } from './src/scriptWorkbench.ts';
import { applyAuthoringInsert, completionContext, filterFonts, filterParams, filterSnippets, invalidReplacements, paragraphTemplate } from './src/scriptAuthoring.ts';
import { handleSSEEvent, REFRESH_DOMAINS, SUPPORTED_SSE_EVENTS } from './src/queryRefresh.ts';
import { rebaseReferences, insertReference, candidateLabel, synchronizeReferenceNames } from './src/scriptReferences.ts';
import { linkedSelection, omitsLinkedPartner, snapFrame, frameLabel, frameSeconds, parseTimecode, trackGaps } from './src/timelineContracts.ts';

test('script highlighting preserves all text, escaped braces and quoted closing brace', () => {
  const source = '自然语言\\{文本\\} {@片段.mp4}\n\n{"括号 } 与 \\"引号\\"", 字号:32px,}{白屏}';
  const tokens = scriptTokens(source);
  assert.equal(tokens.map(t => t.text).join(''), source);
  assert.equal(tokens.filter(t => t.kind === 'directive').length, 3);
  for (const invalid of ['{未知}', '{黑屏', '{{白屏}}']) assert.equal(scriptTokens(invalid)[0].kind, 'invalid');
  assert.equal(scriptTokens('{"字幕", opi:1}')[0].kind, 'invalid');
  assert.equal(scriptTokens('{"重点说明", 字号:32px}')[0].kind, 'directive');
  assert.equal(scriptTokens('{"重点说明", 字体:"YaHei", 字号:24px}')[0].kind, 'directive');
});
test('completion filters known effects and subtitle parameters without new directives', () => {
  assert.deepEqual(filterSnippets('黑').map(item => item.text), ['{黑屏}']);
  assert.deepEqual(filterSnippets('交').map(item => item.text), ['{交叉溶解}']);
  assert.equal(filterSnippets('').length, 5);
  assert.equal(filterSnippets('闪白').length, 0);
  assert.equal(filterSnippets('字').some(item => item.text.includes('字幕')), true);
  const typed = completionContext('{黑', 2);
  assert.equal(typed?.kind, 'snippet');
  assert.equal(typed?.query, '黑');
  assert.equal(typed?.start, 0);
  const open = '{"字幕", ';
  const params = completionContext(open, open.length);
  assert.equal(params?.kind, 'param');
  assert.deepEqual(filterParams(params?.query ?? '').map(item => item.text), ['字号:', '字体:"']);
  assert.deepEqual(filterParams('字体').map(item => item.key), ['font']);
  const sized = '{"字幕", 字号:32px';
  const nextParam = completionContext(sized, sized.length);
  assert.equal(nextParam?.kind, 'param');
  assert.deepEqual(nextParam?.omit, ['size']);
  const font = '{"字幕", 字体:"Ya';
  const fonts = completionContext(font, font.length);
  assert.equal(fonts?.kind, 'font');
  assert.equal(fonts?.query, 'Ya');
  assert.deepEqual(filterFonts(['YaHei', 'SimSun', 'Microsoft YaHei'], 'Ya'), ['YaHei']);
  assert.equal(completionContext('纯文字', 1, true), null);
  const forced = completionContext('介绍{黑屏}', 1, true);
  assert.equal(forced?.kind, 'snippet');
  assert.equal(forced?.start, 1);
});
test('invalid spans can be replaced and paragraph templates rebase references', () => {
  assert.deepEqual(invalidReplacements('{未知效果}').map(item => item.text), ['{黑屏}', '{白屏}', '{交叉溶解}']);
  assert.equal(invalidReplacements('{黑')[0].text, '{黑屏}');
  assert.equal(invalidReplacements('{"坏字幕", opi:1}')[0].label, '换成字幕模板');
  const reference = invalidReplacements('{@');
  assert.equal(reference[0].openReference, true);
  assert.equal(reference[0].text, '{@}');
  const before = '{未知效果}尾{@片段.mp4}';
  const bad = scriptTokens(before).find(token => token.kind === 'invalid');
  assert.ok(bad);
  const fix = invalidReplacements(bad.text)[0];
  const refs = [{ id: '1', name: '片段.mp4', kind: 'video', target_id: 't', start: before.indexOf('{@'), end: before.length, unresolved: false }];
  const replaced = applyAuthoringInsert(before, refs, bad.start, bad.end, fix);
  assert.equal(replaced.body, '{黑屏}尾{@片段.mp4}');
  assert.equal(replaced.references[0].start, replaced.body.indexOf('{@'));
  const opening = paragraphTemplate('open');
  assert.match(opening.text, /^\{黑屏\}\{@\}在此写开场要求\n\n$/);
  assert.equal(opening.text[opening.selectStart], '}');
  const result = paragraphTemplate('result');
  assert.equal(result.text.slice(result.selectStart, result.selectEnd), '在此写结果要求');
  assert.match(result.text, /\{"重点说明", 字号:32px\}\{白屏\}/);
  assert.match(paragraphTemplate('transition').text, /^\{交叉溶解\}\{@\}/);
  const filled = applyAuthoringInsert('前文', [], 2, 2, opening, { id: '', name: '演示.mp4', kind: 'video', target_id: 't', resource_id: 'r', owner: '库', unresolved: false, start: 0, end: 0, fps: { num: 30, den: 1 } });
  assert.equal(filled.body, `前文${opening.text.replace('{@}', '{@演示.mp4}')}`);
  assert.equal(filled.references[0]?.name, '演示.mp4');
  assert.equal(filled.references[0]?.start, filled.body.indexOf('{@'));
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
