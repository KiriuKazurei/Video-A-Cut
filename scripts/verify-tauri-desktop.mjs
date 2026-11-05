// Canonical visible Tauri regression: current AVP workspace, real Go and native dialogs.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { createServer } from 'node:net';
import { createHash } from 'node:crypto';
import { CDP, sleep } from './tauri-cdp.mjs';
import { testStorageRoot } from './test-paths.mjs';
const [portArg, pidArg, directoryArg] = process.argv.slice(2);
const testVersion = JSON.parse(await fs.readFile(path.resolve('desktop/tauri.conf.json'), 'utf8')).version;
let pid = Number(pidArg), c, launched, originalClipboard;
const out = path.resolve(directoryArg), port = Number(portArg);
assert(out.startsWith(testStorageRoot + path.sep), 'Versioned output required');
await fs.mkdir(out, {recursive:true});
const checks = [], report = {checks, mode:'visible_tauri_avp_real_go_native_dialogs', human_acceptance:'pending'};

const record = async (name, detail) => { checks.push({name, pass:true, detail}); await fs.writeFile(path.join(out,'results.json'),JSON.stringify(report,null,2)); console.log(name); };
const run = (exe,args) => new Promise((resolve,reject) => { const child=spawn(exe,args,{windowsHide:true,stdio:['ignore','pipe','pipe']}); let text=''; child.stdout.on('data',b=>text+=b); child.stderr.on('data',b=>text+=b); child.on('error',reject); child.on('exit',code=>code===0?resolve(text):reject(Error(text))); });
const probe = (action,extra=[]) => run('C:/Program Files/PowerShell/7/pwsh.exe',['-File','scripts/Tauri-WindowProbe.ps1','-DesktopPid',String(pid),'-Action',action,...extra]);
const until = async (read,match,timeout=30000) => { const deadline=Date.now()+timeout;let last;while(Date.now()<deadline){last=await read();if(match(last))return last;await sleep(150)}const state=last?.document?{id:last.id,project:last.document.project.name,dirty:last.dirty,active_tasks:last.active_tasks,scripts:last.document.scripts.map(s=>({id:s.id,title:s.title})),resources:last.document.resources.map(r=>({id:r.id,name:r.name,notes:r.notes,removed:r.removed}))}:last;throw Error('State timeout '+JSON.stringify(state)); };
const click = async (text, selector='button') => { await c.wait(`[...document.querySelectorAll(${JSON.stringify(selector)})].some(n=>n.offsetParent!==null&&!n.disabled&&n.textContent.trim()===${JSON.stringify(text)})`); await c.ev(`[...document.querySelectorAll(${JSON.stringify(selector)})].find(n=>n.offsetParent!==null&&!n.disabled&&n.textContent.trim()===${JSON.stringify(text)}).click()`); };
const pointerClick = async selector => { await c.wait(`(()=>{const n=document.querySelector(${JSON.stringify(selector)});return n&&n.offsetParent!==null})()`);await sleep(400);const point=await c.ev(`(()=>{const n=document.querySelector(${JSON.stringify(selector)});n.scrollIntoView({block:'nearest'});const r=n.getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2}})()`);await c.send('Input.dispatchMouseEvent',{type:'mouseMoved',...point});await c.send('Input.dispatchMouseEvent',{type:'mousePressed',button:'left',clickCount:1,...point});await c.send('Input.dispatchMouseEvent',{type:'mouseReleased',button:'left',clickCount:1,...point}); };
const shortcut = async (key,shift=false) => { await c.wait(`document.querySelector('.project-menu-bar')?.getAttribute('aria-busy')!=='true'`);await sleep(150); const code=key.toUpperCase().charCodeAt(0);await c.send('Input.dispatchKeyEvent',{type:'keyDown',key,code:'Key'+key.toUpperCase(),windowsVirtualKeyCode:code,modifiers:2|(shift?8:0)});await c.send('Input.dispatchKeyEvent',{type:'keyUp',key,code:'Key'+key.toUpperCase(),windowsVirtualKeyCode:code,modifiers:0}); };
const input = async (selector,value) => { await c.wait(`(()=>{const n=document.querySelector(${JSON.stringify(selector)});return n&&n.offsetParent!==null&&!n.disabled&&!n.readOnly})()`);await c.ev(`(()=>{const n=document.querySelector(${JSON.stringify(selector)});n.focus();Object.getOwnPropertyDescriptor(n instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,'value').set.call(n,${JSON.stringify(value)});n.dispatchEvent(new Event('input',{bubbles:true}));n.dispatchEvent(new Event('change',{bubbles:true}));})()`); };
const menu = async (top,item) => {
  await c.wait(`document.querySelector('.project-menu-bar')?.getAttribute('aria-busy')!=='true'`);
  await c.wait(`[...document.querySelectorAll('.project-menu-bar .ant-menu-submenu-title')].some(n=>n.textContent.trim()===${JSON.stringify(top)}&&n.getAttribute('aria-disabled')!=='true')`);
  const point=await c.ev(`(()=>{const n=[...document.querySelectorAll('.project-menu-bar .ant-menu-submenu-title')].find(n=>n.textContent.trim()===${JSON.stringify(top)});const r=n.getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2}})()`);
  await c.send('Input.dispatchMouseEvent',{type:'mouseMoved',x:700,y:110});await sleep(100);
  await c.send('Input.dispatchMouseEvent',{type:'mouseMoved',...point});
  await c.wait(`[...document.querySelectorAll('.ant-menu-submenu-popup [role=menuitem]')].some(n=>n.offsetParent!==null&&n.textContent.trim()===${JSON.stringify(item)})`);
  // Ant's popup enters with a transform. Wait before measuring its final hit box.
  await sleep(400);
  await c.ev(`[...document.querySelectorAll('.ant-menu-submenu-popup [role=menuitem]')].find(n=>n.offsetParent!==null&&n.textContent.trim()===${JSON.stringify(item)}).focus({preventScroll:true})`);
  await c.send('Input.dispatchKeyEvent',{type:'keyDown',key:'Enter',code:'Enter',windowsVirtualKeyCode:13});await c.send('Input.dispatchKeyEvent',{type:'keyUp',key:'Enter',code:'Enter',windowsVirtualKeyCode:13});
  await c.send('Input.dispatchMouseEvent',{type:'mouseMoved',x:700,y:110});await sleep(450);
};
const decideSave = async () => { await sleep(200); if(await c.ev(`Boolean([...document.querySelectorAll('button')].find(n=>n.offsetParent!==null&&n.textContent.trim()==='保存并继续'))`)) await click('保存并继续'); };
let base;
const api = async (endpoint, method='GET', body, headers={}) => { const r=await fetch(base+endpoint,{method,headers:{'Content-Type':'application/json',...headers},body:body===undefined?undefined:JSON.stringify(body)});const text=await r.text();if(!r.ok)throw Error(r.status+' '+text);return JSON.parse(text); };
const workspace = () => api('/api/workspace');
const choose = (title,file) => probe('choose',['-DialogTitle',title,'-FilePath',file]);
// OS input is sent only to the verified VAC window. CDP supplies measurements,
// not synthetic drag events; the same canonical suite retains AVP regression.
const nativePoint = async (selector,fx=.5,fy=.5) => {
  await c.wait(`document.querySelector(${JSON.stringify(selector)})?.getClientRects().length`);
  await c.ev(`document.querySelector(${JSON.stringify(selector)}).scrollIntoView({block:'nearest',inline:'nearest'})`);await sleep(150);
  if(selector.includes('[data-lane]'))await c.ev(`document.querySelector('.timeline-scroll').scrollLeft=0`);
  let previous,stable=0;
  await until(async()=>{const rect=await c.ev(`(()=>{const r=document.querySelector(${JSON.stringify(selector)}).getBoundingClientRect();return [r.left,r.top,r.width,r.height]})()`);const key=JSON.stringify(rect);stable=key===previous?stable+1:0;previous=key;return stable;},value=>value>=3,5000);
  return c.ev(`(()=>{const r=document.querySelector(${JSON.stringify(selector)}).getBoundingClientRect();return {x:Math.round((r.left+r.width*${fx})*devicePixelRatio),y:Math.round((r.top+r.height*${fy})*devicePixelRatio)}})()`);
};
const nativeClick = async (selector,action='click') => {const point=await nativePoint(selector);await probe(action,['-X',String(point.x),'-Y',String(point.y)]);};
const mcpClient = endpoint => {
  let session,protocol='2025-11-25',id=0,reinitializations=0;
  const send=async(method,params,notification=false,recover=true)=>{
    const request={jsonrpc:'2.0',method,params,...(!notification?{id:++id}:{})};
    const response=await fetch(endpoint,{method:'POST',headers:{'Content-Type':'application/json',Accept:'application/json, text/event-stream',...(session?{'Mcp-Session-Id':session,'MCP-Protocol-Version':protocol}:{})},body:JSON.stringify(request)});
    if(response.status===404&&session&&recover&&method!=='initialize'&&method!=='notifications/initialized'){
      await response.text();session=undefined;reinitializations++;await initialize();return send(method,params,notification,false);
    }
    assert(response.ok,`MCP ${method}: ${response.status}`);session=response.headers.get('Mcp-Session-Id')||session;const text=await response.text();if(notification||!text)return;
    const messages=response.headers.get('Content-Type')?.includes('text/event-stream')?text.split('\n').filter(line=>line.startsWith('data:')).map(line=>JSON.parse(line.slice(5))):[JSON.parse(text)];
    const result=messages.find(message=>message.id===request.id);assert(result&&!result.error,JSON.stringify(result));return result.result;
  };
  const initialize=async()=>{const result=await send('initialize',{protocolVersion:protocol,capabilities:{},clientInfo:{name:'VAC desktop timeline regression',version:testVersion}});protocol=result.protocolVersion;await send('notifications/initialized',{},true);};
  return {init:initialize,get reinitializations(){return reinitializations;},async call(name,args){const result=await send('tools/call',{name,arguments:args});assert(!result.isError,JSON.stringify(result));return result.structuredContent??JSON.parse(result.content.find(item=>item.type==='text').text);}};
};
const verifyTimeline = async runtime => {
  await probe('resize',['-Width','1900','-Height','1200']);
  const file=path.join(out,'多轨工程 '+pid+'.avp'),media=path.join(out,'多音轨.mp4'),script=path.join(out,'轨道引用.avs');
  await run('ffmpeg',['-v','error','-nostdin','-y','-f','lavfi','-i','testsrc2=size=320x180:rate=30:duration=3','-f','lavfi','-i','anullsrc=r=48000:cl=stereo:d=3','-f','lavfi','-i','sine=frequency=880:sample_rate=48000:duration=3','-map','0:v','-map','1:a','-map','2:a','-c:v','libx264','-threads','1','-c:a','aac',media]);
  const sourceAvs=JSON.stringify({format:'avs',schema_version:1,id:'timeline-script',title:'轨道引用',body:'轨道测试',segments:[],bindings:[]});await fs.writeFile(script,sourceAvs);
  await shortcut('n');await choose('保存 VAC 工程',file);let ws=await until(workspace,v=>v.path===file),project=ws.document.project.id,sequence=ws.document.sequences[0].id;
  const read=()=>api(`/api/projects/${project}/sequences/${sequence}`);
  assert.equal(ws.document.sequences[0].tracks.filter(t=>t.kind==='video').length,3);assert.equal(ws.document.sequences[0].tracks.filter(t=>t.kind==='audio').length,3);
  await importFile(media);await importFile(script);ws=await workspace();const resource=ws.document.resources.find(r=>r.kind==='video'),scriptResource=ws.document.resources.find(r=>r.kind==='script');
  await menu('视图','编辑');await c.wait('document.querySelector(".sequence-timeline")');await select(resource.id);
  const track=ws.document.sequences[0].tracks[0].id,from=await nativePoint(`.project-resource-list [data-resource-id="${resource.id}"]`),to=await nativePoint(`[data-track-id="${track}"] [data-lane]`,.003,.5);
  report.native_drag_hit=await c.ev(`(()=>{const n=document.elementFromPoint(${from.x}/devicePixelRatio,${from.y}/devicePixelRatio);const row=document.querySelector('.project-resource-list [data-resource-id="${resource.id}"]');return {tag:n?.tagName,html:n?.outerHTML.slice(0,800),row:row.getBoundingClientRect().toJSON(),scroll:scrollY,viewport:innerHeight}})()`);
  await c.ev(`(()=>{window.__nativeDragEvents=[];for(const type of ['pointerdown','pointerup','dragstart','dragover','drop','dragend'])document.addEventListener(type,e=>{if(window.__nativeDragEvents.length<80)window.__nativeDragEvents.push({type,x:e.clientX,y:e.clientY,tag:e.target.tagName,resource:e.target.closest?.('[data-resource-id]')?.dataset.resourceId,track:e.target.closest?.('[data-track-id]')?.dataset.trackId,types:e.dataTransfer?[...e.dataTransfer.types]:undefined})},true)})()`);
  await probe('drag',['-X',String(from.x),'-Y',String(from.y),'-EndX',String(to.x),'-EndY',String(to.y)]);
  report.native_drag_events=await c.ev('window.__nativeDragEvents');report.native_drag_points={from,to};
  let view=await until(read,v=>v.sequence.tracks.flatMap(t=>t.clips).length===3),clip=view.sequence.tracks[0].clips[0];assert.equal(clip.at,0);assert.deepEqual(view.sequence.tracks.filter(t=>t.kind==='audio').flatMap(t=>t.clips.map(c=>c.stream_index)),[1,2]);assert(view.sequence.tracks.flatMap(t=>t.clips).every(c=>c.link_id===clip.link_id));
  await record('native resource drag creates linked video and each actual audio stream',{sequence,streams:[1,2]});
  const selected=`[data-clip-id="${clip.id}"]`;await nativeClick(selected);const p=await nativePoint(selected,.6,.5),scale=await c.ev('devicePixelRatio');
  await probe('drag',['-X',String(p.x),'-Y',String(p.y),'-EndX',String(p.x+60*scale),'-EndY',String(p.y),'-CancelDrag']);await sleep(500);assert.equal((await read()).sequence.revision,view.sequence.revision);
  const retryPoint=await nativePoint(selected,.6,.5);report.drag_retry_points={before:p,after:retryPoint};
  await probe('drag',['-X',String(retryPoint.x),'-Y',String(retryPoint.y),'-EndX',String(retryPoint.x+60*scale),'-EndY',String(retryPoint.y)]);view=await until(read,v=>v.sequence.tracks[0].clips[0].at===30);clip=view.sequence.tracks[0].clips[0];assert(view.sequence.tracks.flatMap(t=>t.clips).every(c=>c.at===30));
  await record('native drag threshold, Escape cancellation and linked movement preserve authoritative frames');
  await nativeClick(selected);await probe('key',['-Key','M']);await c.wait('document.querySelector("input[aria-label=标记备注]")');await input('input[aria-label="标记备注"]','定位');await pointerClick('.ant-modal-confirm .ant-btn-primary');view=await until(read,v=>v.sequence.markers.length===1);const marker=view.sequence.markers[0];assert.equal(marker.kind,'point');
  await c.wait('!document.querySelector("input[aria-label=标记备注]")');
  if(await c.ev('!!document.querySelector(".source-monitor-badge")'))await pointerClick('.source-monitor-badge');
  assert.equal(await c.ev('document.querySelectorAll(".source-monitor .program-playback").length'),0,'source monitor must not own the program clock');
  await record('source monitor opens without duplicating the program playback owner');
  await click('源标记');await input('input[aria-label="标记备注"]','定位');await pointerClick('.ant-modal-confirm .ant-btn-primary');await until(workspace,v=>v.document.resources.find(r=>r.id===resource.id).markers?.some(m=>m.notes==='定位'));await c.wait('!document.querySelector("input[aria-label=标记备注]")');
  await c.ev(`document.querySelector('[data-resource-id="${scriptResource.id}"]').dispatchEvent(new MouseEvent('dblclick',{bubbles:true}))`);await c.wait('document.querySelector("#script-body")');await input('#script-body','{@定');await c.ev('document.querySelector("#script-body").focus()');await c.send('Input.dispatchKeyEvent',{type:'keyUp',key:'定'});
  await c.wait('document.querySelector(".script-completion")?.innerText.includes("时间线标记")&&document.querySelector(".script-completion")?.innerText.includes("素材标记")');
  await c.ev(`[...document.querySelectorAll('.script-completion button')].find(n=>n.textContent.includes('时间线标记')).click()`);await shortcut('s');ws=await until(workspace,v=>v.document.scripts[0].references?.some(r=>r.target_id===marker.id));
  const revision=view.sequence.revision;await c.ev('document.querySelector("#script-body").focus()');await probe('key',['-Key','M']);await sleep(200);assert.equal((await read()).sequence.markers.length,1);assert(!(await c.ev('!!document.querySelector("input[aria-label=标记备注]")')));
  await input(`input[aria-label="标记备注 ${marker.id}"]`,'新的定位');await c.ev('document.querySelector("#script-body").focus()');await until(workspace,v=>v.document.scripts[0].body.includes('{@新的定位}'));assert.equal(await fs.readFile(script,'utf8'),sourceAvs);
  await record('Chinese completion distinguishes namesakes, binds a single point, guards input keys and synchronizes only bound rename',{marker:marker.id,prior_revision:revision});
  // Split via actual keyboard; the right-click menu is reached with OS input.
  await nativeClick(selected);const lane=await nativePoint(`[data-track-id="${track}"] [data-lane]`,0,.5),ruler=await nativePoint('.timeline-scale',0,.16);await probe('click',['-X',String(lane.x+105*scale),'-Y',String(ruler.y)]);await nativeClick(selected,'rightclick');await c.wait('[...document.querySelectorAll("[role=menuitem]")].some(n=>n.offsetParent!==null&&n.textContent.includes("在播放头分割"))');await probe('key',['-Key','ESC']);await nativeClick(selected);await probe('key',['-Key','CTRL+K']);view=await until(read,v=>v.sequence.tracks[0].clips.length===2);
  await probe('key',['-Key','CTRL+Z']);await until(read,v=>v.sequence.tracks[0].clips.length===1);await probe('key',['-Key','CTRL+SHIFT+Z']);view=await until(read,v=>v.sequence.tracks[0].clips.length===2);
  await record('native context menu and Ctrl+K, undo and redo apply one linked editing transaction');
  await click('复制序列');ws=await until(workspace,v=>v.document.sequences.length===2);const second=ws.document.sequences[1];assert.notEqual(second.tracks[0].clips[0].id,view.sequence.tracks[0].clips[0].id);await c.ev(`[...document.querySelectorAll('.sequence-tabs .ant-tabs-tab')].find(n=>n.textContent.includes('主序列')&&!n.textContent.includes('副本')).click()`);
  await c.wait('document.querySelector(".source-monitor video")?.readyState>=2');
  const mcp=mcpClient(runtime.mcp_url);await mcp.init();const listing=await mcp.call('sequences.list',{project_id:project});assert(listing.sequences.every(s=>!s.history?.length&&!s.history_cursor));const pack=await mcp.call('skills.get',{id:'vac-visual-summary'});await mcp.call('skills.read_resource',{id:pack.id,content_sha256:pack.content_sha256,resource:'vac/tool-contracts.md'});const visible=await mcp.call('projects.visible_set.get',{id:project});
  let agentRun=await mcp.call('skill_runs.create',{project_id:project,sequence_id:sequence,flow:'visual_heavy',skill_id:pack.id,execution_mode:'external_mcp',visible_set_sha:visible.sha256,goal:'测试结构化轨道编辑，不调用服务商',idempotency_key:'native-sequence-'+pid});
  const tool=async(name,args)=>{const response=await mcp.call('skill_runs.tool',{run_id:agentRun.id,request:{name,arguments:args,expected_version:agentRun.version,idempotency_key:'native-'+name+'-'+agentRun.version}});agentRun=response.run;return response.result;};
  view=await tool('timeline.read',{});assert(!view.history?.length);clip=view.sequence.tracks[0].clips[0];view=await tool('timeline.edit',{sequence_id:sequence,command:{session:view.session,expected_revision:view.sequence.revision,idempotency_key:'native-agent-effect',label:'Agent 测试效果',operations:[{type:'clip.update',clip_ids:[clip.id],effects:{...clip.effects,fade_in:3,gain_db:-3}}]}});
  const finished=await tool('run.finish',{sequence_revision:view.sequence.revision,content_version:view.content_version});assert(finished.completed&&finished.structured_edit);const agentClip=view.sequence.tracks[0].clips[0];await c.wait(`document.querySelector('[data-clip-id="${agentClip.id}"]')?.classList.contains('is-agent')`);
  const privateView=await mcp.call('sequences.get',{project_id:project,sequence_id:sequence});assert(!privateView.history?.length);assert(!(await c.ev(`document.querySelector('[data-clip-id="${agentClip.id}"]').textContent`)).includes('undefined'));await record('real MCP structured edit is purple on actual clips, renders without a provider and hides human history',{run:agentRun.id,content_version:view.content_version});
  await c.wait('document.querySelector(".sequence-timeline")?.offsetParent!==null');await c.wait('document.querySelector(".program-monitor video")?.readyState>=2',60000);assert(await c.ev(`!!document.querySelector('[data-clip-id="${agentClip.id}"] .clip-effect-scope')`));await nativeClick(`[data-clip-id="${agentClip.id}"]`);await probe('key',['-Key','SPACE']);await c.wait('document.querySelector(".program-monitor video")?.paused===false');await probe('key',['-Key','SPACE']);await c.wait('document.querySelector(".program-monitor video")?.paused===true');const exported=path.join(out,`sequence-export ${pid}.mp4`);await click('导出当前范围');await choose('导出当前序列轨道范围',exported);await until(async()=>{try{return (await fs.stat(exported)).size>0}catch{return false}},Boolean,60000);const hash=createHash('sha256').update(await fs.readFile(exported)).digest('hex');assert.equal(hash,finished.artifact.sha256);
  await probe('capture',['-FilePath',path.join(out,'native-timeline-wide.png')]);
  await probe('resize',['-Width','1000','-Height','720']);await c.wait('document.querySelector(".compact-monitor-switch")?.offsetParent!==null');
  await sleep(300);const trackLayout=await c.ev(`(()=>{const sc=document.querySelector('.timeline-scroll').getBoundingClientRect(),timeline=document.querySelector('.sequence-timeline').getBoundingClientRect(),footer=document.querySelector('.app-footer').getBoundingClientRect();return {scroll:{top:sc.top,bottom:sc.bottom},timelineBottom:timeline.bottom,footerTop:footer.top,centers:[${JSON.stringify(track)},${JSON.stringify(view.sequence.tracks.find(t=>t.kind==='audio').id)}].map(id=>{const r=document.querySelector('[data-track-id="'+id+'"]').getBoundingClientRect();return r.top+r.height/2})}})()`);assert(trackLayout.timelineBottom<=trackLayout.footerTop+1&&trackLayout.centers.every(y=>y>=trackLayout.scroll.top+52&&y<=trackLayout.scroll.bottom));report.compact_track_layout=trackLayout;
  const layout=await c.ev(`(()=>{const v=document.querySelector('.program-monitor video.program-video'),r=v.getBoundingClientRect(),s=v.closest('.editor-monitor-screen').getBoundingClientRect();return {video:{w:r.width,h:r.height},screen:{w:s.width,h:s.height},controls:v.controls||[...document.querySelectorAll('.program-monitor .editor-monitor-controls button')].some(n=>n.offsetParent!==null&&!n.disabled&&n.textContent.includes('播放')),bodyWidth:document.documentElement.scrollWidth,viewport:innerWidth}})()`);report.program_layout=layout;assert(layout.controls&&layout.video.w>0&&layout.video.h>0&&layout.video.h<=layout.screen.h+1&&layout.video.w<=layout.screen.w+1&&layout.bodyWidth<=layout.viewport+1);await c.screenshot(path.join(out,'timeline-compact.png'));await record('program preview and export share a hash; compact monitors retain the complete frame and playback controls',{hash,layout});
  await shortcut('s');await until(workspace,v=>!v.dirty);const saved=JSON.parse(await fs.readFile(file));assert.equal(saved.schema_version,3);assert.equal(saved.sequences.length,2);assert(saved.scripts[0].references.some(r=>r.target_id===marker.id));
  await menu('文件','关闭项目');await until(workspace,v=>!v.document);await shortcut('o');await choose('打开 VAC 工程',file);ws=await until(workspace,v=>v.document?.project.id===project);assert.equal(ws.document.sequences.length,2);assert(ws.document.scripts[0].body.includes('新的定位'));await record('multisequence AVP v2 reopens with stable clips, effects and typed script bindings');
  await verifyTimelineReopen(saved,sequence);
};
const verifyTimelineReopen=async(saved,sequence)=>{
  const savedResource=saved.workspace.selected_resource;
  assert(saved.resources.some(resource=>resource.id===savedResource),'saved selection must identify a saved resource');
  await probe('resize',['-Width','1900','-Height','1200']);await c.wait(`document.querySelector('.project-resource-list [data-resource-id="${savedResource}"]')?.getAttribute('aria-pressed')==='true'`);
  if(!await c.ev('!!document.querySelector(".sequence-timeline")?.offsetParent'))await menu('视图','编辑');
  const savedHead=saved.workspace.sequence_states[sequence].head,nominal=Math.round(saved.sequences[0].fps.num/saved.sequences[0].fps.den),seconds=Math.floor(savedHead/nominal),savedLabel=[Math.floor(seconds/3600),Math.floor(seconds/60)%60,seconds%60,savedHead%nominal].map(n=>String(n).padStart(2,'0')).join(':');
  try{await c.wait('document.querySelector(".program-monitor video.program-video")?.readyState>=2',60000);}catch(error){report.playback_diagnostics=await c.ev(`({videos:[...document.querySelectorAll('.program-monitor video')].map(v=>({className:v.className,ready:v.readyState,time:v.currentTime,duration:v.duration,error:v.error?.message,buffered:Array.from({length:v.buffered.length},(_,i)=>[v.buffered.start(i),v.buffered.end(i)])})),messages:window.__VAC_TEST_MESSAGES})`).catch(diagnosticError=>({unavailable:String(diagnosticError)}));throw error;}await c.wait(`document.querySelector('.timeline-ruler>.track-heading')?.textContent===${JSON.stringify(savedLabel)}`);
  const restoredMedia=await c.ev(`(()=>{const v=document.querySelector('.program-monitor video.program-video');return {ready:v.readyState,time:v.currentTime}})()`);assert(Math.abs(restoredMedia.time-savedHead/nominal)<=1/nominal);await record('saved program preview is ready at the restored nonzero media time',{head:savedHead,...restoredMedia});
  await shortcut('s');await until(workspace,v=>!v.dirty);
  await c.wait(`document.title===${JSON.stringify('Video Auto Cut — '+saved.project.name)}`);
  await c.send('Page.reload');await c.wait(`document.querySelector('.project-resource-list [data-resource-id="${savedResource}"]')?.getAttribute('aria-pressed')==='true'`);await c.wait(`document.querySelector('.timeline-ruler>.track-heading')?.textContent===${JSON.stringify(savedLabel)}`);await c.wait(`document.title===${JSON.stringify('Video Auto Cut — '+saved.project.name)}`);const ws=await workspace();assert(!ws.dirty&&ws.document.workspace.selected_resource===savedResource);await record('page reload restores saved selection and nonzero playhead without a spurious dirty layout',{head:savedHead,resource_id:savedResource});
  await shortcut('s');await until(workspace,v=>!v.dirty);await menu('文件','关闭项目');await until(workspace,v=>!v.document);await probe('resize',['-Width','1900','-Height','1200']);
};
// Focused drawing mode reuses the native runner, dialogs, MCP transport and
// shutdown assertions rather than keeping a second desktop acceptance suite.
const verifyUnifiedDrawing = async runtime => {
 await probe('resize',['-Width','1600','-Height','1050']);
 const file=path.join(out,'统一绘制 '+pid+'.avp');
 await shortcut('n');await choose('保存 VAC 工程',file);let ws=await until(workspace,v=>v.path===file);
 const project=ws.document.project.id,sequence=ws.document.sequences[0].id;
 const image=process.env.VAC_DRAW_TEST_IMAGE||path.resolve('.run-data/tests',testVersion,'cache/media-fixtures/Sony-ILCE-7M3.ARW');
 await fs.access(image);await importFile(image);ws=await workspace();const resource=ws.document.resources.find(r=>r.kind==='image');assert(resource);
 await menu('视图','绘制');await click('新建构图');await c.wait('document.querySelector(".draw-preview-status")?.textContent.startsWith("当前画面")',60000);
 assert.equal(await c.ev('document.querySelectorAll(".draw-toolbar input[type=radio]").length'),0);
 await click('素材','.ant-tabs-tab-btn');await click(resource.name);await c.wait('document.querySelector(".draw-selection")',60000);
 await c.wait('document.querySelector(".draw-preview-status")?.textContent.startsWith("当前画面")',60000);
 const x=Number(await c.ev(`document.querySelector('input[aria-label="位置 X"]').value`));
 const from=await nativePoint('.draw-selection',.25,.4),scale=await c.ev('devicePixelRatio');
 await probe('drag',['-X',String(from.x),'-Y',String(from.y),'-EndX',String(from.x+60*scale),'-EndY',String(from.y+25*scale)]);
 await c.wait(`Number(document.querySelector('input[aria-label="位置 X"]').value)>${x+10}`);
 const dragged=Number(await c.ev(`document.querySelector('input[aria-label="位置 X"]').value`));
 await nativeClick('button[aria-label="撤销绘制修改"]');await c.wait(`Math.abs(Number(document.querySelector('input[aria-label="位置 X"]').value)-${x})<1`);
 await nativeClick('button[aria-label="重做绘制修改"]');await c.wait(`Math.abs(Number(document.querySelector('input[aria-label="位置 X"]').value)-${dragged})<1`);
 await shortcut('s');ws=await until(workspace,v=>!v.dirty);
 await record('native image file import, automatic canvas, pointer move and atomic gesture undo/redo',{resource:resource.id,path:image,x,dragged});
 const composition=ws.document.draw_compositions[0],d=structuredClone(composition.document);
 d.canvas={width:1280,height:720,fps:{num:30,den:1},color_space:'srgb',alpha:'straight'};d.duration_frames=30;d.preset='interface';d.goal='图片背景上的界面演示与数据卡同步呈现';d.explanation='保留图片素材，以界面动效与数据卡组合呈现。';d.variables={series:[36,68,92]};d.theme={accent:'#6c91ff'};d.seed=7;
 d.nodes=d.nodes.map(n=>({...n,duration:30,transform:n.kind==='image'?{x:0,y:0,width:1280,height:720}:n.transform}));
 const title=d.nodes.find(n=>n.kind==='text');title.text='一个画布 · 同步绘制';title.font_size=62;title.transform={x:72,y:48,width:1100,height:100};title.z=6;
 d.sources=[{id:'interface',engine:'hyperframes',name:'界面演示',reason:'HTML 布局和 GSAP 动效',canvas:{width:600,height:370,fps:{num:30,den:1}},duration_frames:30,source:`<!doctype html><html><head><style>body{margin:0;font-family:Arial,sans-serif;color:#eff3ff;background:transparent}#root{width:600px;height:370px;box-sizing:border-box;padding:30px;background:#171e31;border:1px solid #405477;border-radius:18px}.dots{color:#88a8ff;font-size:16px;letter-spacing:7px}.row{height:46px;margin-top:20px;border-radius:8px;background:#273651;width:90%}#active{background:#567de4;width:55%}h2{font-size:28px}#cursor{position:absolute;width:18px;height:18px;top:260px;left:70px;border:3px solid white;border-radius:50%}</style></head><body><div id="root" data-composition-id="ui" data-width="600" data-height="370" data-duration="1"><div class="dots">● ● ●</div><h2>Interface / live canvas</h2><div class="row" id="active"></div><div class="row"></div><div id="cursor"></div></div><script>const tl=gsap.timeline({paused:true});tl.to('#active',{width:'90%',duration:1,ease:'none'},0).to('#cursor',{x:360,y:-40,duration:1,ease:'none'},0);window.__timelines.ui=tl;</script></body></html>`},{id:'data',engine:'remotion',name:'数据卡',reason:'复用 React SVG 逐帧数据组件',canvas:{width:460,height:370,fps:{num:30,den:1}},duration_frames:30,source:`import React from 'react';import {useCurrentFrame} from 'remotion';export default function Data({series,theme}){const f=useCurrentFrame();return <div style={{width:460,height:370,boxSizing:'border-box',background:'#171e31',border:'1px solid #405477',borderRadius:18,padding:30,color:'#eff3ff',fontFamily:'Arial,sans-serif'}}><div style={{fontSize:28,marginBottom:20}}>Data / same frame</div><svg width="390" height="240">{series.map((v,i)=><g key={i}><rect x={28+i*120} y={215-v*(.5+f/29)*1.2} width="70" height={v*(.5+f/29)*1.2} rx="7" fill={theme.accent}/><text x={63+i*120} y="238" textAnchor="middle" fill="#c6d2ed" fontSize="18">{v}%</text></g>)}</svg></div>}` }];
 d.nodes.push({id:'shade',kind:'shape',shape:'rect',color:'#07101f',opacity:.8,from:0,duration:30,transform:{x:0,y:0,width:1280,height:720},z:3},{id:'web',name:'界面演示',kind:'source',source_id:'interface',from:0,duration:30,transform:{x:72,y:230,width:600,height:370},z:4},{id:'chart',name:'数据卡',kind:'source',source_id:'data',from:0,duration:30,transform:{x:716,y:230,width:460,height:370},z:4});
 const mcp=mcpClient(runtime.mcp_url);await mcp.init();const pack=await mcp.call('skills.get',{id:'vac-draw-pipeline'});await mcp.call('skills.read_resource',{id:pack.id,content_sha256:pack.content_sha256,resource:'vac/tool-contracts.md'});const visible=await mcp.call('projects.visible_set.get',{id:project});
 let agentRun=await mcp.call('skill_runs.create',{project_id:project,sequence_id:sequence,flow:'custom',skill_id:pack.id,strategy:'draw',execution_mode:'external_mcp',visible_set_sha:visible.sha256,goal:d.goal,idempotency_key:'native-draw-'+pid});
 const tool=async(name,args)=>{const response=await mcp.call('skill_runs.tool',{run_id:agentRun.id,request:{name,arguments:args,expected_version:agentRun.version,idempotency_key:'draw-'+agentRun.version+'-'+name}});agentRun=response.run;return response.result;};
 await tool('draw.capabilities',{});
 agentRun=await mcp.call('skill_runs.progress',{run_id:agentRun.id,expected_version:agentRun.version,phase:'generating',output_chars:128});
 await c.ev(`location.hash=${JSON.stringify('/draw?project='+project+'&slot='+composition.id+'&run='+agentRun.id+'&focus=canvas')}`);
 await c.wait(`document.querySelector('.draw-agent-status')?.textContent.includes('正在生成画面')&&document.querySelector('.draw-agent-status')?.textContent.includes('128')`,15000);
 await c.screenshot(path.join(out,'native-agent-progress.png'));
 await record('real MCP heartbeat and visible drawing status show generation phase and output count without model text',{run:agentRun.id,output_chars:128});
 ws=await workspace();await tool('draw.save',{session:ws.id,expected_revision:ws.revision,idempotency_key:'native-mixed',composition:{...composition,document:d}});ws=await workspace();const mixed=ws.document.draw_compositions[0];
 await c.wait(`document.body.innerText.includes('界面演示')`,60000);await c.wait(`!document.querySelector('button[aria-label="撤销绘制修改"]').disabled`);
 await nativeClick('button[aria-label="撤销绘制修改"]');await until(workspace,v=>v.document.draw_compositions[0].document.sources.length===0);
 await c.wait(`!document.querySelector('button[aria-label="重做绘制修改"]').disabled`);await nativeClick('button[aria-label="重做绘制修改"]');await until(workspace,v=>v.document.draw_compositions[0].document.sources.length===2);
 await record('native scene undo/redo restores the complete Agent transaction without rewinding runs or original media');
 await tool('draw.check',{session:ws.id,composition_id:mixed.id,content_version:mixed.content_version});
 for(const frame of [0,15,29]){const result=await tool('draw.preview',{session:ws.id,composition_id:mixed.id,content_version:mixed.content_version,frame,request_id:'native-frame-'+frame});await tool('artifacts.get',{id:result.artifact.id});}
 await c.wait(`document.querySelector('.draw-layer-item')&&document.body.innerText.includes('界面演示')`,60000);await c.wait('document.querySelector(".draw-preview-status")?.textContent.startsWith("当前画面")',60000);
 await c.ev(`document.querySelector('[role=slider][aria-label="构图播放帧"]').focus()`);await c.send('Input.dispatchKeyEvent',{type:'keyDown',key:'Home',code:'Home',windowsVirtualKeyCode:36});await c.send('Input.dispatchKeyEvent',{type:'keyUp',key:'Home',code:'Home',windowsVirtualKeyCode:36});for(let n=0;n<15;n++){await c.send('Input.dispatchKeyEvent',{type:'keyDown',key:'ArrowRight',code:'ArrowRight',windowsVirtualKeyCode:39});await c.send('Input.dispatchKeyEvent',{type:'keyUp',key:'ArrowRight',code:'ArrowRight',windowsVirtualKeyCode:39});}
 await c.wait('document.querySelector(".draw-preview-status")?.textContent.includes("第 15 帧")',60000);
 await probe('capture',['-FilePath',path.join(out,'native-unified-draw.png')]);await c.screenshot(path.join(out,'unified-draw.png'));
 const tv=await tool('timeline.read',{sequence_id:sequence});const attached=await tool('draw.import',{session:ws.id,composition_id:mixed.id,content_version:mixed.content_version,sequence_id:sequence,expected_revision:tv.sequence.revision,idempotency_key:'native-source',source_mode:'snapshot'});
 const finished=await tool('run.finish',{sequence_revision:attached.sequence.revision,content_version:attached.content_version});assert(finished.completed&&finished.structured_edit);
 assert((await workspace()).document.resources.some(r=>r.draw_source?.mode==='snapshot'));
 await record('real MCP authors and previews native HTML, React data and an imported image in one canvas; source import finishes through the shared sequence renderer',{run:agentRun.id,content_version:mixed.content_version,finish:finished});
 const standalone=path.join(out,'绘制导出.mp4');await click('导出视频');await choose('导出绘制视频',standalone);await until(async()=>{try{return (await fs.stat(standalone)).size}catch{return 0}},v=>v>1000,120000);await record('native drawing export dialog writes a verified standalone video',{path:standalone});
 await shortcut('s');await until(workspace,v=>!v.dirty);const saved=JSON.parse(await fs.readFile(file));assert.equal(saved.schema_version,3);assert.equal(saved.workspace.draw_states[mixed.id].head,15);
 await menu('文件','关闭项目');await until(workspace,v=>!v.document);await shortcut('o');await choose('打开 VAC 工程',file);await until(workspace,v=>v.document?.project.id===project);await menu('视图','绘制');await c.wait('document.querySelector(".draw-preview-status")?.textContent.includes("第 15 帧")',60000);
 assert.equal((await workspace()).document.draw_compositions[0].document.sources.length,2);await record('AVP 3 native save and reopen retain both editable sources and the shared playhead');
 await shortcut('s');await until(workspace,v=>!v.dirty);await menu('文件','关闭项目');await until(workspace,v=>!v.document);
};
const verifyWindowChrome = async title => {
  await until(async()=>JSON.parse(await probe('inspect')),value=>value.title===title);
  const native=await until(async()=>JSON.parse(await probe('capture',['-FilePath',path.join(out,'native-current.png')])),value=>value.caption_color==='#1E1E1E',5000);
  assert.equal(native.caption_color_source,'screen_pixel');assert.equal(native.caption_color,'#1E1E1E');
  await c.wait(`document.title===${JSON.stringify(title)}`);
  const layout=await c.ev(`(()=>{const status=document.querySelector('.app-footer [aria-label="后台服务状态"]');const footer=status?.closest('.app-footer');return {topBar:!!document.querySelector('.desktop-runtime-bar'),menuTitle:!!document.querySelector('.project-menu-bar .project-title'),status:status?.textContent,bottom:footer?.getBoundingClientRect().bottom,height:innerHeight,background:getComputedStyle(document.body).backgroundColor}})()`);
  assert(!layout.topBar&&!layout.menuTitle);assert(layout.status.includes('后台服务已就绪'));assert(Math.abs(layout.bottom-layout.height)<2);assert.equal(layout.background,'rgb(30, 30, 30)');
  return {native,layout};
};
const importFile = async file => { const count=(await workspace()).document.resources.length;await shortcut('i');await choose('选择工程资源',file);await until(workspace,v=>v.document.resources.length===count+1); };
const select = async id => {await c.wait(`document.querySelector('[data-resource-id="${id}"]')`);await c.ev(`document.querySelector('[data-resource-id="${id}"]').click()`);await c.wait(`document.querySelector('[data-resource-id="${id}"]').getAttribute('aria-pressed')==='true'`);};
const verifyMcp = async runtime => {
  await c.ev(`location.hash='/settings/skills'`);
  await c.wait(`document.querySelector('input[aria-label="MCP 地址"]')?.value===${JSON.stringify(runtime.mcp_url)}`);
  await click('复制地址');await until(async()=>await c.invoke('clipboard_read_text')===runtime.mcp_url,Boolean);
  await click('复制连接配置');await until(async()=>(await c.invoke('clipboard_read_text')).includes('[mcp_servers.vac]'),Boolean);const toml=await c.invoke('clipboard_read_text');
  assert(toml.includes('[mcp_servers.vac]')&&toml.includes(JSON.stringify(runtime.mcp_url)));assert(!/token|Authorization/i.test(toml));
  await pointerClick('[aria-label="连接配置格式"]');await click('mcpServers JSON','.ant-select-item-option-content');
  await c.wait(`document.querySelector('textarea[aria-label="MCP 连接配置"]')?.value.startsWith('{')`);
  await click('复制连接配置');await until(async()=>(await c.invoke('clipboard_read_text')).startsWith('{'),Boolean);const config=JSON.parse(await c.invoke('clipboard_read_text'));assert.equal(config.mcpServers.vac.url,runtime.mcp_url);
  await c.screenshot(path.join(out,checks.length?'mcp-current.png':'mcp.png'));
};
const verifyAgentEdit = async runtime => {
  await probe('resize',['-Width','1600','-Height','1000']);
  const media=path.join(out,'edit-source.mp4'),projectFile=path.join(out,'agent-edit-'+pid+'.avp');
  await run('ffmpeg',['-v','error','-nostdin','-y','-f','lavfi','-i','testsrc2=size=320x180:rate=30:duration=4','-f','lavfi','-i','sine=frequency=440:duration=4','-c:v','libx264','-threads','1','-c:a','aac','-shortest',media]);
  let ws=await api('/api/workspace/new','POST',{path:projectFile,name:'Agent二次修改'});
  const project=ws.document.project.id,sequence=ws.document.sequences[0].id;
  ws=await api(`/api/projects/${project}/resources`,'POST',{path:media});
  const resource=ws.document.resources.find(r=>r.kind==='video');
  const readSequence=()=>api(`/api/projects/${project}/sequences/${sequence}`);
  let timeline=await readSequence();
  timeline=await api(`/api/projects/${project}/sequences/${sequence}/commands`,'POST',{session:timeline.session,expected_revision:timeline.sequence.revision,idempotency_key:'manual-original',label:'保留人工片段',operations:[{type:'clip.insert',track_id:timeline.sequence.tracks[0].id,resource_id:resource.id,at:90,source_in:{value:2,scale:1},source_out:{value:3,scale:1}}]});
  const manualClips=timeline.sequence.tracks.flatMap(t=>t.clips).map(clip=>JSON.parse(JSON.stringify(clip)));
  await c.send('Page.reload');await c.wait(`window.__TAURI__?.core`);
  await c.ev(`location.hash='/script'`);await click('新建脚本');
  await input('textarea#script-body','保留人工片段。使用素材前两秒生成两个片段，随后仅缩短第一个片段，并保留第二段内容。');
  await click('保存脚本');await c.wait(`document.body.innerText.includes('脚本已保存，Agent 可读取当前修订。')`);
  ws=await workspace();const scriptResource=ws.document.resources.find(r=>r.kind==='script');
  let mcp=mcpClient(runtime.mcp_url);await mcp.init();
  const savedScript=await mcp.call('resources.read',{project_id:project,resource_id:scriptResource.id});
  assert(savedScript.script.script.body.includes('仅缩短第一个片段'));
  await record('native script creation/save is readable through live MCP', {script_id:scriptResource.script_id});
  const pack=await mcp.call('skills.get',{id:'vac-visual-summary'});
  await mcp.call('skills.read_resource',{id:pack.id,content_sha256:pack.content_sha256,resource:'vac/tool-contracts.md'});
  const visible=await mcp.call('projects.visible_set.get',{id:project});
  let agentRun=await mcp.call('skill_runs.create',{project_id:project,sequence_id:sequence,flow:'visual_heavy',skill_id:pack.id,execution_mode:'external_mcp',visible_set_sha:visible.sha256,goal:savedScript.script.script.body,budget:{max_steps:64,max_seconds:900,max_frames:24},idempotency_key:'native-agent-edit'});
  const tool=async(name,args={})=>{const response=await mcp.call('skill_runs.tool',{run_id:agentRun.id,request:{name,arguments:args,expected_version:agentRun.version,idempotency_key:'edit-tool-'+crypto.randomUUID()}});agentRun=response.run;return response.result;};
  const items=await tool('media.list');const source=Array.isArray(items)?items[0]:items.media[0];
  await tool('scripts.propose',{body:'Opening\n\nEnding',segments:[{media_id:source.id,start:0,end:1,narration:'Opening'},{media_id:source.id,start:1,end:2,narration:'Ending'}],subtitles:false});
  const finish=async()=>{
    await tool('renders.create');await tool('renders.inspect');
    const inspection=agentRun.inspection;assert(inspection?.frame_ids.length);
    for(const id of inspection.frame_ids)await tool('artifacts.get',{id});
    await tool('renders.review',{plan_version:inspection.plan_version,frame_ids:inspection.frame_ids,verdict:'passed',issues:[]});
    await tool('run.finish');assert.equal(agentRun.state,'succeeded');
    return agentRun.artifacts.find(a=>a.kind==='mp4'&&a.plan_version===agentRun.plan.version);
  };
  const first=await finish();assert(first);
  await record('live MCP produces, inspects and finishes the first real MP4 without approval', {run_id:agentRun.id,sha:first.sha256??first.sha});
  const originalEnding=JSON.parse(JSON.stringify(agentRun.plan.segments[1]));
  const target=await mcp.call('skill_runs.edit',{run_id:agentRun.id,expected_version:agentRun.version,action:'target',arguments:{plan_version:agentRun.plan.version,prompt:'只缩短第一段，第二段保持原样',segment_index:0,artifact_id:first.id,artifact_sha256:first.sha256??first.sha,idempotency_key:'shorten-opening'}});agentRun=target.run;
  const changed=JSON.parse(JSON.stringify(agentRun.plan));changed.segments[0].end=.7;changed.segments[0].narration='Short opening';changed.body='Short opening\n\nEnding';
  const edited=await mcp.call('skill_runs.edit',{run_id:agentRun.id,expected_version:agentRun.version,action:'plan',arguments:{plan:changed}});agentRun=edited.run;
  assert.deepEqual(agentRun.plan.segments[1],originalEnding);
  const second=await finish();assert.notEqual(second.sha256??second.sha,first.sha256??first.sha);
  timeline=await readSequence();
  for(const original of manualClips)assert.deepEqual(timeline.sequence.tracks.flatMap(t=>t.clips).find(c=>c.id===original.id),original);
  await record('targeted edit preserves the ending and manual clips, then renders a new artifact',{first:first.id,second:second.id});
  await api('/api/workspace/save','POST',{});
  const exported=path.join(out,'edited-program.mp4');
  // Read registered artifact bytes through the existing API, never a guessed disk path.
  const response=await fetch(base+`/api/skill-runs/${agentRun.id}/artifacts/${second.id}`);assert(response.ok);await fs.writeFile(exported,Buffer.from(await response.arrayBuffer()));
  const probeResult=JSON.parse(await run('ffprobe',['-v','error','-show_format','-show_streams','-of','json',exported]));assert(Math.abs(Number(probeResult.format.duration)-1.7)<.1);
  await c.wait(`document.querySelector('.program-monitor video')?.readyState>=2`,60000);
  const geometry=await c.ev(`(()=>{const monitor=document.querySelector('.program-monitor').getBoundingClientRect(),bar=document.querySelector('.editor-panels-bar').getBoundingClientRect();return {monitor_bottom:monitor.bottom,toolbar_top:bar.top}})()`);assert(geometry.monitor_bottom<=geometry.toolbar_top+1,'program preview must not overlap timeline tools');
  await c.wait(`document.querySelector('.script-editor-sider')`);await c.screenshot(path.join(out,'agent-edit-desktop.png'));await probe('capture',['-FilePath',path.join(out,'native-agent-edit.png')]);
  await api('/api/workspace/close','POST',{decision:'save',stop_tasks:true});await api('/api/workspace/open','POST',{path:projectFile});
  const reopened=await workspace();assert.equal(reopened.document.project.id,project);assert(reopened.document.scripts.find(x=>x.id===scriptResource.script_id).body.includes('仅缩短第一个片段'));
  await record('saved AVP reopens with the edited timeline and script, exported video is 1.7 seconds');
  await api('/api/workspace/close','POST',{decision:'save',stop_tasks:true});
  await c.invoke('stop_runtime');await until(()=>c.invoke('runtime_status'),v=>v.status==='stopped');
  await c.invoke('restart_runtime');const restarted=await until(()=>c.invoke('runtime_status'),v=>v.status==='ready'||v.status==='fault',60000);assert.equal(restarted.status,'ready',restarted.message);
  assert.equal(restarted.mcp_url,runtime.mcp_url,'backend restart must retain the installed MCP endpoint');
  await record('backend restart retains the installed MCP endpoint',{endpoint:restarted.mcp_url});
  base=restarted.api_base_url;
  await api('/api/workspace/open','POST',{path:projectFile});
  const after=await mcp.call('resources.read',{project_id:project,resource_id:scriptResource.id});assert.equal(after.script.script.body,savedScript.script.script.body);
  assert.equal(mcp.reinitializations,1);
  await record('same client and URL recover a stale session with automatic reinitialization',{reinitializations:mcp.reinitializations});
  const configPath=path.resolve(process.argv[5]),config=JSON.parse(await fs.readFile(configPath));
  const pinned=JSON.parse(await fs.readFile(path.join(config.data_directory,'mcp-endpoint.json')));
  const stablePort=Number(new URL(runtime.mcp_url).port);assert.equal(pinned.port,stablePort);
  await api('/api/workspace/close','POST',{decision:'save',stop_tasks:true});
  await c.invoke('stop_runtime');await until(()=>c.invoke('runtime_status'),v=>v.status==='stopped');
  const holder=createServer(socket=>socket.destroy());
  await new Promise((resolve,reject)=>{holder.once('error',reject);holder.listen(stablePort,'127.0.0.1',resolve)});
  try {
    await c.invoke('restart_runtime');const conflict=await until(()=>c.invoke('runtime_status'),v=>v.status==='fault',60000);
    assert(conflict.message.includes('端口'));assert.equal(conflict.mcp_url,null);
    assert.equal(JSON.parse(await fs.readFile(path.join(config.data_directory,'mcp-endpoint.json'))).port,stablePort);
    await record('occupied fixed port reports failure without switching ports or publishing another endpoint');
  } finally {await new Promise(resolve=>holder.close(resolve));}
  await c.invoke('restart_runtime');const recovered=await until(()=>c.invoke('runtime_status'),v=>v.status==='ready'||v.status==='fault',60000);assert.equal(recovered.status,'ready',recovered.message);assert.equal(recovered.mcp_url,runtime.mcp_url);base=recovered.api_base_url;
  await api('/api/workspace/open','POST',{path:projectFile});
  assert.equal((await mcp.call('resources.read',{project_id:project,resource_id:scriptResource.id})).script.script.body,savedScript.script.script.body);
  await record('releasing the occupied port restores service at the original address');
  assert.equal(c.errors.length,0,'native WebView must have no page exceptions');
  if(launched){
    await api('/api/workspace/close','POST',{decision:'save',stop_tasks:true});
    await c.invoke('confirm_project_exit');await until(async()=>launched.exitCode,code=>code!==null,30000);c.close();
    const root=path.dirname(configPath),stdout=await fs.open(path.join(root,'desktop.out.log'),'a'),stderr=await fs.open(path.join(root,'desktop.err.log'),'a');
    launched=spawn(process.env.VAC_DESKTOP_EXECUTABLE,['--desktop-config',configPath,'--webview-data',path.join(root,'webview'),'--webview-debug-port',String(port)],{windowsHide:true,stdio:['ignore',stdout.fd,stderr.fd]});pid=launched.pid;await stdout.close();await stderr.close();
    c=await attachDesktop();const fresh=await until(()=>c.invoke('runtime_status'),v=>v.status==='ready'||v.status==='fault',60000);assert.equal(fresh.status,'ready',fresh.message);assert.equal(fresh.mcp_url,runtime.mcp_url);base=fresh.api_base_url;
    const visible=JSON.parse(await probe('inspect'));assert(visible.visible&&visible.handle);
    await api('/api/workspace/open','POST',{path:projectFile});
    assert.equal((await mcp.call('resources.read',{project_id:project,resource_id:scriptResource.id})).script.script.body,savedScript.script.script.body);
    assert.equal(mcp.reinitializations,3);
    await record('full desktop exit/relaunch retains the URL and the existing client reconnects automatically',{reinitializations:mcp.reinitializations});
  }
  assert.equal(c.errors.length,0,'native WebView must have no page exceptions');
  report.mode='visible_tauri_live_mcp_script_edit_and_persistent_endpoint';
};
const verifyProjectPlayback = async () => {
 const projectFile=path.resolve(process.env.VAC_VERIFY_PLAYBACK_PROJECT);
 const digest=async()=>createHash('sha256').update(await fs.readFile(projectFile)).digest('hex');
 const before=await digest();assert(!(await workspace()).document,'playback regression requires an isolated empty workspace');
 await c.ev(`(()=>{const original=window.fetch;window.__previewRequests=[];window.__projectConflicts=[];window.fetch=async(...args)=>{const url=String(args[0]),entry={url,start:Date.now()};if(url.includes('/preview'))window.__previewRequests.push(entry);try{const r=await original(...args);entry.status=r.status;entry.elapsed=Date.now()-entry.start;if(r.status===409)window.__projectConflicts.push(entry);return r;}catch(e){entry.error=String(e);throw e;}}})()`);
 const opened=await api('/api/workspace/open','POST',{path:projectFile});
 await c.ev(`location.hash='/editing/import'`);
 const started=Date.now();let state;
 for(let i=0;i<(process.env.VAC_VERIFY_PLAYBACK_FAST==='1'?2100:420);i++){
  state=await c.ev(`(()=>{const v=document.querySelector('.program-video'),b=[...document.querySelectorAll('button')].find(n=>n.textContent.trim()==='播放 / 暂停');return {still:!!document.querySelector('.program-frame'),video:!!v,ready:v?.readyState,time:v?.currentTime,duration:v?.duration,playDisabled:b?.disabled,status:document.querySelector('.program-updating')?.textContent,error:document.querySelector('.program-monitor .ant-alert')?.textContent,requests:window.__previewRequests};})()`);
  await fs.writeFile(path.join(out,'playback-progress.json'),JSON.stringify({elapsed_ms:Date.now()-started,...state},null,2));
  if(state.video&&state.ready>=2&&!state.playDisabled)break;
  if(state.error)throw Error(state.error);
  await sleep(process.env.VAC_VERIFY_PLAYBACK_FAST==='1'?200:1000);
 }
 assert(state.video&&state.ready>=2&&!state.playDisabled,'playable movie never became ready: '+JSON.stringify(state));
 await record('real project reaches a playable movie after current-frame preview',{elapsed_ms:Date.now()-started,...state});
 if(process.env.VAC_VERIFY_PLAYBACK_FAST==='1'){
  assert.equal(await c.ev(`document.querySelector('.program-playback')?.dataset.mode`),'direct');assert.deepEqual(await c.ev('window.__projectConflicts.filter(entry=>entry.url.includes('+JSON.stringify('/api/projects/'+opened.document.project.id+'/')+'))'),[],'initial current-project requests used a stale session');
  assert(!state.requests.some(r=>/\/preview(?:\?|$)/.test(r.url)),'simple playback must not render the whole movie');
  await record('simple project plays source media without requesting a full-movie render');
 }
 await click('播放 / 暂停');await sleep(1500);
 const played=await c.ev(`(()=>{const v=document.querySelector('.program-video');return {time:v.currentTime,paused:v.paused,error:v.error?.message,meter:Number.parseFloat(document.querySelector('.program-vu-bar')?.style.height||'0')}})()`);
 assert(!played.paused&&played.time>state.time+.5,JSON.stringify(played));
 await c.screenshot(path.join(out,'playing.png'));await click('播放 / 暂停');
 if(process.env.VAC_VERIFY_PLAYBACK_FAST==='1'){
  assert(played.meter>2,'decoded audio did not reach the playback meter');await record('decoded source audio reaches the live output meter',{meter:played.meter});
  const seek=async text=>{await c.ev(`document.querySelector('button[aria-label="节目时间码"]').click()`);await input('input[aria-label="节目时间码"]',text);await c.send('Input.dispatchKeyEvent',{type:'keyDown',key:'Enter',code:'Enter',windowsVirtualKeyCode:13});await c.send('Input.dispatchKeyEvent',{type:'keyUp',key:'Enter',code:'Enter',windowsVirtualKeyCode:13});await c.wait(`!document.querySelector('.program-updating')`,20000);};
  await seek(process.env.VAC_VERIFY_PLAYBACK_LONG==='1'?'00:00:00:00':'00:00:57:00');await click('播放 / 暂停');
  const samples=[];for(let i=0;i<(process.env.VAC_VERIFY_PLAYBACK_LONG==='1'?650:65);i++){samples.push(await c.ev(`(()=>{const p=document.querySelector('.program-playback'),v=document.querySelector('.program-video');return {wall:Date.now(),head:Number(p.dataset.playhead),playing:p.dataset.playing,drift:Number(p.dataset.avDrift),source:v?.currentSrc,seeking:v?.seeking,audio:[...p.querySelectorAll('audio')].filter(a=>!a.paused).length};})()`));await sleep(100);if(process.env.VAC_VERIFY_PLAYBACK_LONG==='1'&&samples.at(-1).head>62)break;}
  await fs.writeFile(path.join(out,'cut-samples.json'),JSON.stringify(samples,null,2));
  assert(samples.at(-1).head>62,'playback did not cross both real project cuts');
  if(process.env.VAC_VERIFY_PLAYBACK_LONG==='1')assert(samples.at(-1).head-samples[0].head>=(samples.at(-1).wall-samples[0].wall)/1000*.95,'playback ran slower than the wall clock');
  assert(new Set(samples.map(s=>s.source)).size>=2,'next source was not loaded');
  const steady=samples.filter(s=>s.playing==='true'&&!s.seeking);assert(steady.every(s=>Math.abs(s.drift)<.08),'audio/video drift at cuts');assert(steady.some(s=>s.audio===1),'source audio never played');
  await click('播放 / 暂停');await seek('00:01:15:00');
  assert(Math.abs(await c.ev(`Number(document.querySelector('.program-playback').dataset.playhead)`)-75)<.1);
  await record('preloaded source cuts at 58.27/60 seconds, independent audio and later seek stay aligned',{end:samples.at(-1),max_drift:Math.max(...steady.map(s=>Math.abs(s.drift)))});
 }
 await api('/api/workspace/close','POST',{decision:'discard',stop_tasks:true});
 assert.equal(await digest(),before,'Business AVP must remain unchanged');
 await record('native movie advances while playing and the business AVP remains unchanged',played);
};
const verifySequencePreview = async () => {
 await probe('resize',['-Width','1600','-Height','1000']);
 const initial=await workspace();assert(!initial.document,'preview regression requires its isolated empty workspace');
 let ws=await api('/api/workspace/new','POST',{path:path.join(out,'preview-'+pid+'.avp'),name:'GPU preview regression'});
 const project=ws.document.project.id,sequence=ws.document.sequences[0].id,track=ws.document.sequences[0].tracks[0].id;
 ws=await api('/api/workspace/state','PATCH',{session:ws.id,state:{workspace:'resources'}});await c.ev(`location.hash='/resources'`);
 const source='import React from "react";import {useCurrentFrame} from "remotion";export default function Scene(){const f=useCurrentFrame();return <div style={{position:"absolute",inset:0,background:"#091521"}}><svg width="1920" height="1080"><text x="100" y="220" fill="#dceef8" fontSize="72">GPU PREVIEW</text><text x="100" y="320" fill="#83bbcb" fontSize="36">Frame {f} / 1800</text><rect x={100+(f*2)%1200} y="440" width="400" height="220" rx="36" fill="#ff9d45"/><circle cx="1500" cy="800" r={90+f%60} fill="#25b8ac"/></svg></div>;}';
 const document={schema_version:1,canvas:{width:1920,height:1080,fps:{num:30,den:1}},duration_frames:1800,sources:[{id:'scene',engine:'remotion',source}],nodes:[{id:'scene',kind:'source',source_id:'scene',from:0,duration:1800}]};
 const saved=await api(`/api/projects/${project}/draw-compositions`,'POST',{session:ws.id,expected_revision:ws.revision,idempotency_key:'preview-source',composition:{id:'preview',name:'GPU preview',document}});
 const composition=saved.document.draw_compositions.find(c=>c.id==='preview');
 let view=await api(`/api/projects/${project}/draw-compositions/preview/import`,'POST',{session:ws.id,content_version:composition.content_version,expected_revision:ws.document.sequences[0].revision,idempotency_key:'preview-import',sequence_id:sequence,source_mode:'snapshot',target_track_id:track,at:0});
 const sample=async frame=>{const started=Date.now();const result=await api(`/api/projects/${project}/sequences/${sequence}/preview/frame`,'POST',{session:ws.id,expected_revision:view.sequence.revision,content_version:view.content_version,frame});return {...result,wall_ms:Date.now()-started};};
 const cold=await sample(900),warm=await sample(901),repeat=await sample(901);
 assert(cold.worker_pid>0);assert.equal(warm.worker_pid,cold.worker_pid);assert.equal(repeat.worker_pid,cold.worker_pid);
 assert.equal(cold.diagnostics.backend,'webgl2',cold.diagnostics.fallback_reason);assert.equal(cold.diagnostics.hardware_accelerated,true,cold.diagnostics.renderer);
 assert.equal(warm.diagnostics.host_loads,1);assert.equal(warm.diagnostics.source_compiles,1);assert(repeat.diagnostics.frame_cache_hits>0);
 await record('native backend renders 1080p minute source through one persistent hardware GPU worker',{cold:{wall_ms:cold.wall_ms,...cold.diagnostics},warm:{wall_ms:warm.wall_ms,...warm.diagnostics},repeat:{wall_ms:repeat.wall_ms,...repeat.diagnostics}});
 const clip=view.sequence.tracks.flatMap(t=>t.clips)[0];
 view=await api(`/api/projects/${project}/sequences/${sequence}/commands`,'POST',{session:ws.id,expected_revision:view.sequence.revision,idempotency_key:'preview-position',label:'Move preview layer',operations:[{type:'clip.update',clip_ids:[clip.id],effects:{...clip.effects,x:40}}]});
 const edited=await sample(901);assert.equal(edited.worker_pid,cold.worker_pid);assert.equal(edited.diagnostics.source_compiles,1);assert(edited.diagnostics.source_cache_hits>0);
 await record('native timeline edit reuses unchanged drawing pixels and compilation',{wall_ms:edited.wall_ms,...edited.diagnostics});
 await api('/api/workspace/state','PATCH',{session:ws.id,state:{workspace:'editing',active_sequence:sequence,sequence_states:{[sequence]:{head:901,zoom:60}}}});
 await sleep(2500); // Let the ordinary workspace poll publish the fixture before activating its player.
 await c.ev(`location.hash='/editing/import'`);
 await c.wait(`(()=>{const image=document.querySelector('.program-frame');return ((image?.complete&&image.naturalWidth===1920)||document.querySelector('.program-video')?.readyState>=2)&&(document.querySelector('.program-monitor')?.innerText.includes('GPU 合成')||document.querySelector('.program-monitor')?.innerText.includes('分段预览'))})()`,45000);
 const visible=JSON.parse(await probe('inspect'));assert(visible.visible&&visible.handle);
 const geometry=await c.ev(`(()=>{const image=document.querySelector('.program-frame')??document.querySelector('.program-video'),screen=document.querySelector('.program-monitor .editor-monitor-screen').getBoundingClientRect(),timeline=document.querySelector('.sequence-timeline').getBoundingClientRect(),r=image.getBoundingClientRect(),overlay=document.querySelector('.program-updating')?.getBoundingClientRect();const canvas=document.createElement('canvas');canvas.width=1920;canvas.height=1080;const context=canvas.getContext('2d');context.drawImage(image,0,0,1920,1080);return {screen:{width:screen.width,height:screen.height,bottom:screen.bottom},timeline:{top:timeline.top,height:timeline.height},frame:document.querySelector('.timeline-ruler>.track-heading')?.textContent,imageTopmost:document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)===image,overlayHeight:overlay?.height??0,orangePixel:[...context.getImageData(900,550,1,1).data]}})()`);
 assert(geometry.screen.width>100&&geometry.screen.height>100);assert(geometry.timeline.top>=geometry.screen.bottom-2);
 assert(geometry.imageTopmost&&geometry.overlayHeight<geometry.screen.height/4,'render progress obscures the current frame');assert(geometry.orangePixel[0]>220&&geometry.orangePixel[1]>140&&geometry.orangePixel[2]<120,'native monitor did not receive the expected GPU pixels');
 await c.screenshot(path.join(out,'preview.png'));await probe('capture',['-FilePath',path.join(out,'native-preview.png')]);
 await record('visible WebView monitor shows real GPU pixels without covering the timeline',geometry);
 await c.wait(`(()=>{const v=document.querySelector('.program-video'),b=[...document.querySelectorAll('button')].find(n=>n.textContent.trim()==='播放 / 暂停');return v?.readyState>=2&&!b?.disabled;})()`,90000);
 const movieStart=await c.ev(`document.querySelector('.program-video').currentTime`);
 assert(movieStart>=30&&movieStart<30.1,'segment did not seek to the saved playhead: '+movieStart);
 const segmentTarget=process.env.VAC_VERIFY_PLAYBACK_LONG==='1'?46:33;
 const segmentPlayStart=Date.now();await click('播放 / 暂停');await c.wait(`document.querySelector('.program-video')?.currentTime>${segmentTarget}`,60000);
 const playing=await c.ev(`(()=>{const v=document.querySelector('.program-video');return {time:v.currentTime,paused:v.paused,error:v.error?.message,mode:document.querySelector('.program-playback')?.dataset.mode}})()`);
 playing.wall_ms=Date.now()-segmentPlayStart;await fs.writeFile(path.join(out,'segments-progress.json'),JSON.stringify({start:movieStart,...playing},null,2));assert(playing.time-movieStart>=playing.wall_ms/1000*.9,'segmented playback stalled at boundaries: '+JSON.stringify(playing));assert.equal(playing.mode,'segments');assert(!playing.paused&&playing.time>segmentTarget,JSON.stringify(playing));
 await c.screenshot(path.join(out,'segments-playing.png'));await click('播放 / 暂停');await record('drawing preview plays across fMP4 boundaries without waiting for the full minute',playing);
 await c.ev(`location.hash='/resources'`);await c.wait(`!document.querySelector('.sequence-timeline')?.getClientRects().length`);
 await until(async()=>JSON.parse(await run('C:/Program Files/PowerShell/7/pwsh.exe',['-Command',`[bool](Get-Process -Id ${cold.worker_pid} -ErrorAction SilentlyContinue) | ConvertTo-Json`])),alive=>alive===false,15000);
 assert.equal(await c.ev(`document.querySelectorAll('.program-playback video,.program-playback audio').length`),0,'hidden editor rebuilt owned media');
 await record('leaving native editing releases the owned preview worker and browser');
 await api('/api/workspace/close','POST',{decision:'discard',stop_tasks:true});
 assert.equal(c.errors.length,0,'native WebView page exceptions');
};

