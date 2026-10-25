// Generate the human interface index from the existing inventory and TypeScript
// declarations. This is a documentation tool; it never starts VAC or a provider.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';
import { createHash } from 'node:crypto';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(path.join(root, 'webui/package.json'));
const ts = require('typescript');
const inventory = JSON.parse(fs.readFileSync(path.join(root, 'docs/frontend-api-inventory.json'), 'utf8'));
const documentPath = path.join(root, 'docs/接口文档.md');
const begin = '<!-- BEGIN VAC GENERATED INTERFACES -->';
const end = '<!-- END VAC GENERATED INTERFACES -->';
const modelFiles = [
  'webui/src/projectContracts.ts', 'webui/src/skillContracts.ts', 'webui/src/drawContracts.ts',
  'webui/src/scriptWorkbench.ts', 'webui/src/types.ts', 'webui/src/preparation.ts',
  'webui/src/ttsContracts.ts', 'webui/src/ingest.ts', 'webui/src/autoRun.ts',
  'webui/src/previewContracts.ts', 'webui/src/deliveryVariantContracts.ts',
  'webui/src/desktop.ts', 'webui/src/api.ts',
  'webui/src/mcpConnection.ts', 'webui/src/timelineContracts.ts',
];
const sdkFiles = ['webui/src/api.ts', 'webui/src/skillApi.ts', 'webui/src/projectApi.ts', 'webui/src/timelineApi.ts', 'webui/src/drawApi.ts'];
const parsed = new Map();
function source(file) {
  if (!parsed.has(file)) parsed.set(file, ts.createSourceFile(file,
    fs.readFileSync(path.join(root, file), 'utf8'), ts.ScriptTarget.Latest, true, file.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS));
  return parsed.get(file);
}
const compact = value => value.replace(/\s+/g, ' ').trim();
const cell = value => compact(String(value ?? '')).replaceAll('|', '\\|');
const inline = value => {
  const text = cell(value);
  return text.includes('`') ? '`` ' + text + ' ``' : '`' + text + '`';
};
const ref = (file, node) => `../${file}:${source(file).getLineAndCharacterOfPosition(node.getStart()).line + 1}`;
const sdk = new Map();
for (const file of sdkFiles) {
  const unit = source(file);
  function visit(node) {
    if (ts.isVariableDeclaration(node) && node.initializer && ts.isObjectLiteralExpression(node.initializer)) {
      const owner = node.name.getText(unit);
      for (const property of node.initializer.properties) {
        const fn = ts.isPropertyAssignment(property) ? property.initializer : property;
        if (!ts.isArrowFunction(fn) && !ts.isFunctionExpression(fn) && !ts.isMethodDeclaration(fn)) continue;
        const name = property.name?.getText(unit).replaceAll("'", '').replaceAll('"', '');
        if (!name) continue;
        const parameters = fn.parameters.map(parameter => compact(parameter.getText(unit))).join(', ');
        sdk.set(owner + '.' + name, { signature: owner + '.' + name + '(' + parameters + ')', file, node: property });
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(unit);
}

if (process.argv.includes('--refresh-inventory')) {
  const sha = file => createHash('sha256').update(fs.readFileSync(path.join(root, file), 'utf8').replaceAll('\r\n', '\n')).digest('hex');
  inventory.source_hash_normalization = 'lf';
  const patterns = new Set();
  for (const file of inventory.routes_sources) {
    inventory.source_hashes[file] = sha(file);
    for (const match of fs.readFileSync(path.join(root, file), 'utf8').matchAll(/\.Handle(?:Func)?\("([A-Z]+) ([^"]+)"/g)) patterns.add(match[1] + ' ' + match[2]);
  }
  inventory.routes_source_sha256 = sha(inventory.routes_source);
  inventory.registered_route_count = patterns.size;
  inventory.expanded_endpoint_count = inventory.endpoints.length;
  inventory.frontend_status_summary.counts = inventory.endpoints.reduce((counts, entry) => {
    counts[entry.frontend_status] = (counts[entry.frontend_status] || 0) + 1;
    return counts;
  }, {});
  const position = (file, expression) => {
    const text = fs.readFileSync(path.join(root, file), 'utf8');
    const at = text.search(expression);
    if (at < 0) throw new Error('Cannot locate current interface in ' + file + ': ' + expression);
    return text.slice(0, at).split('\n').length;
  };
  for (const entry of inventory.endpoints) {
    const handler = entry.backend_handler === 'HandleFunc'
      ? '.HandleFunc("' + entry.method + ' ' + entry.path + '"'
      : undefined;
    entry.backend_source.line = handler
      ? position(entry.backend_source.file, new RegExp(handler.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))
      : position(entry.backend_source.file, new RegExp('^func\\s+(?:\\([^\\n]+\\)\\s+)?' + entry.backend_handler + '\\s*\\(', 'm'));
    const definitions = [...new Set(String(entry.frontend_function || '').match(/\b[A-Za-z_]\w*\.[A-Za-z_]\w*/g) || [])].map(name => sdk.get(name)).filter(Boolean);
    for (const reference of entry.frontend_wrapper || []) {
      const definition = definitions.find(value => value.file === reference.file);
      if (definition) reference.line = source(definition.file).getLineAndCharacterOfPosition(definition.node.getStart()).line + 1;
    }
    const names = new Set(definitions.map(value => value.signature.split('(')[0]));
    const used = new Set();
    for (const reference of entry.frontend_calls || []) {
      if (!/\.[cm]?[jt]sx?$/.test(reference.file) || !names.size) continue;
      const unit = source(reference.file), calls = [];
      const visit = node => {
        if (ts.isCallExpression(node) && names.has(node.expression.getText(unit))) calls.push(unit.getLineAndCharacterOfPosition(node.getStart()).line + 1);
        ts.forEachChild(node, visit);
      };
      visit(unit);
      const next = calls.filter(line => !used.has(reference.file + ':' + line)).sort((a, b) => Math.abs(a - reference.line) - Math.abs(b - reference.line))[0];
      if (next) { reference.line = next; used.add(reference.file + ':' + next); }
    }
  }
  for (const command of inventory.desktop_commands) command.source.line = position(command.source.file, new RegExp('\\bfn\\s+' + command.command + '\\s*\\('));
  fs.writeFileSync(path.join(root, 'docs/frontend-api-inventory.json'), JSON.stringify(inventory, null, 2) + '\n', 'utf8');
}

const output = [begin, '', '<a id="http"></a>', '## 8. HTTP 完整索引', '',
  '以下逐项保留方法、路径、参数、响应、行为、SDK/页面接线和后端位置。`retired_in_workspace` 的旧路径在当前 AVP 模式返回 410；静态接线不代表人工验收。', ''];
let group;
for (const entry of inventory.endpoints) {
  if (entry.group !== group) {
    group = entry.group;
    output.push('### ' + group, '');
  }
  output.push('#### ' + entry.id + ' · ' + entry.method + ' ' + inline(entry.path), '',
    '- 请求：' + cell(entry.request),
    '- 成功响应：' + cell(entry.success_response),
    '- 行为：' + cell(entry.behavior),
    '- 生命周期/接线：' + cell(entry.lifecycle || entry.frontend_status || 'registered') + '；' + cell(entry.frontend_ui));
  const names = [...new Set(String(entry.frontend_function || '').match(/(?:api|skillApi|projectApi|timelineApi)\.[A-Za-z0-9_]+/g) || [])];
  const definitions = names.map(name => sdk.get(name)).filter(Boolean);
  for (const definition of definitions) {
    output.push('- SDK：' + inline(definition.signature) + `；[源码](${ref(definition.file, definition.node)})`);
  }
  if (!definitions.length && entry.frontend_function) output.push('- 客户端：' + cell(entry.frontend_function));
  const backend = entry.backend_source;
  output.push(`- 后端：[${entry.backend_handler}](../${backend.file}:${backend.line})`, '');
}
output.push('<a id="ipc"></a>', '## 9. 桌面自定义 IPC 索引', '',
  '| command | 参数 | 返回 | 身份/实现 |', '|---|---|---|---|');
for (const command of inventory.desktop_commands) {
  output.push(`| ${inline(command.command)} | ${inline(command.arguments)} | ${inline(command.result)} | ${cell(command.authorization)}；[Rust](../${command.source.file}:${command.source.line}) |`);
}

output.push('', '<a id="models"></a>', '## 10. 前端公开数据类型与字段', '',
  '从当前 TypeScript 声明生成，`?` 表示可选；继承字段见对应父类型。类型别名保留联合/交叉/映射表达式。类型名后注明源码，字段完整性随源码检查。旧 ProjectSave/Governance 等类型供兼容调用，当前工程写入以第 2 节为准。', '');
let modelCount = 0;
for (const file of modelFiles) {
  const unit = source(file);
  const declarations = unit.statements.filter(node =>
    (ts.isInterfaceDeclaration(node) || ts.isTypeAliasDeclaration(node)) &&
    node.modifiers?.some(modifier => modifier.kind === ts.SyntaxKind.ExportKeyword));
  if (!declarations.length) continue;
  output.push('### ' + file, '');
  for (const declaration of declarations) {
    modelCount++;
    const params = declaration.typeParameters?.length ? '<' + declaration.typeParameters.map(p => p.getText(unit)).join(', ') + '>' : '';
    const parents = declaration.heritageClauses?.map(clause => clause.getText(unit)).join(' ') || '';
    output.push('#### ' + declaration.name.text + cell(params), '',
      `[类型源码](${ref(file, declaration)})${parents ? '；' + inline(parents) : ''}`, '');
    if (ts.isTypeAliasDeclaration(declaration)) {
      output.push('```typescript', declaration.getText(unit), '```', '');
      continue;
    }
    output.push('| 字段 | 类型 | 说明 |', '|---|---|---|');
    for (const member of declaration.members) {
      const name = member.name?.getText(unit) || '[index]';
      const optional = member.questionToken ? '?' : '';
      const description = member.jsDoc?.map(doc => typeof doc.comment === 'string' ? doc.comment : '').join(' ') || '';
      const type = member.type?.getText(unit) || member.getText(unit);
      output.push(`| ${inline(name + optional)} | ${inline(type)} | ${cell(description)} |`);
    }
    output.push('');
  }
}
const refresh = fs.readFileSync(path.join(root, 'webui/src/queryRefresh.ts'), 'utf8');
const block = label => refresh.match(new RegExp(`export const ${label} = \\[([\\s\\S]*?)\\] as const`))?.[1] || '';
const strings = value => [...value.matchAll(/'([^']+)'/g)].map(match => match[1]);
output.push('<a id="events"></a>', '## 11. SSE 事件与刷新领域', '',
  '事件实际支持名单：' + strings(block('SUPPORTED_SSE_EVENTS')).map(inline).join('、') + '。', '',
  '刷新查询键前缀：' + strings(block('REFRESH_DOMAINS')).map(inline).join('、') + '。', '',
  '业务对象仍从 REST 重读。项目切换/播放器停止规则见第 5 节；后端白名单见 [sse.go](../control-plane/internal/api/sse.go)，前端失效分派见 [queryRefresh.ts](../webui/src/queryRefresh.ts)。', '', end);
const generated = output.join('\n');
const original = fs.readFileSync(documentPath, 'utf8');
const first = original.indexOf(begin), last = original.indexOf(end);
if (first < 0 || last < first) throw new Error('Missing interface documentation generation markers');
const updated = original.slice(0, first) + generated + original.slice(last + end.length);
if (process.argv.includes('--write')) {
  fs.writeFileSync(documentPath, updated, 'utf8');
} else if (updated.replaceAll('\r\n', '\n') !== original.replaceAll('\r\n', '\n')) {
  throw new Error('接口文档自动区域过期，请运行 node scripts/update-interface-documentation.mjs --write');
}
console.log(JSON.stringify({ status: 'passed', mode: process.argv.includes('--write') ? 'write' : 'check',
  http: inventory.endpoints.length, ipc: inventory.desktop_commands.length, models: modelCount,
  sse_events: strings(block('SUPPORTED_SSE_EVENTS')).length }));
