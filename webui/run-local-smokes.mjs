// Serve an isolated built UI for the existing route-mocked browser checks.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import http from 'node:http';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

assert.ok(process.env.VAC_TEST_OUTPUT && process.env.VAC_DIST, 'Run through TestWorkspace.ps1 with VAC_DIST');
const here = path.dirname(fileURLToPath(import.meta.url));
const dist = path.resolve(process.env.VAC_DIST);
const output = path.join(process.env.VAC_TEST_OUTPUT, 'review-smokes');
fs.mkdirSync(output, { recursive: true });
const asset = { asset_id: '_selfcheck', status: 'ingested', agent_visible: true, human_approved: true, locked: false, allowed_agents: [], artifacts: {} };
const effects = {gain_db:0,fade_in:0,fade_out:0,scale:1,x:0,y:0,crop_left:0,crop_right:0,crop_top:0,crop_bottom:0,transition:0};
const sequence = {id:'seq-review',revision:1,name:'主序列',width:1920,height:1080,fps:{num:30,den:1},sample_rate:48000,configured:true,markers:[],groups:[],scope_group:'original',history_cursor:0,
  tracks:['video','video','video','audio','audio','audio'].map((kind,index)=>({id:`track-${index}`,name:kind==='video'?'视频':'音频',kind,height:64,locked:false,muted:false,hidden:false,solo:false,gain_db:0,clips:index===0||index===3?[{id:`clip-${index}`,resource_id:'review-resource',name:'审查素材',stream_index:index===0?0:1,at:0,duration:180,source_in:{value:0,scale:48000},source_out:{value:288000,scale:48000},speed:1,link_id:'linked',origin:{actor:'human:window'},effects}]:[]}))};
const workspace = { id: 'review-session', path: 'review.avp', dirty: false, revision: 1, active_tasks: 0, recent: [], recoveries: [],
  document: { format: 'avp', schema_version: 2, project: { id: 'review-project', name: '审查回归' }, sequences:[sequence],
    resources: [{ id: 'review-resource', asset_id: '_selfcheck', kind: 'video', name: '审查素材', notes: '', in: 0, out: 6, missing: false, source: { size_bytes: 0 }, metadata: {} }],
    scripts: [], settings: {}, workspace: {workspace:'editing',active_sequence:'seq-review',selected_resource:'review-resource',sequence_states:{'seq-review':{head:45,zoom:60}}} } };
const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://localhost');
  const json = value => { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(value)); };
  if (url.pathname === '/api/events') {
    res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.write(': local fixture\n\n'); return;
  }
  if (url.pathname === '/api/assets') return json([asset]);
  if (url.pathname === '/api/workspace') return json(workspace);
  if (url.pathname === '/api/workspace/state') return json(workspace);
  if (url.pathname === '/api/visible-set') return json({ confirmed: true, sha256: 'review-visible', assets: [asset] });
  if (url.pathname === '/api/assets/_selfcheck') return json(asset);
  if (url.pathname === '/api/projects/review-project/sequences/seq-review') return json({session:workspace.id,project_revision:workspace.revision,sequence,history:[],content_version:'browser-fixture'});
  if (url.pathname === '/api/ingest-roots') return json({ configured: false, roots: [], policy: {}, ingesters: [] });
  if (/^\/api\/workflows\/[^/]+\/previews$/.test(url.pathname)) return json({ previews: [], presets: [], workers: [] });
  if (url.pathname.startsWith('/api/')) {
    if (req.method !== 'GET') { res.writeHead(405); res.end(); return; }
    return json([]);
  }
  const file = path.resolve(dist, '.' + (url.pathname === '/' ? '/index.html' : url.pathname));
  if (!file.startsWith(dist + path.sep)) { res.writeHead(403); res.end(); return; }
  try {
    res.writeHead(200, { 'Content-Type': ({ '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' })[path.extname(file)] ?? 'application/octet-stream' });
    res.end(fs.readFileSync(file));
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
try {
  const scripts = process.argv.slice(2);
  for (const script of scripts.length ? scripts : ['review_shortcuts_smoke.mjs']) {
    assert.ok(['review_shortcuts_smoke.mjs'].includes(script), 'The retired quickstart page is no longer mounted; current startup is covered by Skill tests and native regression');
    const log = fs.createWriteStream(path.join(output, `${script}.log`));
    const child = spawn(process.execPath, [path.join(here, script), base, path.join(output, script)], { cwd: here, windowsHide: true });
    child.stdout.pipe(log, { end: false }); child.stderr.pipe(log, { end: false });
    const code = await new Promise((resolve, reject) => { child.once('error', reject); child.once('close', resolve); });
    await new Promise(resolve => log.end(resolve));
    assert.equal(code, 0, `${script} failed; see ${path.join(output, `${script}.log`)}`);
    console.log(`PASS: ${script}`);
  }
} finally {
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
}
