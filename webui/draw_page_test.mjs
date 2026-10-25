import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { DRAW_HANDOFF_ACTIONS, DRAW_ZONES, drawAgentContext, drawDraftChanged, newDrawComposition, newDrawImage, drawImagePosition, drawImageReference, drawRoute, zoneForFocus } from './src/drawContracts.ts';
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

test('draw page keeps four accessible zones and previews rendered media outside the source runtime', () => {
  DRAW_ZONES.forEach((zone, index) => {
    assert(page.includes('data-draw-zone="'+zone.id+'"'));
    assert(page.includes('aria-label={DRAW_ZONES['+index+'].label}'));
  });
  assert.match(page,/构图时间轴/);
  assert.match(page,/drawApi.render/);
  assert.match(page,/<video /);
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
