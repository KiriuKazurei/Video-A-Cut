// Canonical visible Tauri regression: current AVP workspace, real Go and native dialogs.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { CDP, sleep } from './tauri-cdp.mjs';
import { testStorageRoot } from './test-paths.mjs';
const [portArg, pidArg, directoryArg] = process.argv.slice(2);
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
  return c.ev(`(()=>{const r=document.querySelector(${JSON.stringify(selector)}).getBoundingClientRect();return {x:Math.round((r.left+r.width*${fx})*devicePixelRatio),y:Math.round((r.top+r.height*${fy})*devicePixelRatio)}})()`);
};
const nativeClick = async (selector,action='click') => {const point=await nativePoint(selector);await probe(action,['-X',String(point.x),'-Y',String(point.y)]);};
const mcpClient = endpoint => {
  let session,protocol='2025-11-25',id=0;
  const send=async(method,params,notification=false)=>{
    const request={jsonrpc:'2.0',method,params,...(!notification?{id:++id}:{})};
    const response=await fetch(endpoint,{method:'POST',headers:{'Content-Type':'application/json',Accept:'application/json, text/event-stream',...(session?{'Mcp-Session-Id':session,'MCP-Protocol-Version':protocol}:{})},body:JSON.stringify(request)});
    assert(response.ok,`MCP ${method}: ${response.status}`);session=response.headers.get('Mcp-Session-Id')||session;const text=await response.text();if(notification||!text)return;
    const messages=response.headers.get('Content-Type')?.includes('text/event-stream')?text.split('\n').filter(line=>line.startsWith('data:')).map(line=>JSON.parse(line.slice(5))):[JSON.parse(text)];
    const result=messages.find(message=>message.id===request.id);assert(result&&!result.error,JSON.stringify(result));return result.result;
  };
  return {async init(){const result=await send('initialize',{protocolVersion:protocol,capabilities:{},clientInfo:{name:'VAC desktop timeline regression',version:'0.2.1'}});protocol=result.protocolVersion;await send('notifications/initialized',{},true);},async call(name,args){const result=await send('tools/call',{name,arguments:args});assert(!result.isError,JSON.stringify(result));return result.structuredContent??JSON.parse(result.content.find(item=>item.type==='text').text);}};
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
  const track=ws.document.sequences[0].tracks[0].id,to=await nativePoint(`[data-track-id="${track}"] [data-lane]`,.003,.5),from=await nativePoint(`.project-resource-list [data-resource-id="${resource.id}"]`);
  report.native_drag_hit=await c.ev(`(()=>{const n=document.elementFromPoint(${from.x}/devicePixelRatio,${from.y}/devicePixelRatio);const row=document.querySelector('.project-resource-list [data-resource-id="${resource.id}"]');return {tag:n?.tagName,html:n?.outerHTML.slice(0,800),row:row.getBoundingClientRect().toJSON(),scroll:scrollY,viewport:innerHeight}})()`);
  await c.ev(`(()=>{window.__nativeDragEvents=[];for(const type of ['pointerdown','pointerup','dragstart','dragover','drop','dragend'])document.addEventListener(type,e=>{if(window.__nativeDragEvents.length<80)window.__nativeDragEvents.push({type,x:e.clientX,y:e.clientY,tag:e.target.tagName,resource:e.target.closest?.('[data-resource-id]')?.dataset.resourceId,track:e.target.closest?.('[data-track-id]')?.dataset.trackId,types:e.dataTransfer?[...e.dataTransfer.types]:undefined})},true)})()`);
  await probe('drag',['-X',String(from.x),'-Y',String(from.y),'-EndX',String(to.x),'-EndY',String(to.y)]);
  report.native_drag_events=await c.ev('window.__nativeDragEvents');report.native_drag_points={from,to};
  let view=await until(read,v=>v.sequence.tracks.flatMap(t=>t.clips).length===3),clip=view.sequence.tracks[0].clips[0];assert.equal(clip.at,0);assert.deepEqual(view.sequence.tracks.filter(t=>t.kind==='audio').flatMap(t=>t.clips.map(c=>c.stream_index)),[1,2]);assert(view.sequence.tracks.flatMap(t=>t.clips).every(c=>c.link_id===clip.link_id));
  await record('native resource drag creates linked video and each actual audio stream',{sequence,streams:[1,2]});
  const selected=`[data-clip-id="${clip.id}"]`;await nativeClick(selected);const p=await nativePoint(selected,.6,.5),scale=await c.ev('devicePixelRatio');
  await probe('drag',['-X',String(p.x),'-Y',String(p.y),'-EndX',String(p.x+60*scale),'-EndY',String(p.y),'-CancelDrag']);await sleep(500);assert.equal((await read()).sequence.revision,view.sequence.revision);
  await probe('drag',['-X',String(p.x),'-Y',String(p.y),'-EndX',String(p.x+60*scale),'-EndY',String(p.y)]);view=await until(read,v=>v.sequence.tracks[0].clips[0].at===30);clip=view.sequence.tracks[0].clips[0];assert(view.sequence.tracks.flatMap(t=>t.clips).every(c=>c.at===30));
  await record('native drag threshold, Escape cancellation and linked movement preserve authoritative frames');
  await nativeClick(selected);await probe('key',['-Key','M']);await c.wait('document.querySelector("input[aria-label=标记备注]")');await input('input[aria-label="标记备注"]','定位');await pointerClick('.ant-modal-confirm .ant-btn-primary');view=await until(read,v=>v.sequence.markers.length===1);const marker=view.sequence.markers[0];assert.equal(marker.kind,'point');
  await c.wait('!document.querySelector("input[aria-label=标记备注]")');await click('源标记');await input('input[aria-label="标记备注"]','定位');await pointerClick('.ant-modal-confirm .ant-btn-primary');await until(workspace,v=>v.document.resources.find(r=>r.id===resource.id).markers?.some(m=>m.notes==='定位'));await c.wait('!document.querySelector("input[aria-label=标记备注]")');
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
  await click('时间线','.editor-panels-bar .ant-segmented-item-label');await c.wait('document.querySelector(".unified-editor")?.dataset.panel==="timeline"');await c.wait('document.querySelector(".program-monitor video")?.readyState>=2',60000);assert(await c.ev(`!!document.querySelector('[data-clip-id="${agentClip.id}"] .clip-effect-scope')`));await nativeClick(`[data-clip-id="${agentClip.id}"]`);await probe('key',['-Key','SPACE']);await c.wait('document.querySelector(".program-monitor video")?.paused===false');await probe('key',['-Key','SPACE']);await c.wait('document.querySelector(".program-monitor video")?.paused===true');const exported=path.join(out,`sequence-export ${pid}.mp4`);await click('导出当前范围');await choose('导出当前序列轨道范围',exported);await until(async()=>{try{return (await fs.stat(exported)).size>0}catch{return false}},Boolean,60000);const hash=createHash('sha256').update(await fs.readFile(exported)).digest('hex');assert.equal(hash,finished.artifact.sha256);
  await probe('capture',['-FilePath',path.join(out,'native-timeline-wide.png')]);
  await probe('resize',['-Width','1000','-Height','720']);await c.wait('document.querySelector(".compact-monitor-switch")?.offsetParent!==null');
  await sleep(300);const trackLayout=await c.ev(`(()=>{const sc=document.querySelector('.timeline-scroll').getBoundingClientRect(),timeline=document.querySelector('.sequence-timeline').getBoundingClientRect(),footer=document.querySelector('.app-footer').getBoundingClientRect();return {scroll:{top:sc.top,bottom:sc.bottom},timelineBottom:timeline.bottom,footerTop:footer.top,centers:[${JSON.stringify(track)},${JSON.stringify(view.sequence.tracks.find(t=>t.kind==='audio').id)}].map(id=>{const r=document.querySelector('[data-track-id="'+id+'"]').getBoundingClientRect();return r.top+r.height/2})}})()`);assert(trackLayout.timelineBottom<=trackLayout.footerTop+1&&trackLayout.centers.every(y=>y>=trackLayout.scroll.top+52&&y<=trackLayout.scroll.bottom));report.compact_track_layout=trackLayout;
  const layout=await c.ev(`(()=>{const v=document.querySelector('.program-monitor video'),r=v.getBoundingClientRect(),s=v.parentElement.getBoundingClientRect();return {video:{w:r.width,h:r.height},screen:{w:s.width,h:s.height},controls:v.controls,bodyWidth:document.documentElement.scrollWidth,viewport:innerWidth}})()`);assert(layout.controls&&layout.video.h<=layout.screen.h+1&&layout.video.w<=layout.screen.w+1&&layout.bodyWidth<=layout.viewport+1);await c.screenshot(path.join(out,'timeline-compact.png'));await record('program preview and export share a hash; compact monitors retain the complete frame and native controls',{hash,layout});
  await shortcut('s');await until(workspace,v=>!v.dirty);const saved=JSON.parse(await fs.readFile(file));assert.equal(saved.schema_version,2);assert.equal(saved.sequences.length,2);assert(saved.scripts[0].references.some(r=>r.target_id===marker.id));
  await menu('文件','关闭项目');await until(workspace,v=>!v.document);await shortcut('o');await choose('打开 VAC 工程',file);ws=await until(workspace,v=>v.document?.project.id===project);assert.equal(ws.document.sequences.length,2);assert(ws.document.scripts[0].body.includes('新的定位'));await record('multisequence AVP v2 reopens with stable clips, effects and typed script bindings');
  await probe('resize',['-Width','1900','-Height','1200']);await c.wait(`document.querySelector('.project-resource-list [data-resource-id="${resource.id}"]')?.getAttribute('aria-pressed')==='true'`);
  const savedHead=saved.workspace.sequence_states[sequence].head,nominal=Math.round(saved.sequences[0].fps.num/saved.sequences[0].fps.den),seconds=Math.floor(savedHead/nominal),savedLabel=[Math.floor(seconds/3600),Math.floor(seconds/60)%60,seconds%60,savedHead%nominal].map(n=>String(n).padStart(2,'0')).join(':');
  await c.wait('document.querySelector(".program-monitor video")?.readyState>=2',60000);await c.wait(`document.querySelector('.timeline-ruler>.track-heading')?.textContent===${JSON.stringify(savedLabel)}`);await c.wait(`document.title===${JSON.stringify('Video Auto Cut — 多轨工程 ')}+${pid}`);
  await c.send('Page.reload');await c.wait(`document.querySelector('.project-resource-list [data-resource-id="${resource.id}"]')?.getAttribute('aria-pressed')==='true'`);await c.wait(`document.querySelector('.timeline-ruler>.track-heading')?.textContent===${JSON.stringify(savedLabel)}`);await c.wait(`document.title===${JSON.stringify('Video Auto Cut — 多轨工程 ')}+${pid}`);ws=await workspace();assert(!ws.dirty&&ws.document.workspace.selected_resource===resource.id);await record('page reload restores saved selection and nonzero playhead without a spurious dirty layout',{head:savedHead});
  await shortcut('s');await until(workspace,v=>!v.dirty);await menu('文件','关闭项目');await until(workspace,v=>!v.document);await probe('resize',['-Width','1900','-Height','1200']);
};
const verifyWindowChrome = async title => {
  await until(async()=>JSON.parse(await probe('inspect')),value=>value.title===title);
  const native=JSON.parse(await probe('capture',['-FilePath',path.join(out,'native-current.png')]));
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
const attachDesktop = async () => {
  const deadline=Date.now()+120000;let error;
  while(Date.now()<deadline){
    if(launched?.exitCode!==null&&launched?.exitCode!==undefined)throw Error('Desktop exited before bridge readiness: '+launched.exitCode);
    let candidate;
    try{
      candidate=await CDP.connect(port);await candidate.wait('window.__TAURI__?.core',10000);
      assert.equal(await candidate.ev('location.origin'),'http://tauri.localhost');return candidate;
    }catch(reason){error=String(reason);candidate?.close();report.startup_retries=(report.startup_retries??0)+1;await sleep(250);}
  }
  throw Error('Desktop bridge did not become ready: '+error);
};
try {
  if (pidArg === 'launch') {
    const executable=process.env.VAC_DESKTOP_EXECUTABLE;assert(executable,'VAC_DESKTOP_EXECUTABLE required');
    const config=path.resolve(process.argv[5]);const root=path.dirname(config);
    const stdout=await fs.open(path.join(root,'desktop.out.log'),'w'),stderr=await fs.open(path.join(root,'desktop.err.log'),'w');
    launched=spawn(executable,['--desktop-config',config,'--webview-data',path.join(root,'webview'),'--webview-debug-port',String(port)],{windowsHide:true,stdio:['ignore',stdout.fd,stderr.fd]});pid=launched.pid;await fs.writeFile(path.join(out,'desktop-process.json'),JSON.stringify({pid,config,executable}));await stdout.close();await stderr.close();
  }
  c=await attachDesktop();
  await c.ev(`(()=>{window.__VAC_TEST_MESSAGES=[];new MutationObserver(()=>{for(const n of document.querySelectorAll('.ant-message-notice-content')){const text=n.textContent;if(!window.__VAC_TEST_MESSAGES.includes(text))window.__VAC_TEST_MESSAGES.push(text)}}).observe(document.body,{subtree:true,childList:true})})()`);
  const status=await until(()=>c.invoke('runtime_status'),v=>v.status==='ready'||v.status==='fault');assert.equal(status.status,'ready',status.message);base=status.api_base_url;report.runtime=status;
  originalClipboard=await c.invoke('clipboard_read_text');
  await c.wait(`document.querySelector('.project-home')`);const window=JSON.parse(await probe('inspect'));assert(window.visible&&window.handle);await record('visible home and real desktop bridge',window);await c.screenshot(path.join(out,'home.png'));if(await c.ev(`document.body.innerText.includes('跳过引导')`))await click('跳过引导');
  await record('native title bar matches the window background; ready status is at the bottom on the home page',await verifyWindowChrome('Video Auto Cut'));
  await probe('capture',['-FilePath',path.join(out,'native-title-home.png')]);
  const appVersion=JSON.parse(await fs.readFile(path.resolve('desktop/tauri.conf.json'))).version;
  assert.equal((await c.invoke('update_status')).current_version,appVersion);await menu('关于','版本信息');
  await c.wait(`document.querySelector('.ant-modal-content')?.innerText.includes(${JSON.stringify('Video Auto Cut '+appVersion)})`);
  await pointerClick('.ant-modal .ant-btn-primary');await record('About menu and native runtime report the current application version',{version:appVersion});
  await verifyMcp(status);await record('live MCP address, Codex TOML and JSON copy through native clipboard before opening a project');
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
  report.result='passed';
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