const attachDesktop = async () => {
  const deadline=Date.now()+120000;let error;
  while(Date.now()<deadline){
    if(launched?.exitCode!==null&&launched?.exitCode!==undefined)throw Error('Desktop exited before bridge readiness: '+launched.exitCode);
    let candidate;
    try{
      candidate=await CDP.connect(port);if(!report.startup_page){report.startup_page=await candidate.ev(`({url:location.href,title:document.title,text:document.body?.innerText.slice(0,1200),tauri:!!window.__TAURI__,internals:!!window.__TAURI_INTERNALS__})`);await fs.writeFile(path.join(out,'startup-page.json'),JSON.stringify(report.startup_page,null,2));await candidate.screenshot(path.join(out,'startup-page.png'));}await candidate.wait('window.__TAURI__?.core',10000);
      assert.equal(await candidate.ev('location.origin'),'http://tauri.localhost');return candidate;
    }catch(reason){error=String(reason);candidate?.close();report.startup_retries=(report.startup_retries??0)+1;await sleep(250);}
  }
  throw Error('Desktop bridge did not become ready: '+error);
};
try {
  if (pidArg === 'launch') {
    const running=JSON.parse(await run('C:/Program Files/PowerShell/7/pwsh.exe',['-Command',`@(Get-Process -Name video-auto-cut-desktop -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Id) | ConvertTo-Json -Compress`])||'[]');
    assert(![].concat(running??[]).filter(Boolean).length,'An existing VAC desktop is running; isolated verification will not attach to or stop it');
    const executable=process.env.VAC_DESKTOP_EXECUTABLE;assert(executable,'VAC_DESKTOP_EXECUTABLE required');
    const config=path.resolve(process.argv[5]);const root=path.dirname(config);
    const stdout=await fs.open(path.join(root,'desktop.out.log'),'w'),stderr=await fs.open(path.join(root,'desktop.err.log'),'w');
    launched=spawn(executable,['--desktop-config',config,'--webview-data',path.join(root,'webview'),'--webview-debug-port',String(port)],{windowsHide:true,stdio:['ignore',stdout.fd,stderr.fd]});pid=launched.pid;await fs.writeFile(path.join(out,'desktop-process.json'),JSON.stringify({pid,config,executable}));await stdout.close();await stderr.close();
  }
  c=await attachDesktop();
  await c.ev(`(()=>{window.__VAC_TEST_MESSAGES=[];new MutationObserver(()=>{for(const n of document.querySelectorAll('.ant-message-notice-content')){const text=n.textContent;if(!window.__VAC_TEST_MESSAGES.includes(text))window.__VAC_TEST_MESSAGES.push(text)}}).observe(document.body,{subtree:true,childList:true})})()`);
  const status=await until(()=>c.invoke('runtime_status'),v=>v.status==='ready'||v.status==='fault');assert.equal(status.status,'ready',status.message);base=status.api_base_url;report.runtime=status;
  if(process.env.VAC_VERIFY_TIMELINE_REOPEN){
    report.mode='visible_tauri_saved_timeline_reopen';
    const file=path.resolve(process.env.VAC_VERIFY_TIMELINE_REOPEN);assert(file.startsWith(testStorageRoot+path.sep),'Only managed timeline fixtures may be reopened');
    const saved=JSON.parse(await fs.readFile(file,'utf8'));
    await c.wait('document.querySelector(".project-home")');await shortcut('o');await choose('打开 VAC 工程',file);await until(workspace,v=>v.document?.project.id===saved.project.id);
    await verifyTimelineReopen(saved,saved.workspace.active_sequence);
  }else if(process.env.VAC_VERIFY_TIMELINE==='1'){
    report.mode='visible_tauri_timeline_real_go';
    await c.wait('document.querySelector(".project-home")');
    if(await c.ev(`document.body.innerText.includes('跳过引导')`))await click('跳过引导');
    await verifyTimeline(status);
  }else if(process.env.VAC_VERIFY_PLAYBACK_PROJECT){
    report.mode='visible_tauri_real_project_playback';
    await verifyProjectPlayback();
  }else if(process.env.VAC_VERIFY_PREVIEW==='1'){
    report.mode='visible_tauri_gpu_preview_real_go';
    await verifySequencePreview();
  }else if(process.env.VAC_VERIFY_AGENT_EDIT==='1'){
    const visible=JSON.parse(await probe('inspect'));assert(visible.visible&&visible.handle);
    await verifyAgentEdit(status);
  }else{
  originalClipboard=await c.invoke('clipboard_read_text');
  await c.wait(`document.querySelector('.project-home')`);const window=JSON.parse(await probe('inspect'));assert(window.visible&&window.handle);await record('visible home and real desktop bridge',window);await c.screenshot(path.join(out,'home.png'));if(await c.ev(`document.body.innerText.includes('跳过引导')`))await click('跳过引导');
  await record('native title bar matches the window background; ready status is at the bottom on the home page',await verifyWindowChrome('Video Auto Cut'));
  await probe('capture',['-FilePath',path.join(out,'native-title-home.png')]);
  const appVersion=JSON.parse(await fs.readFile(path.resolve('desktop/tauri.conf.json'))).version;
  assert.equal((await c.invoke('update_status')).current_version,appVersion);await menu('关于','版本信息');
  await c.wait(`document.querySelector('.ant-modal-content')?.innerText.includes(${JSON.stringify('Video Auto Cut '+appVersion)})`);
  await pointerClick('.ant-modal .ant-btn-primary');await record('About menu and native runtime report the current application version',{version:appVersion});
  await verifyMcp(status);await record('live MCP address, Codex TOML and JSON copy through native clipboard before opening a project');
  if(process.env.VAC_VERIFY_DRAW==='1'){
    await verifyUnifiedDrawing(status);
  }else{
  if(process.env.VAC_ASR_TEST_MODEL&&process.env.VAC_ASR_TEST_PYTHON){
    await c.ev(`location.hash='/settings/profiles'`);await click('新建预设');
    await c.wait(`document.querySelector('input[pattern]')?.value.startsWith('profile_')`);
    const profileID=await c.ev(`document.querySelector('input[pattern]').value`);
    const baselineProfile=JSON.parse(await fs.readFile(path.resolve('configs/phase6/profile.builtin.example.json')));
    await input('.prep-main input[maxlength="100"]','本地 ASR '+pid);
    await input('.prep-main input[placeholder="本机声音名称，或在上方绑定 TTS 模板"]',baselineProfile.tts_voice);
    await c.ev(`location.hash='/settings/providers'`);await c.wait(`document.querySelector('.transcription-settings')`);
    await c.ev(`(()=>{const n=document.querySelector('.transcription-settings input[type=checkbox]');if(!n.checked)n.click()})()`);
    await click('选择模型目录');await choose('选择本地转录模型目录',process.env.VAC_ASR_TEST_MODEL);
    await c.wait(`document.querySelector('[aria-label="本地模型目录"]')?.value===${JSON.stringify(process.env.VAC_ASR_TEST_MODEL.replaceAll('/','\\'))}||document.querySelector('[aria-label="本地模型目录"]')?.value===${JSON.stringify(process.env.VAC_ASR_TEST_MODEL)}`);
    await click('选择 Python');await choose('选择本地转录 Python',process.env.VAC_ASR_TEST_PYTHON);
    await click('检查本地转录环境');await c.wait(`document.querySelector('.transcription-settings .ant-alert-success')?.innerText.includes('faster-whisper')`,60000);
    await c.ev(`document.querySelector('.transcription-settings').scrollIntoView({block:'center'})`);await sleep(250);
    await c.screenshot(path.join(out,'local-asr-settings.png'));await click('保存新版本');
    await c.wait(`document.body.innerText.includes('已保存 v1')`);
    const savedProfile=await api('/api/processing-profiles/'+profileID+'/revisions/0');assert.equal(savedProfile.profile.revision,1);
    assert.equal(savedProfile.profile.transcription.api_format,'faster_whisper');
    assert.equal(path.resolve(savedProfile.profile.transcription.model_path),path.resolve(process.env.VAC_ASR_TEST_MODEL));
    assert.equal(path.resolve(savedProfile.profile.transcription.python_executable),path.resolve(process.env.VAC_ASR_TEST_PYTHON));
    assert(!savedProfile.profile.transcription.endpoint&&!savedProfile.profile.transcription.token_env);
    await c.ev(`location.reload()`);await c.wait(`document.querySelector('.project-home[aria-busy="false"]')`,60000);
    await c.ev(`location.hash='/settings/profiles'`);await pointerClick('.select-saved-profile .ant-select-selector');
    await click(savedProfile.profile.name+' · v'+savedProfile.profile.revision,'.ant-select-item-option-content');await click('加载版本');
    await c.ev(`location.hash='/settings/providers'`);await c.wait(`document.querySelector('.transcription-settings input[aria-label="本地模型目录"]')?.value===${JSON.stringify(savedProfile.profile.transcription.model_path)}`);
    await record('local ASR native model/Python pickers, real dependency check and immutable profile persistence without an open project',{profile_id:profileID});
  }
  await c.ev(`location.hash='/resources'`);await c.wait(`document.querySelector('.project-home')`);
  await verifyTimeline(status);
  const nameA='工程 A '+pid,nameB='工程 B '+pid;const projectA=path.join(out,nameA+'.avp'), projectB=path.join(out,nameB+'.avp');
  await shortcut('n');await choose('保存 VAC 工程',projectA);const first=await until(workspace,v=>v.document?.project.name===nameA);await c.wait(`document.querySelector('.resource-bin')`);await record('Ctrl+N native save dialog creates independent AVP',{id:first.document.project.id,path:projectA});
  // Initial workspace state is a real project edit; save it before checking
  // the clean caption rather than assuming opening the resource page is clean.
  await shortcut('s');await until(workspace,v=>!v.dirty);await verifyWindowChrome('Video Auto Cut — '+nameA);
  const fixture=path.resolve('workers/node/timeline-cli/fixtures/media/clip_01.mp4');
  const audio=path.join(out,'声音.wav'),image=path.join(out,'图片.tif'),subtitle=path.join(out,'字幕.srt'),document=path.join(out,'文稿.md'),script=path.join(out,'脚本.avs');
  await run('ffmpeg',['-v','error','-nostdin','-y','-f','lavfi','-i','sine=frequency=440:duration=1','-c:a','pcm_s16le',audio]);
  await run('ffmpeg',['-v','error','-nostdin','-y','-i',fixture,'-frames:v','1','-update','1',image]);
  await fs.writeFile(subtitle,'1\n00:00:00,000 --> 00:00:01,000\n字幕预览\n');await fs.writeFile(document,'# 当前工程文稿\n原件保留');
  const avs={format:'avs',schema_version:1,id:'source-script',title:'脚本副本',body:'脚本预览正文',segments:[],bindings:[],tts_enabled:false,subtitles_enabled:false,allow_web_research:false};await fs.writeFile(script,JSON.stringify(avs));const avsOriginal=await fs.readFile(script);
  for(const file of [fixture,audio,image,subtitle,document,script]) await importFile(file);
  let view=await workspace();assert.equal(new Set(view.document.resources.map(r=>r.kind)).size,6);await record('native imports cover six resource types without visibility approval',view.document.resources.map(r=>({kind:r.kind,name:r.name})));
  await record('native title shows the active project and unsaved marker',await verifyWindowChrome('Video Auto Cut — '+nameA+' *'));
  await probe('capture',['-FilePath',path.join(out,'native-title-project-dirty.png')]);
  const resources=view.document.resources;
  for(const resource of resources){await select(resource.id);if(resource.kind==='video'||resource.kind==='audio')await c.wait(`(()=>{const n=document.querySelector('.resource-workspace ${resource.kind}');return n?.readyState>=1&&n.duration>0})()`);if(resource.kind==='image')await c.wait(`document.querySelector('.resource-workspace img')?.naturalWidth>0`);if(['document','subtitle','script'].includes(resource.kind))await c.wait(`document.querySelector('.resource-text-preview')?.textContent.length>0`);}
  await record('six kinds preview in the visible window');
  const video=resources.find(r=>r.kind==='video');await select(video.id);await c.ev(`document.querySelector('.resource-workspace video').currentTime=.2`);await sleep(200);await click('设置入点 I');await until(workspace,v=>Math.abs(v.document.resources.find(r=>r.id===video.id).in-.2)<.03);
  await c.ev(`document.querySelector('.resource-workspace video').currentTime=.7`);await sleep(200);await click('设置出点 O');await until(workspace,v=>Math.abs(v.document.resources.find(r=>r.id===video.id).out-.7)<.03);
  await c.ev(`document.querySelector('[aria-label="前进一帧"]').click()`);await c.wait(`document.querySelector('.resource-workspace video').currentTime>.7`);
  await input('textarea[aria-label="资源备注"]','工程备注持久化');await shortcut('s');await until(workspace,v=>!v.dirty&&v.document.resources.find(r=>r.id===video.id).notes==='工程备注持久化');let saved=JSON.parse(await fs.readFile(projectA));assert.equal(saved.resources.find(r=>r.id===video.id).notes,'工程备注持久化');assert(!saved.resources.find(r=>r.id===video.id).source.original_path.startsWith(out));await record('transport marks frame step notes and Ctrl+S persist in AVP');await c.screenshot(path.join(out,'resource-preview.png'));
  await record('saving clears the native title dirty marker and retains the bottom service status',await verifyWindowChrome('Video Auto Cut — '+nameA));
  await c.ev(`(()=>{const n=document.querySelector('textarea[aria-label="资源备注"]');n.focus();n.select()})()`);await menu('编辑','剪切');await c.wait(`document.querySelector('textarea[aria-label="资源备注"]').value===''`);assert((await c.invoke('clipboard_read_text'))==='工程备注持久化','Cut did not copy the focused selection');await menu('编辑','粘贴');await c.wait(`document.querySelector('textarea[aria-label="资源备注"]').value==='工程备注持久化'`);await shortcut('s');await until(workspace,v=>!v.dirty);await record('keyboard edit menu cut and paste dispatch to the focused notes field');
  // An imported script is editable inside AVP; saving must leave its AVS intact.
  const importedScript=resources.find(r=>r.kind==='script');await select(importedScript.id);await c.ev(`document.querySelector('[data-resource-id="${importedScript.id}"]').dispatchEvent(new MouseEvent('dblclick',{bubbles:true}))`);await c.wait(`document.querySelector('[data-testid="script-workbench"] #script-title')?.value===${JSON.stringify(avs.title)}`);await input('#script-title','工程内独立脚本');await shortcut('s');await until(workspace,v=>!v.dirty&&v.document.scripts.some(s=>s.title==='工程内独立脚本'));assert.deepEqual(await fs.readFile(script),avsOriginal);await record('script double-click opens workbench; project save preserves source AVS');
  // Return via view menu; file shortcuts continue working from the editing workspace.
  await menu('视图','资源管理');await select(video.id);await input('textarea[aria-label="资源备注"]','取消关闭后仍保留');await menu('文件','关闭项目');await c.wait(`document.body.innerText.includes('离开当前工程')`);await click('取消');assert.equal((await workspace()).document.project.id,first.document.project.id);await shortcut('s');await until(workspace,v=>!v.dirty);await record('unsaved prompt cancel keeps the active project');
  await input('textarea[aria-label="资源备注"]','窗口关闭也可取消');await probe('close');await c.wait(`document.body.innerText.includes('离开当前工程')`);await click('取消');assert(JSON.parse(await probe('inspect')).visible);assert.equal((await workspace()).document.project.id,first.document.project.id);await shortcut('s');await until(workspace,v=>!v.dirty);await record('native window close respects unsaved cancel and keeps the window visible');
  // Queue a real external-Agent task through the visible WebView's HTTP
  // transport. It performs no model/provider call while awaiting a claim.
  const visible=await api(`/api/projects/${first.document.project.id}/visible-set`);
  const taskRequest={project_id:first.document.project.id,flow:'visual_heavy',skill_id:'vac-visual-summary',execution_mode:'external_mcp',visible_set_sha:visible.sha256,goal:'验证工程切换停止待执行任务',idempotency_key:'native-switch-'+pid};
  const queued=await c.ev(`fetch(${JSON.stringify(base+'/api/skill-runs')},{method:'POST',headers:{'Content-Type':'application/json'},body:${JSON.stringify(JSON.stringify(taskRequest))}}).then(async r=>{const v=await r.json();if(!r.ok)throw Error(JSON.stringify(v));return v})`);assert.equal(queued.state,'queued');await until(workspace,v=>v.active_tasks>0);
  // A second native project gets a fresh request generation and empty resources.
  await shortcut('n');await choose('保存 VAC 工程',projectB);await c.wait(`document.body.innerText.includes('将停止当前工程的')`);await decideSave();let second=await until(workspace,v=>v.document?.project.name===nameB);assert.equal(second.document.resources.length,0);assert.equal((await fetch(base+'/api/skill-runs/'+queued.id)).status,404);await record('native project switch prompts and stops queued work before replacing the workspace');
  const other=path.join(out,'仅 B.txt');await fs.writeFile(other,'only-project-B');await importFile(other);await shortcut('s');await until(workspace,v=>!v.dirty);
  const denied=await fetch(base+`/api/projects/${first.document.project.id}/resources/${video.id}/content`);assert.equal(denied.status,404);const stale=await fetch(base+'/api/assets',{headers:{'X-VAC-Project-Session':first.id}});assert.equal(stale.status,409);await c.wait(`!document.querySelector('[data-resource-id="${video.id}"]')`);await record('second project isolates resources direct reads stale requests and player state');
  // The real native single-instance plugin must forward file arguments.
  const executable=process.env.VAC_DESKTOP_EXECUTABLE;assert(executable,'VAC_DESKTOP_EXECUTABLE required');await run(executable,[projectA]);await decideSave();view=await until(workspace,v=>v.document?.project.id===first.document.project.id);assert.equal(view.document.resources.length,6);assert.equal((await api('/api/skill-runs/'+queued.id)).state,'cancelled');await c.wait(`!document.body.innerText.includes('仅 B.txt')`);await record('second process forwards AVP open to the original visible window');
  await shortcut('i');await probe('cancel',['-DialogTitle','选择工程资源']);await sleep(300);assert.equal((await workspace()).document.resources.length,6);await record('native file picker cancellation leaves project unchanged');
  const bound=path.join(out,'待关联.avs');await fs.writeFile(bound,JSON.stringify({...avs,id:'bound-source',body:'{@旧源.mp4}待关联段落',segments:[{index:0,media_id:'old-video',start:0,end:1,narration:'待关联'}],bindings:[{id:'old-video',name:'旧源.mp4',kind:'video',unresolved:false}]}));await shortcut('i');await choose('选择工程资源',bound);await c.wait(`document.body.innerText.includes('关联脚本素材')`);await click('仅导入脚本');view=await until(workspace,v=>v.document.resources.length===7);const pending=view.document.scripts.find(s=>s.bindings.some(b=>b.unresolved));assert(pending);assert.equal(view.document.resources.filter(r=>r.kind==='video').length,1);await record('bound AVS script-only import retains unresolved bindings and imports no foreign media');
  const pendingResource=view.document.resources.find(r=>r.script_id===pending.id);await select(pendingResource.id);await c.ev(`document.querySelector('[data-resource-id="${pendingResource.id}"]').dispatchEvent(new MouseEvent('dblclick',{bubbles:true}))`);await click('关联素材');await c.wait(`document.querySelector('[aria-label="关联 旧源.mp4"]')`);await pointerClick('[aria-label="关联 旧源.mp4"]');await click('clip_01.mp4','.ant-select-item-option-content');await click('关联并导入');await until(workspace,v=>v.document.scripts.find(s=>s.id===pending.id).bindings.every(b=>!b.unresolved));await record('pending script can bind a current project resource in the workbench');
  const exportedScript=path.join(out,'工作台导出 '+pid+'.avs');await c.wait(`document.querySelector('.project-menu-bar')?.getAttribute('aria-busy')!=='true'`);await click('导出 .avs');await choose('导出脚本',exportedScript);await until(async()=>{try{return JSON.parse(await fs.readFile(exportedScript))}catch{return null}},v=>v?.bindings?.length>0);await shortcut('i');await choose('选择工程资源',exportedScript);await c.wait(`document.body.innerText.includes('关联脚本素材')`);await click('取消');await shortcut('s');assert.equal((await workspace()).document.resources.length,7);await record('native AVS export and bound import cancellation preserve project resources');await menu('视图','资源管理');
  await select(video.id);await menu('编辑','移除所选资源');await until(workspace,v=>v.document.resources.find(r=>r.id===video.id)?.referenced&&v.document.resources.find(r=>r.id===video.id)?.removed);await c.wait(`document.querySelector('[data-resource-id="${video.id}"]')&&document.body.innerText.includes('资源引用缺失')`);await click('重新定位');await choose('选择工程资源',fixture);view=await until(workspace,v=>!v.document.resources.find(r=>r.id===video.id).removed&&!v.document.resources.find(r=>r.id===video.id).missing);assert.equal(view.document.scripts.find(s=>s.id===pending.id).segments[0].media_id,video.id);await record('removed referenced media stays as a relinkable placeholder with stable bindings');
  const proxyName=`代理预览 ${pid}.mkv`,unsupported=path.join(out,proxyName);await run('ffmpeg',['-v','error','-nostdin','-y','-f','lavfi','-i','testsrc2=size=160x90:rate=8:duration=1','-c:v','ffv1',unsupported]);await importFile(unsupported);view=await workspace();const proxyVideo=view.document.resources.find(r=>r.name===proxyName);await select(proxyVideo.id);assert.equal(await c.ev('document.querySelectorAll(".source-monitor video").length'),0);await c.wait(`(()=>{const n=document.querySelector('.resource-workspace video');return n?.src.includes('proxy=1')&&n.readyState>=2&&n.videoWidth>0})()`,60000);await record('unsupported video and TIFF image use real FFmpeg preview proxies without hidden-editor competition');
  await shortcut('s');await until(workspace,v=>!v.dirty);
  const clonePath=path.join(out,'另存 '+pid+'.avp');await shortcut('s',true);await choose('保存 VAC 工程',clonePath);await decideSave();const cloned=await until(workspace,v=>v.path===clonePath);assert.notEqual(cloned.document.project.id,first.document.project.id);await select(video.id);await input('textarea[aria-label="资源备注"]','独立工程中的备注');await shortcut('s');await until(workspace,v=>!v.dirty);saved=JSON.parse(await fs.readFile(projectA));assert.notEqual(saved.resources.find(r=>r.id===video.id).notes,'独立工程中的备注');await record('Ctrl+Shift+S uses native dialog and preserves independent original AVP');
  await shortcut('o');await choose('打开 VAC 工程',projectA);await decideSave();await until(workspace,v=>v.document?.project.id===first.document.project.id);await record('Ctrl+O reopens saved original project');await c.screenshot(path.join(out,'workspace-final.png'));assert.equal(c.errors.length,0,JSON.stringify(c.errors));await record('final visible workspace without page exceptions');
  if(launched){
    await shortcut('s');await until(workspace,v=>!v.dirty);await probe('close');await until(async()=>launched.exitCode,v=>v!==null,45000);c.close();
    const config=path.resolve(process.argv[5]),root=path.dirname(config);const stdout=await fs.open(path.join(root,'desktop.out.log'),'a'),stderr=await fs.open(path.join(root,'desktop.err.log'),'a');
    launched=spawn(executable,[projectA,'--desktop-config',config,'--webview-data',path.join(root,'webview'),'--webview-debug-port',String(port)],{windowsHide:true,stdio:['ignore',stdout.fd,stderr.fd]});pid=launched.pid;await stdout.close();await stderr.close();
    c=await attachDesktop();const restarted=await until(()=>c.invoke('runtime_status'),v=>v.status==='ready'||v.status==='fault');assert.equal(restarted.status,'ready',restarted.message);base=restarted.api_base_url;
    const reopened=await until(workspace,v=>v.document?.project.id===first.document.project.id);assert.equal(reopened.document.resources.find(r=>r.id===video.id).notes,'窗口关闭也可取消');await c.wait(`document.querySelector('.resource-bin')`);await shortcut('s');await until(workspace,v=>!v.dirty);await verifyWindowChrome('Video Auto Cut — '+nameA);await record('native close and fresh executable startup reopen the saved AVP argument with its native title');
    assert.notEqual(restarted.session_id,status.session_id);await verifyMcp(restarted);await record('fresh runtime session supplies the current MCP address and native copied configurations after restart');
  }
  await shortcut('s');await until(workspace,v=>!v.dirty);await menu('文件','关闭项目');await until(workspace,v=>!v.document);await record('project close resets the native title and keeps service status in the home footer',await verifyWindowChrome('Video Auto Cut'));
  await c.invoke('stop_runtime');const stopped=await until(()=>c.invoke('runtime_status'),v=>v.status==='stopped');await c.wait(`document.querySelector('.desktop-runtime-footer [aria-label="后台服务状态"]')?.textContent.includes(${JSON.stringify(stopped.message)})`);const stoppedLayout=await c.ev(`(()=>{const footer=document.querySelector('.desktop-runtime-footer');return {bottom:footer?.getBoundingClientRect().bottom,height:innerHeight,topBar:!!document.querySelector('.desktop-runtime-bar')}})()`);assert(!stoppedLayout.topBar&&Math.abs(stoppedLayout.bottom-stoppedLayout.height)<2);await record('stopped service retains its status in the bottom footer',stoppedLayout);
  await click('重新启动');const resumed=await until(()=>c.invoke('runtime_status'),v=>v.status==='ready'||v.status==='fault');assert.equal(resumed.status,'ready',resumed.message);base=resumed.api_base_url;await until(workspace,v=>!v.document);await record('runtime restart restores the home footer without a top status row',await verifyWindowChrome('Video Auto Cut'));
  }
  }
  report.result=checks.every(x=>x.pass)?'passed':'partial';
  if(report.result==='partial')process.exitCode=1;
} catch(error){report.result='failed';report.error=String(error.stack||error);report.messages=await c?.ev('window.__VAC_TEST_MESSAGES').catch(()=>[]);await c?.screenshot(path.join(out,'failure.png')).catch(()=>{});await fs.writeFile(path.join(out,'results.json'),JSON.stringify(report,null,2));if(process.env.VAC_DESKTOP_KEEP_FAILURE==='1'){console.log('Diagnostic hold: '+report.error);await until(async()=>{try{await fs.access(path.join(out,'resume-validation'));return true}catch{return false}},Boolean,180000).catch(()=>{});}throw error;}
finally{
  await fs.writeFile(path.join(out,'results.json'),JSON.stringify(report,null,2));
  if(originalClipboard!==undefined)await c?.invoke('clipboard_write_text',{text:originalClipboard}).catch(()=>{});
  if(launched){
    if(base)await api('/api/workspace/close','POST',{decision:'discard',stop_tasks:true}).catch(()=>{});
    await c?.invoke('confirm_project_exit').catch(()=>{});
    await until(async()=>launched.exitCode,code=>code!==null,15000).catch(()=>{launched.kill();});
  }
  c?.close();
}
