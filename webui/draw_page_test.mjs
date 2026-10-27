import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { DRAW_HANDOFF_ACTIONS, DRAW_ZONES, drawAgentContext, drawDraftChanged, newDrawComposition, newDrawImage, drawImagePosition, drawImageReference, drawRoute, zoneForFocus, unifiedComposition, reconcileDrawDraft, acceptDrawSave } from './src/drawContracts.ts';
import { WORKSPACE_KEYS, WORKSPACES, buildWorkspaceHash, parseWorkspaceHash } from './src/navigation.ts';
import { locationCrumbs, stageOfRoute } from './src/pipeline.ts';
import * as drawStatus from './src/drawContracts.ts';

const page = readFileSync(new URL('./src/workspaces/DrawWorkspace.tsx', import.meta.url), 'utf8');
const app = readFileSync(new URL('./src/App.tsx', import.meta.url), 'utf8');
const shortcuts = readFileSync(new URL('./src/hooks/useKeyboardShortcuts.ts', import.meta.url), 'utf8');

test('draw task status reports actual ownership, phase and expired leases',()=>{
 assert.equal(typeof drawStatus.drawAgentStatus,'function');
 const now=Date.parse('2026-10-27T12:00:00Z');
 const run={state:'queued',execution_mode:'external_mcp',owner:'human:desktop',created_at:'2026-10-27T11:59:00Z'};
 assert.match(drawStatus.drawAgentStatus(run,now),/等待.*领取/);
 const claimed={...run,owner:'agent:editor',lease_until:'2026-10-27T12:01:00Z',progress:{phase:'generating',output_chars:128,started_at:'2026-10-27T11:59:30Z',updated_at:'2026-10-27T11:59:35Z'}};
 assert.match(drawStatus.drawAgentStatus(claimed,now),/生成/);
 assert.match(drawStatus.drawAgentStatus(claimed,now),/128/);
 assert.match(drawStatus.drawAgentStatus(claimed,now),/上次更新 25 秒前/);
 assert.match(drawStatus.drawAgentStatus(claimed,now+120000),/超时/);
 assert.match(drawStatus.drawAgentStatus({...claimed,state:'waiting_human',reason:'tool_failed'},now),/暂停/);
 assert.match(drawStatus.drawAgentStatus({...claimed,state:'succeeded'},now),/完成/);
});

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


test('draw drafts distinguish picture content from saved revisions', () => {
  for (const engine of ['hyperframes','remotion']) {
    const original = {...newDrawComposition(engine),id:'slot',revision:1,content_version:'saved'};
    assert.equal(original.fps.num/original.fps.den,30);
    assert.equal(drawDraftChanged(structuredClone(original),original),false);
    assert.equal(drawDraftChanged({...original,revision:2},original),false);
    for (const patch of [{source:original.source+' '},{name:'改名'},{width:1280},{duration_frames:30},{fps:{num:24,den:1}}]) {
      assert.equal(drawDraftChanged({...original,...patch},original),true);
    }
  }
  assert.equal(drawDraftChanged(undefined,undefined),false);
  const context=drawAgentContext({project_id:'p',engine:'remotion',sequence_id:'seq'});
  for(const text of ['strategy=draw','shell=unavailable','transport=local-worker','draw.list/save/check/render/get_frame/import','timeline.read/edit'])assert(context.includes(text),text);
  assert.equal(DRAW_HANDOFF_ACTIONS.length,3);
});

test('draw API scopes identifiers, CAS and idempotency and propagates request cancellation', async () => {
  const {drawApi}=await import('./src/drawApi.ts');
  const old=globalThis.fetch,calls=[],controller=new AbortController();
  try{
    globalThis.fetch=async(url,init)=>{calls.push({url,init});return new Response(JSON.stringify([]));};
    await drawApi.list('project/id',controller.signal);
    assert.equal(calls[0].url,'/api/projects/project%2Fid/draw-compositions');
    assert.equal(calls[0].init.signal,controller.signal);
    const save={session:'session',expected_revision:4,idempotency_key:'save-once',composition:newDrawComposition('remotion')};
    await drawApi.save('project/id',save);
    assert.deepEqual(JSON.parse(calls[1].init.body),save);
    const action={session:'session',content_version:'sha'};
    await drawApi.check('p','slot/id',action);
    await drawApi.render('p','slot/id',action);
    const insertion={...action,sequence_id:'seq',expected_revision:7,idempotency_key:'import-once'};
    await drawApi.import('p','slot/id',insertion);
    assert.equal(calls[2].url,'/api/projects/p/draw-compositions/slot%2Fid/check');
    assert.equal(calls[3].url,'/api/projects/p/draw-compositions/slot%2Fid/render');
    assert.deepEqual(JSON.parse(calls[4].init.body),insertion);
    assert.equal(drawApi.renderURL('p','slot/id','a/b'),'/api/projects/p/draw-compositions/slot%2Fid/render?version=a%2Fb');
    globalThis.fetch=async()=>new Response(JSON.stringify({error:{code:'conflict',message:'源码版本已更新'}}),{status:409});
    await assert.rejects(drawApi.render('p','slot',action),error=>error.status===409&&error.code==='conflict');
  }finally{globalThis.fetch=old;}
});

