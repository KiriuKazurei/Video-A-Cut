import { lstat, readdir, readFile, realpath } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { createHash } from 'node:crypto';

const compare = (a, b) => Buffer.compare(Buffer.from(a), Buffer.from(b));
const validFamily = value => typeof value === 'string' && value.trim() && Buffer.byteLength(value) <= 128 && !/[,\x00-\x1f\x7f\\{}]/u.test(value);

export function systemFontDirectories(platform = process.platform, env = process.env) {
  if (platform === 'win32') return [
    join(env.WINDIR || env.SYSTEMROOT || 'C:\\Windows', 'Fonts'),
    ...(env.LOCALAPPDATA ? [join(env.LOCALAPPDATA, 'Microsoft', 'Windows', 'Fonts')] : [])
  ];
  if (platform === 'darwin') return ['/System/Library/Fonts', '/Library/Fonts', ...(env.HOME ? [join(env.HOME, 'Library', 'Fonts')] : [])];
  return ['/usr/share/fonts', '/usr/local/share/fonts', ...(env.HOME ? [join(env.HOME, '.local/share/fonts'), join(env.HOME, '.fonts')] : [])];
}

async function unlinkedPath(path) {
  // Include ancestors: an ordinary Fonts directory can live under a junction.
  for (let cursor = path;; cursor = dirname(cursor)) {
    if ((await lstat(cursor)).isSymbolicLink()) return false;
    if (dirname(cursor) === cursor) break;
  }
  const actual = await realpath(path);
  return process.platform === 'win32' ? actual.toLowerCase() === path.toLowerCase() : actual === path;
}

/** Injectable roots keep catalogue tests local; no fonts are downloaded or installed. */
export function createFontCatalog({ roots = systemFontDirectories(), refreshIntervalMs = 60_000, now = Date.now } = {}) {
  let cached, scannedAt = -Infinity, pending;
  let files = new Map();
  const scan = async () => {
    const fontkit = await import('fontkit');
    const found = [], nextFiles = new Map(), visited = new Set();
    const visit = async directory => {
      const dir = resolve(directory);
      if (visited.has(dir)) return;
      visited.add(dir);
      let entries;
      try {
        if (!await unlinkedPath(dir) || !(await lstat(dir)).isDirectory()) return;
        entries = await readdir(dir, { withFileTypes: true });
      } catch { return; } // Missing/unreadable optional font directories are normal.
      entries.sort((a, b) => compare(a.name, b.name));
      for (const entry of entries) {
        if (entry.isSymbolicLink()) continue;
        const path = join(dir, entry.name);
        if (entry.isDirectory()) { await visit(path); continue; }
        if (!entry.isFile() || !/\.(ttf|otf|ttc)$/i.test(entry.name)) continue;
        try {
          const before = await lstat(path);
          if (!before.isFile() || before.isSymbolicLink() || !await unlinkedPath(path)) continue;
          const stamp = `${before.size}:${before.mtimeMs}:${before.ctimeMs}:${before.ino}`;
          let record = files.get(path);
          if (record?.stamp !== stamp) {
            const bytes = await readFile(path);
            const after = await lstat(path);
            if (after.isSymbolicLink() || after.size !== before.size || after.mtimeMs !== before.mtimeMs || after.ctimeMs !== before.ctimeMs) continue;
            const parsed = fontkit.create(bytes), sha256 = createHash('sha256').update(bytes).digest('hex');
            const faces = [];
            for (const face of parsed.fonts || [parsed]) {
              try {
                if (validFamily(face.familyName)) faces.push(Object.freeze({
                  family: face.familyName, sha256, path,
                  subfamily: face.subfamilyName || '', postscript_name: face.postscriptName || ''
                }));
              } catch { /* Other readable faces in a collection remain available. */ }
            }
            record = { stamp, faces };
          }
          nextFiles.set(path, record);
          found.push(...record.faces);
        } catch { /* Damaged, disappearing or unsupported fonts are not selectable. */ }
      }
    };
    for (const root of roots) await visit(root);
    const seen = new Set();
    cached = Object.freeze(found.filter(font => {
      const key = JSON.stringify([font.family, font.path, font.subfamily, font.postscript_name]);
      if (seen.has(key)) return false;
      seen.add(key); return true;
    }).sort((a, b) => compare(a.family, b.family) || compare(a.path, b.path) || compare(a.subfamily, b.subfamily) || compare(a.postscript_name, b.postscript_name)));
    files = nextFiles; scannedAt = now();
    return cached;
  };
  const installedFonts = async (refresh = false) => {
    if (pending) return pending;
    if (cached && !refresh && now() - scannedAt < refreshIntervalMs) return cached;
    pending = scan();
    try { return await pending; } finally { pending = undefined; }
  };
  const resolveFonts = async families => {
    if (!Array.isArray(families) || !families.length || families.some(f => !validFamily(f))) throw new Error('无效的字体列表');
    // Re-scan directories before rendering; never return removed/replaced cached files.
    const fonts = await installedFonts(true);
    return [...new Set(families)].flatMap(family => {
      const matches = fonts.filter(font => font.family === family);
      if (!matches.length) throw new Error(`字体不可用：${family}`);
      return matches;
    });
  };
  return { installedFonts, resolveFonts };
}

const systemCatalog = createFontCatalog();
export const installedFonts = systemCatalog.installedFonts;
export const resolveFonts = systemCatalog.resolveFonts;
