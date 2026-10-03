import fs from 'node:fs/promises';

const [base, debuggerBase, output, mock] = process.argv.slice(2);
const targets = await (await fetch(debuggerBase + '/json/list', {signal:AbortSignal.timeout(5000)})).json();
const ws = new WebSocket(targets.find(t => t.type === 'page').webSocketDebuggerUrl);
await new Promise((resolve, reject) => { const timer=setTimeout(()=>reject(Error('CDP connection timed out')),8000); ws.onopen=()=>{clearTimeout(timer);resolve()};ws.onerror=e=>{clearTimeout(timer);reject(e)}; });
let seq = 0; const pending = new Map();
ws.onmessage = e => { const r = JSON.parse(e.data), p = pending.get(r.id); if (p) { pending.delete(r.id); r.error ? p.reject(Error(r.error.message)) : p.resolve(r.result); } };
const send = (method, params = {}) => new Promise((resolve, reject) => { const id = ++seq; const timer=setTimeout(()=>{pending.delete(id);reject(Error('CDP timed out: '+method))},10000); pending.set(id, { resolve:v=>{clearTimeout(timer);resolve(v)}, reject:e=>{clearTimeout(timer);reject(e)} }); ws.send(JSON.stringify({ id, method, params })); });
const evaluate = async expression => { const r = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }); if (r.exceptionDetails) throw Error(JSON.stringify(r.exceptionDetails)); return r.result.value; };
const wait = async expression => { for (let i = 0; i < 100; i++) { if (await evaluate(expression)) return; await new Promise(r => setTimeout(r, 100)); } throw Error('DOM wait failed: ' + expression); };
const J = JSON.stringify;
// 页面全部保持挂载、只隐藏：交互只认当前可见的元素，隐藏页里的同名控件不会被点到。
const VIS = `(e=>!!e&&e.getClientRects().length>0&&e.checkVisibility({visibilityProperty:true}))`;
const click = async (text, scope = 'body') => { const expression = `[...document.querySelectorAll(${J(scope)}+' button')].find(b=>b.textContent.includes(${J(text)})&&!b.disabled&&${VIS}(b))`; await wait(`!!(${expression})`); await evaluate(`(${expression}).click()`); };
const clickSel = async (selector) => { const expression = `[...document.querySelectorAll(${J(selector)})].find(${VIS})`; await wait(`!!(${expression})`); await evaluate(`(${expression}).click()`); };
const input = async (selector, value) => { const expression = `[...document.querySelectorAll(${J(selector)})].find(${VIS})`; await wait(`!!(${expression})`); await evaluate(`(()=>{const e=${expression};Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(e,${J(value)});e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));})()`); await new Promise(r => setTimeout(r, 100)); };
// antd Select：在可见且未禁用的选择器上按下鼠标展开，再点下拉层中 data-value 对应的选项。
const pick = async (selector, value) => {
  const select = `[...document.querySelectorAll(${J(selector)})].map(e=>e.classList.contains('ant-select')?e:e.querySelector('.ant-select')).find(e=>e&&${VIS}(e)&&!e.classList.contains('ant-select-disabled'))`;
  await wait(`!!(${select})`);
  await evaluate(`(${select}).querySelector('.ant-select-selector').dispatchEvent(new MouseEvent('mousedown',{bubbles:true,cancelable:true}))`);
  const option = `[...document.querySelectorAll('.ant-select-dropdown:not(.ant-select-dropdown-hidden) [data-value="${String(value)}"]')].find(${VIS})`;
  await wait(`!!(${option})`);
  await evaluate(`(${option}).closest('.ant-select-item-option').click()`);
  await wait(`![...document.querySelectorAll('.ant-select-dropdown:not(.ant-select-dropdown-hidden)')].some(${VIS})`);
};
const vision = '[data-provider-role="vision"]', narrator = '[data-provider-role="narration"]';
// 按 antd Form.Item 的标签文字定位字段：含 Select 的走 pick，其余写入输入框。
const fieldOf = (scope, label) => `[...document.querySelectorAll(${J(scope)}+' .ant-form-item')].find(f=>f.querySelector('.ant-form-item-label label')?.textContent===${J(label)}&&${VIS}(f))`;
const labeled = async (scope, label, value) => {
  await wait(`!!(${fieldOf(scope, label)})`);
  const info = await evaluate(`(()=>{const f=${fieldOf(scope, label)};f.dataset.testField=f.dataset.testField||Math.random().toString(36).slice(2);return {id:f.dataset.testField,select:!!f.querySelector('.ant-select')}})()`);
  if (info.select) await pick(`[data-test-field="${info.id}"]`, value); else await input(`[data-test-field="${info.id}"] input`, value);
};
// 顶部 antd Menu：窄屏时折叠进「…」子菜单的页签先展开再点；切换后断言目标页可见、其它页全部隐藏。
const openPage = async (page) => {
  const item = `[...document.querySelectorAll('.app-nav [data-page="${page}"], .ant-menu-submenu-popup [data-page="${page}"]')].find(${VIS})`;
  if (!await evaluate(`!!(${item})`)) await clickSel('.app-nav .ant-menu-overflow-item-rest .ant-menu-submenu-title');
  await wait(`!!(${item})`); await evaluate(`(${item}).click()`);
  await wait(`location.hash==='#/${page}'&&${VIS}(document.querySelector('[data-page-view="${page}"]'))&&[...document.querySelectorAll('[data-page-view]')].every(v=>v.dataset.pageView==='${page}'||!${VIS}(v))`);
};
// 任何越过视口右边缘的可见元素（被祖先 overflow 裁掉的不算）都判失败。
const PAGE_OVERFLOW = `(()=>{const cw=document.documentElement.clientWidth;const clipped=e=>{for(let p=e.parentElement;p&&p!==document.body;p=p.parentElement){const s=getComputedStyle(p);if(s.overflowX!=='visible'&&p.getBoundingClientRect().right<=cw+1)return true;}return false;};return [...document.querySelectorAll('body *')].filter(e=>{const b=e.getBoundingClientRect();return b.width>0&&b.height>0&&b.right>cw+1&&e.checkVisibility({opacityProperty:true,visibilityProperty:true})&&!clipped(e)}).slice(0,12).map(e=>({tag:e.tagName,class:String(e.className).slice(0,80),right:Math.round(e.getBoundingClientRect().right),text:e.textContent.slice(0,70)}))})()`;
const report = { engineering: 'pending', human_acceptance: 'pending', driver: 'real headless Chromium CDP; visible-element interaction only', actions: [], layout: [] };
try {
  await send('Page.enable'); await send('Runtime.enable'); await send('Page.navigate', { url: base + '/' });
  await clickSel('.asset-list [data-asset-id="phase7_synthetic"]'); await openPage('prepare');
  await click('新建预设', '.preparation-panel');
  await labeled('.preparation-panel', '内容模式', 'configured');
  for (const format of ['openai', 'anthropic', 'gemini']) {
    await labeled(vision, 'API 格式', format);
    await labeled(vision, 'API 基础地址', mock);
    await labeled(vision, '本次测试用 API key', 'browser-test-key');
    await click('发现模型', vision);
    await pick(vision + ' .provider-models', 'test-model');
    await click('测试连接', vision);
    await wait(`document.querySelector(${JSON.stringify(vision)}).textContent.includes('连接测试通过')`);
    report.actions.push(format + ': native model discovery, select and synthetic-image generation test');
  }
  // Editing the model invalidates its previous test, but retains the matching model list.
  await labeled(vision, '模型 ID', 'manual-model');
  if (await evaluate(`document.querySelector(${JSON.stringify(vision)}).textContent.includes('连接测试通过')`)) throw Error('stale connection result retained');
  await pick(vision + ' .provider-models', 'test-model');
  report.actions.push('manual model remains available; model edit invalidates test and preserves list');
  await labeled(vision, '本次测试用 API key', 'invalid-browser-key');
  await click('测试连接', vision);
  await wait(`document.querySelector(${JSON.stringify(vision)}+' [role=alert]')?.textContent.includes('鉴权失败')`);
  if (await evaluate(`document.querySelector(${JSON.stringify(vision)}).innerText.includes('invalid-browser-key')`)) throw Error('credential leaked in error');
  report.actions.push('authentication error is actionable and secret is not echoed');
  // Slow request followed by an edit must not render its late result.
  await labeled(vision, '本次测试用 API key', 'browser-test-key');
  await labeled(vision, '模型 ID', 'slow-model');
  await click('测试连接', vision);
  await labeled(vision, '模型 ID', 'test-model');
  await new Promise(r => setTimeout(r, 900));
  if (await evaluate(`document.querySelector(${JSON.stringify(vision)}).textContent.includes('连接测试通过')`)) throw Error('late result applied after edit');
  report.actions.push('late diagnostics cancelled when configuration changes');
  // Changing an endpoint clears the transient key and model results.
  await labeled(vision, 'API 基础地址', mock + '/changed');
  const cleared = await evaluate(`(()=>{const f=${fieldOf(vision, '本次测试用 API key')};return f.querySelector('input').value===''&&!document.querySelector(${J(vision)}+' .provider-models')})()`);
  if (!cleared) throw Error('credential/model list retained for another endpoint');
  await labeled(vision, 'API 基础地址', mock);
  await labeled(vision, '本次测试用 API key', 'browser-test-key');
  await click('发现模型', vision); await wait(`!!document.querySelector(${J(vision)}+' .provider-models .ant-select')`);
  await click('测试连接', vision); await wait(`document.querySelector(${JSON.stringify(vision)}).textContent.includes('连接测试通过')`);
  await labeled(narrator, 'API 格式', 'anthropic');
  await labeled(narrator, 'API 基础地址', mock);
  await labeled(narrator, '模型 ID', 'test-model');
  await labeled(narrator, '本次测试用 API key', 'browser-test-key');
  await click('测试连接', narrator); await wait(`document.querySelector(${JSON.stringify(narrator)}).textContent.includes('连接测试通过')`);
  for (const width of [375, 520, 900, 1280]) {
    await send('Emulation.setDeviceMetricsOverride', { width, height: 1100, deviceScaleFactor: 1, mobile: false });
    await new Promise(r => setTimeout(r, 100));
    const row = await evaluate(`(()=>{const p=document.querySelector('.preparation-panel'),r=p.getBoundingClientRect(),clientWidth=document.documentElement.clientWidth;return {width:innerWidth,clientWidth,scrollWidth:document.documentElement.scrollWidth,pageOverflow:${PAGE_OVERFLOW},overflow:[...p.querySelectorAll('input,button,.ant-select')].filter(e=>{const b=e.getBoundingClientRect();return b.width>0&&b.height>0&&(b.right>r.right+1||b.left<r.left-1)}).map(e=>e.tagName+'.'+String(e.className).split(' ')[0])}})()`);
    row.pass = row.scrollWidth <= row.clientWidth && !row.overflow.length && !row.pageOverflow.length; report.layout.push(row);
    await evaluate(`document.querySelector(${JSON.stringify(vision)}).scrollIntoView()`);
    const shot = await send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: false });
    await fs.writeFile(`${output}/providers-${width}.png`, Buffer.from(shot.data, 'base64'));
  }
  if (report.layout.some(r => !r.pass)) throw Error('responsive overflow: ' + JSON.stringify(report.layout));
  await click('保存新版本', '.preparation-panel');
  await wait(`document.querySelector('.preparation-panel').textContent.includes('已保存 v1')`);
  const profiles = await (await fetch(base + '/api/processing-profiles?limit=50')).json();
  const profile = profiles.find(p => p.profile_id.startsWith('profile_'));
  if (!profile || profile.vision.api_format !== 'gemini' || profile.narration.api_format !== 'anthropic') throw Error('format not persisted');
  if (JSON.stringify(profiles).includes('browser-test-key')) throw Error('secret persisted with profile');
  const audit = await (await fetch(base + '/api/audit')).text();
  if (audit.includes('browser-test-key')) throw Error('secret written to audit');
  report.actions.push('saved mixed-format profile excludes transient API key; audit excludes key');
  report.profile_id = profile.profile_id;
  report.engineering = 'passed';
  await fs.writeFile(`${output}/browser-report.json`, JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report));
} catch (error) {
  report.engineering = 'failed'; report.error = String(error); report.dom = await evaluate('document.body.innerText');
  await fs.writeFile(`${output}/browser-failure.json`, JSON.stringify(report, null, 2));
  throw error;
} finally {
  await Promise.race([send('Browser.close').catch(() => {}), new Promise(r => setTimeout(r, 1000))]); ws.close();
}
