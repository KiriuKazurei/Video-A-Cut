import test from 'node:test';
import assert from 'node:assert/strict';

test('code selection follows sequence time and ignores selected ordinary footage',async()=>{
 const {selectedCodeClips,codeDescription}=await import('./src/codeClips.ts');
 const clip=(id,at)=>({id,at,duration:300,code:{description:'画面',revision:1,generated_description:'画面'},effects:{gain_db:0,x:0,y:0,parameter_keyframes:[{kind:'旋转',frame:30,duration:30,value:90}]}});
 const sequence={fps:{num:30,den:1},tracks:[{id:'a',kind:'code',clips:[clip('later',300)]},{id:'v',kind:'video',clips:[clip('video',0)]},{id:'b',kind:'code',clips:[clip('first',0)]}]};
 assert.deepEqual(selectedCodeClips(sequence,['later','video','first']).map(x=>x.clip.id),['first','later']);
 assert.match(codeDescription(sequence.tracks[0].clips[0],sequence.fps),/时间:00:00:11:00/);
 sequence.tracks[0].clips[0].at=600;
 assert.match(codeDescription(sequence.tracks[0].clips[0],sequence.fps),/时间:00:00:21:00/);
});

test('parameter directives highlight only inside the code workbench',()=>{
 const source='{移动, 时间:00:00:01:00, x:100px, y:0px}';
 assert.equal(scriptTokens(source,true)[0].kind,'directive');
 assert.equal(scriptTokens(source)[0].kind,'invalid');
 assert.equal(scriptTokens('{旋转, 时间:00:00:01',true)[0].kind,'invalid');
});

test('shared directive definitions drive code completion and caption highlighting',()=>{
 assert.deepEqual(filterSnippets('缩','code').map(x=>x.key),['缩放']);
 assert.equal(scriptTokens('{缩放, 比例:120%}',true)[0].kind,'directive');
 assert.equal(scriptTokens('{透明度, 从:0%, 到:100%}',true)[0].kind,'directive');
 assert.equal(scriptTokens('{字幕, 文本:"a,{b}", 时间:条内1s, 持续:1s}')[0].kind,'directive');
 assert.equal(scriptTokens('{透明度, 到:101%}',true)[0].kind,'invalid');
});

test('shot card completions insert a stable ID or explicit variant without changing media references',async()=>{
 const {inspectCodeScript}=await import('./src/scriptGrammar.ts');
 const source='{#chart-live-moves/axis-rescale-shock}{@截图}显示坐标缩放';
 assert.equal(filterSnippets('#chart-live-moves/axis')[0].text,'{#chart-live-moves/axis-rescale-shock}');
 assert.equal(completionContext(source,12).kind,'snippet');
 assert.equal(scriptTokens(source).filter(token=>token.kind==='directive').length,2);
 assert.equal(scriptTokens('{#chart-live-moves/unknown}')[0].kind,'invalid');
 const parsed=inspectCodeScript(source,{at:0,duration:120},{num:30,den:1});
 assert.equal(parsed.issues.length,0);
 assert.equal(parsed.instructions[0].mode,'generate');
});

test('code description retains authored layout and rebases only sequence time arguments',async()=>{
 const {codeDescription,rebaseCodeDescription}=await import('./src/codeClips.ts');
 const text='保留两行\n\n{移动, 时间:条内1s, x:20px, y:0px}\n{字幕, 文本:"时间:00:00:01", 时间:序列00:00:11:00, 持续:1s}';
 const c={at:330,duration:90,effects:{gain_db:0},code:{description:'保留两行',script:text,script_at:300}};
 const expected=text.replace('时间:序列00:00:11:00','时间:序列00:00:12:00');
 assert.equal(codeDescription(c,{num:30,den:1}),expected);
 assert.equal(rebaseCodeDescription(text,30,{num:30,den:1}),expected);
});

test('video clip colors use presets or any hex, and other tracks keep their defaults', () => {
  assert.equal(normalizeClipHex('#7a5362'), CLIP_ROSE);
  assert.equal(normalizeClipHex('#7A5362FF'), CLIP_ROSE);
  assert.equal(normalizeClipHex('red'), null);
  assert.equal(presetSelected(undefined, CLIP_VIDEO), true);
  assert.equal(presetSelected(CLIP_ROSE, CLIP_ROSE), true);
  assert.equal(clipBarFill('video', 'human'), 'var(--vac-clip-video)');
  assert.equal(clipBarFill('video', 'human', CLIP_ROSE), CLIP_ROSE);
  assert.equal(clipBarFill('video', 'agent:1', '#C0FFEE'), '#C0FFEE');
  assert.equal(clipBarFill('video', 'agent:1'), 'var(--vac-clip-effect)');
  assert.equal(clipBarFill('code', 'agent:1', CLIP_ROSE), 'var(--vac-clip-code)');
  assert.equal(clipBarFill('audio', 'human', CLIP_ROSE), 'var(--vac-clip-audio)');
  assert.equal(clipBarFill('subtitle', 'human'), 'var(--vac-clip-subtitle)');
});