test('draw page keeps accessible zones and live pixels outside the app source runtime', () => {
  DRAW_ZONES.forEach((zone, index) => {
    assert(page.includes('data-draw-zone="'+zone.id+'"'));
    assert(page.includes('aria-label={DRAW_ZONES['+index+'].label}'));
  });
  assert.match(page,/构图时间轴/);
  assert.match(page,/drawApi.render/);
  assert.match(page,/useDrawPreview/);
  assert.match(page,/当前构图合成帧/);
  assert.doesNotMatch(page,/Radio.Group/);
  assert.doesNotMatch(page,/srcDoc|dangerouslySetInnerHTML|DRAW_HANDOFF_STUB|方案接线中/);
  assert.match(readFileSync(new URL('./src/styles.css',import.meta.url),'utf8'),/\.draw-studio \{/);
});


test('image instances keep resource identity, independent placement and source references',()=>{
 const resource={id:'photo',name:'同名图片',kind:'image',source:{sha256:'a'.repeat(64)},metadata:{width:1000,height:500},missing:false};
 const c=newDrawComposition('remotion'),a=newDrawImage(resource,c),b=newDrawImage(resource,c);
 assert.notEqual(a.id,b.id);assert.equal(a.resource_id,b.resource_id);
 assert.equal(a.width/a.height,2);assert(a.width<=c.width/2&&a.height<=c.height/2);
 const animated={...a,x:10,y:20,dx:100,dy:-40,from_frame:10,to_frame:30};
 assert.deepEqual(drawImagePosition(animated,0),{x:10,y:20});
 assert.deepEqual(drawImagePosition(animated,20),{x:60,y:0});
 assert.deepEqual(drawImagePosition(animated,60),{x:110,y:-20});
 assert.equal(drawImageReference('hyperframes',{...a,id:'hero'}),'vac-image:hero');
 assert.equal(drawImageReference('remotion',{...a,id:'hero'}),'images["hero"].src');
 const saved={...c,id:'composition',images:[a]};
 assert.equal(drawDraftChanged({...saved,images:[{...a,x:a.x+1}]},saved),true);
 assert.equal(drawDraftChanged({...saved,images:[{...a}]},saved),false);
});


test('default drawing is unified and legacy conversion preserves original source, instances and bindings',()=>{
 const fresh=newDrawComposition();assert.equal(fresh.engine,'unified');assert.equal(fresh.document.preset,'auto');assert(fresh.document.nodes.some(n=>n.kind==='text'));
 const old=newDrawComposition('hyperframes');old.images=[{id:'hero',resource_id:'image',resource_sha256:'a'.repeat(64),x:10,y:20,width:100,height:60,dx:30,dy:40,from_frame:0,to_frame:29,mode:'source'}];
 const before=JSON.stringify(old),converted=unifiedComposition(old);assert.equal(JSON.stringify(old),before,'opening legacy state must not mutate it');
 assert.equal(converted.document.sources[0].source,old.source);assert.equal(converted.document.sources[0].image_bindings[0].id,'hero');assert.equal(converted.document.nodes[1].hidden,true);assert.equal(converted.document.nodes[1].keyframes[1].x,40);
 const changed=structuredClone(fresh);changed.document.nodes[0].transform.x+=10;assert(drawDraftChanged(changed,fresh));
});


test('incoming Agent changes refresh an untouched canvas and never replace a local draft',()=>{
 const base={...newDrawComposition(),id:'shared',revision:1,content_version:'v1'};
 const incoming=structuredClone(base);incoming.revision=2;incoming.content_version='v2';incoming.document.nodes[0].text='Agent result';
 assert.equal(reconcileDrawDraft(structuredClone(base),base,incoming).draft.document.nodes[0].text,'Agent result');
 const local=structuredClone(base);local.document.nodes[0].text='Human draft';const conflict=reconcileDrawDraft(local,base,incoming);assert(conflict.conflict);assert.equal(conflict.draft.document.nodes[0].text,'Human draft');
 const dependency=structuredClone(base);dependency.content_version='asset-new';const merged=reconcileDrawDraft(local,base,dependency);assert(!merged.conflict);assert.equal(merged.draft.document.nodes[0].text,'Human draft');assert.equal(merged.draft.content_version,'asset-new');
});


test('an autosave response preserves edits typed while the source request was in flight',()=>{
 const snapshot={...newDrawComposition(),id:'drawing',revision:1,content_version:'before'};
 const current=structuredClone(snapshot);current.document.nodes[0].text='Typed during save';const saved=structuredClone(snapshot);saved.revision=2;saved.content_version='saved';
 const next=acceptDrawSave(current,snapshot,saved);assert.equal(next.document.nodes[0].text,'Typed during save');assert.equal(next.revision,2);assert.equal(next.content_version,'saved');assert(drawDraftChanged(next,saved));
 assert.equal(acceptDrawSave(structuredClone(snapshot),snapshot,saved).revision,2);
 const switched={...newDrawComposition(),id:'other-project'};assert.equal(acceptDrawSave(switched,snapshot,saved).id,'other-project');
});
