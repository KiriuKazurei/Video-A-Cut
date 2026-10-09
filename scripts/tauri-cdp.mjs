import fs from 'node:fs/promises';
export const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
export class CDP {
  pending = new Map(); seq = 0; errors = [];
  static async connect(port) {
    const c = new CDP();
    for (let i = 0; i < 150; i++) {
      try {
        const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
        const target = targets.find(t => t.type === 'page');
        if (target) { c.target = target; break; }
      } catch {}
      await sleep(100);
    }
    if (!c.target) throw Error('Visible desktop/browser target did not start');
    c.ws = new WebSocket(c.target.webSocketDebuggerUrl);
    await new Promise((resolve, reject) => { c.ws.onopen = resolve; c.ws.onerror = reject; });
    c.ws.onmessage = e => {
      const m = JSON.parse(e.data);
      if (m.method === 'Runtime.exceptionThrown') c.errors.push(m.params);
      const p = c.pending.get(m.id);
      if (p) { c.pending.delete(m.id); clearTimeout(p.timer); m.error ? p.reject(Error(m.error.message)) : p.resolve(m.result); }
    };
    await c.send('Runtime.enable'); await c.send('Page.enable');
    return c;
  }
  send(method, params = {}) {
    return new Promise((resolve, reject) => {
      const id = ++this.seq;
      const timer = setTimeout(() => { this.pending.delete(id); reject(Error('CDP timeout: ' + method)); }, 60000);
      this.pending.set(id, { resolve, reject, timer }); this.ws.send(JSON.stringify({ id, method, params }));
    });
  }
  async ev(expression) {
    const r = await this.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
    if (r.exceptionDetails) throw Error(JSON.stringify(r.exceptionDetails)); return r.result.value;
  }
  async wait(expression, timeout = 30000) {
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) { if (await this.ev(`Boolean(${expression})`)) return; await sleep(100); }
    throw Error('Wait failed: ' + expression);
  }
  invoke(name, args = {}) { return this.ev(`window.__TAURI__.core.invoke(${JSON.stringify(name)},${JSON.stringify(args)})`); }
  async screenshot(file) { const shot = await this.send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: false }); await fs.writeFile(file, Buffer.from(shot.data, 'base64')); }
  close() { this.ws.close(); }
}
if (process.argv[1]?.endsWith('tauri-cdp.mjs')) {
  const c = await CDP.connect(Number(process.argv[2]));
  try { console.log(JSON.stringify(await c.ev(process.argv[3] || 'document.body.innerText'))); }
  finally { c.close(); }
}