test('code colors survive inserting and removing other occurrences',async()=>{
 const {codeClipColors}=await import('./src/codeClips.ts');
 const sequence={tracks:[{kind:'code',clips:Array.from({length:30},(_,i)=>({id:`clip-${i}`}))}]};
 const before=codeClipColors(sequence);
 for (const color of Object.values(before)) assert.equal(color, 'var(--vac-clip-code)');
 sequence.tracks[0].clips.unshift({id:'clip-000'});
 const inserted=codeClipColors(sequence);
 for(const [id,color] of Object.entries(before))assert.equal(inserted[id],color,id);
 sequence.tracks[0].clips=sequence.tracks[0].clips.filter(c=>c.id!=='clip-1');
 const removed=codeClipColors(sequence);
 for(const [id,color] of Object.entries(before))if(id!=='clip-1')assert.equal(removed[id],color,id);
 assert.equal(new Set(Object.values(before)).size,1);
 assert.equal(inserted['clip-000'], 'var(--vac-clip-code)');
});

test('moving a dirty code draft rebases parameter times while preserving quoted and escaped text',async()=>{
 const {rebaseCodeDescription}=await import('./src/codeClips.ts');
 const literal='显示\\{移动, 时间:00:00:01:00, x:1px, y:0px\\}\n{"字幕{旋转, 时间:00:00:01:00, 角度:30}"}';
 const directive='{移动, 时间:00:00:02:00, x:20px, y:0px}';
 assert.equal(rebaseCodeDescription(literal+'\n'+directive,30,{num:30,den:1}),literal+'\n'+directive.replace('00:00:02:00','00:00:03:00'));
});
import { scriptTokens, initialScriptSegment } from './src/scriptWorkbench.ts';
import { applyAuthoringInsert, completionContext, filterFonts, filterParams, filterSnippets, invalidReplacements, paragraphTemplate } from './src/scriptAuthoring.ts';
import { handleSSEEvent, REFRESH_DOMAINS, SUPPORTED_SSE_EVENTS } from './src/queryRefresh.ts';
import { rebaseReferences, insertReference, candidateLabel, synchronizeReferenceNames } from './src/scriptReferences.ts';
import { linkedSelection, omitsLinkedPartner, snapFrame, frameLabel, frameSeconds, parseTimecode, trackGaps } from './src/timelineContracts.ts';
import { CLIP_ROSE, CLIP_VIDEO } from './src/theme.ts';
import { clipBarFill, normalizeClipHex, presetSelected } from './src/clipColor.ts';

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
  assert.equal(filterSnippets('定时字幕')[0].text.startsWith('{字幕,'),true);
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
import { buildWorkspaceHash, parseWorkspaceHash } from './src/navigation.ts';
import * as workbench from './src/scriptWorkbench.ts';

test('Agent handoff preserves the current draft and identifies its project and base revision', () => {
  assert.equal(typeof workbench.scriptAgentHandoff, 'function');
  const task = workbench.scriptAgentHandoff({ projectId: 'p1', scriptId: 's1', version: 4, title: '标题', body: '第一段 {@素材}\n\n第二段🙂', dirty: true });
  assert.match(task, /p1/); assert.match(task, /s1/); assert.match(task, /修订 4/);
  assert.match(task, /未保存/); assert.match(task, /第一段 \{@素材\}\n\n第二段🙂/);
  assert.match(task, /projects.list/); assert.match(task, /resources.read/); assert.match(task, /不等待逐步人工定稿/);
  assert.match(task, /按说话内容找剪点用 video-use/);
  assert.match(task, /按画面事件找剪点用 vac-visual-summary/);
  assert.match(task, /以新画面为主用 vac-draw-pipeline/);
  assert.match(task, /同一序列，按当前序列修订交付/);
});

test('script links open the standalone workspace and preserve the selected revision', () => {
  assert.deepEqual(parseWorkspaceHash('#/script?script=s1&version=3&segment=0'), { workspace: 'script', scriptId: 's1', scriptVersion: 3, scriptSegment: 0 });
  assert.deepEqual(parseWorkspaceHash('#/editing/content?script=s1&version=3'), { workspace: 'script', scriptId: 's1', scriptVersion: 3 });
  assert.equal(buildWorkspaceHash({ workspace: 'script', scriptId: 's1', scriptVersion: 3 }), '/script?script=s1&version=3');
  assert.equal(buildWorkspaceHash({ workspace: 'editing', editingSection: 'content', scriptId: 's1' }), '/script?script=s1');
});

test('imported shot cards complete in the workbench and remain valid portable references', async () => {
 const {directiveCompletions,readDirective}=await import('./src/scriptGrammar.ts');
 const custom=[{id:'user-title',summary:'我的标题',styles:[{id:'default',description:'我的标题'}]}];
 assert.equal(directiveCompletions('{#user-ti',9,'code',custom)[0].text,'{#user-title}');
 assert.equal(readDirective('#user-title/default').error,'');
 assert.match(readDirective('#user-title/missing').error,/未知镜头卡/);
});
