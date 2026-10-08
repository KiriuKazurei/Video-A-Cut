import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, rm, symlink } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { createFontCatalog, systemFontDirectories } from '../src/fonts.mjs';

// Minimal metadata-only SFNT fixtures, authored here and read by the real Fontkit.
// They exercise names/collection offsets and are not rendering fonts.
function sfnt(family, subfamily = 'Regular', base = 0, signature = 0x00010000) {
  const names = [[1,family],[2,subfamily],[6,`${family.replaceAll(' ','')}-${subfamily}`]];
  const strings = names.map(([,name]) => Buffer.from(name,'utf16le').swap16());
  const name = Buffer.alloc(6 + 12 * names.length + strings.reduce((sum,b)=>sum+b.length,0));
  name.writeUInt16BE(names.length,2);name.writeUInt16BE(6+12*names.length,4);
  let offset=0;
  names.forEach(([id],index)=>{
    const record=6+index*12;
    [3,1,0x409,id,strings[index].length,offset].forEach((value,i)=>name.writeUInt16BE(value,record+i*2));
    strings[index].copy(name,6+12*names.length+offset);offset+=strings[index].length;
  });
  const bytes=Buffer.alloc(28+name.length);
  bytes.writeUInt32BE(signature,0);bytes.writeUInt16BE(1,4);bytes.writeUInt16BE(16,6);
  bytes.write('name',12,'ascii');bytes.writeUInt32BE(base+28,20);bytes.writeUInt32BE(name.length,24);name.copy(bytes,28);
  return bytes;
}
function collection(faces) {
  const header=Buffer.alloc(12+faces.length*4);header.write('ttcf',0,'ascii');header.writeUInt32BE(0x10000,4);header.writeUInt32BE(faces.length,8);
  let offset=header.length;
  const fonts=faces.map(([family,style],index)=>{header.writeUInt32BE(offset,12+index*4);const bytes=sfnt(family,style,offset);offset+=bytes.length;return bytes;});
  return Buffer.concat([header,...fonts]);
}
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
async function fixture(t) {
  const root=await mkdtemp(join(process.env.TMPDIR||tmpdir(),'vac-fonts-'));
  t.after(()=>rm(root,{recursive:true,force:true}));
  const fonts=join(root,'fonts');await mkdir(fonts);return {root,fonts};
}

test('Fontkit discovers TTF, OTF and every TTC family/face and ignores malformed names/files', async t => {
  const f=await fixture(t), ttf=sfnt('Fixture Sans'), otf=sfnt('示例字体','Regular',0,0x4f54544f);
  const ttc=collection([['Fixture Sans','Bold'],['Fixture Serif','Regular'],['Fixture Serif','Italic']]);
  for(const [path,bytes] of [['sans.TTF',ttf],['cjk.otf',otf],['collection.ttc',ttc],['broken.ttf',Buffer.from('bad')],['unsafe.ttf',sfnt('bad,font')],['ignored.woff',ttf]]) await writeFile(join(f.fonts,path),bytes);
  const catalog=createFontCatalog({roots:[f.fonts,join(f.root,'missing'),f.fonts]});
  const fonts=await catalog.installedFonts();
  assert.equal(fonts.length,5);assert(fonts.some(font=>font.family==='示例字体'&&font.sha256===hash(otf)));
  assert.equal(fonts.filter(font=>font.path.endsWith('.ttc')).length,3);
  const resolved=await catalog.resolveFonts(['Fixture Serif','Fixture Sans','Fixture Sans']);
  assert.equal(resolved.length,4);assert.equal(resolved.filter(font=>font.family==='Fixture Serif').length,2);
  assert(fonts.every(font=>font.path.startsWith(f.fonts)&&/^[a-f0-9]{64}$/.test(font.sha256)));
  assert(Object.isFrozen(fonts));
  await assert.rejects(catalog.resolveFonts(['Unavailable']),/字体不可用：Unavailable/);
  await assert.rejects(catalog.resolveFonts(['bad,font']),/无效/);
});

test('font catalogue refreshes added, replaced and removed files after TTL or explicit refresh', async t => {
  const f=await fixture(t);let clock=0;
  const catalog=createFontCatalog({roots:[f.fonts],now:()=>clock,refreshIntervalMs:100});
  const path=join(f.fonts,'font.ttf');await writeFile(path,sfnt('Before'));
  const first=await catalog.installedFonts();
  await writeFile(path,sfnt('After Replacement'));await writeFile(join(f.fonts,'new.ttf'),sfnt('Added'));
  assert.strictEqual(await catalog.installedFonts(),first);
  clock=100;
  assert.deepEqual((await catalog.installedFonts()).map(font=>font.family),['Added','After Replacement']);
  await rm(path);
  await assert.rejects(catalog.resolveFonts(['After Replacement']),/字体不可用/);
  await rm(join(f.fonts,'new.ttf'));
  assert.deepEqual(await catalog.installedFonts(true),[]);
});

test('font scans skip linked directories, linked roots and linked ancestors', async t => {
  const f=await fixture(t),outside=join(f.root,'outside');await mkdir(outside);await writeFile(join(outside,'font.ttf'),sfnt('Outside'));
  const link=join(f.fonts,'linked');await symlink(outside,link,process.platform==='win32'?'junction':'dir');
  await mkdir(join(outside,'nested'));await writeFile(join(outside,'nested','font.ttf'),sfnt('Nested'));
  assert.deepEqual(await createFontCatalog({roots:[f.fonts,link,join(link,'nested')]}).installedFonts(),[]);
});

test('system font roots include system and per-user locations without changing the environment', () => {
  const env={WINDIR:'system',LOCALAPPDATA:'user',HOME:'home'};
  assert.deepEqual(systemFontDirectories('win32',env),[join('system','Fonts'),join('user','Microsoft','Windows','Fonts')]);
  assert(systemFontDirectories('linux',env).includes(join('home','.local/share/fonts')));
  assert(systemFontDirectories('darwin',env).includes(join('home','Library','Fonts')));
});
