import { effectiveCue, validateDocument } from '../../../../contracts/delivery-variant/v1/style.mjs';
export const ASS_EVENT_FORMAT = 'Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text';
export function parseSrt(raw, durationMs) {
  const content = raw.replace(/^\uFEFF/, '').replace(/\r\n?/g, '\n');
  if (!content.trim()) return [];
  return content.replace(/\n+$/, '').split(/\n\n+/).map((block, index) => {
    const lines = block.split('\n');
    if (lines.shift() !== String(index + 1)) throw new Error('SRT indexes must be consecutive');
    const match = /^(\d{2,}):(\d{2}):(\d{2}),(\d{3}) --> (\d{2,}):(\d{2}):(\d{2}),(\d{3})$/.exec(lines.shift() || '');
    if (!match) throw new Error('invalid SRT timing');
    const time = offset => {
      const h = Number(match[offset]), m = Number(match[offset + 1]), s = Number(match[offset + 2]), ms = Number(match[offset + 3]);
      if (m > 59 || s > 59) throw new Error('invalid SRT time');
      return ((h * 60 + m) * 60 + s) * 1000 + ms;
    };
    const startMs = time(1), endMs = time(5), text = lines.join('\n');
    const start = Math.floor((startMs + 5) / 10), end = Math.floor((endMs + 5) / 10);
    if (!Number.isSafeInteger(endMs) || !text.trim() || /[\u0000-\u0008\u000b-\u001f\u007f]/u.test(text) || startMs < 0 || endMs <= startMs || end <= start || endMs > durationMs + 1) throw new Error('empty, unsafe or out-of-range subtitle');
    return { start, end, text };
  });
}
export function assTime(cs) {
  return `${Math.floor(cs / 360000)}:${String(Math.floor(cs / 6000) % 60).padStart(2, '0')}:${String(Math.floor(cs / 100) % 60).padStart(2, '0')}.${String(cs % 100).padStart(2, '0')}`;
}
export function assText(text) {
  return [...text].map(c => c === '\\' ? '\\\u2060' : c === '{' ? '\\{' : c === '}' ? '\\}' : c === '\n' ? '\\N' : c).join('');
}
// Conservative layout budget, independent of renderer Unicode wrapping support.
// Visible scalars=2, tab=4; combining accents/variation selectors/ZWJ=0.
export function wrapAssText(text, limit) {
  let units = 0, out = '';
  for (const c of text) {
    if (c === '\n') { out += c; units = 0; continue; }
    const cp = c.codePointAt(0);
    const width = cp >= 0x300 && cp <= 0x36f || cp >= 0xfe00 && cp <= 0xfe0f || cp >= 0xe0100 && cp <= 0xe01ef || cp === 0x200d ? 0 : c === '\t' ? 4 : 2;
    if (width && units + width > limit) { out += '\n'; units = 0; }
    out += c; units += width;
  }
  return out;
}
export function serializeAss(cues, spec) {
  const s = spec;
  const header = `[Script Info]\nScriptType: v4.00+\nPlayResX: ${s.play_res_x}\nPlayResY: ${s.play_res_y}\nWrapStyle: ${s.wrap_style}\nScaledBorderAndShadow: yes\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\nStyle: Default,${s.font_family},${s.font_size},${s.primary_colour},${s.primary_colour},${s.outline_colour},&H80000000,0,0,0,0,100,100,0,0,1,${s.outline},${s.shadow},${s.alignment},${s.margin_l},${s.margin_r},${s.margin_v},1\n\n[Events]\n${ASS_EVENT_FORMAT}\n`;
  const result = header + cues.map(c => {
    const wrapped = wrapAssText(c.text, s.line_wrap_units);
    if (wrapped.split('\n').length > s.max_lines) throw new Error('subtitle exceeds fixed ASS line limit');
    return `Dialogue: 0,${assTime(c.start)},${assTime(c.end)},Default,,0,0,0,,${assText(wrapped)}\n`;
  }).join('');
  if (Buffer.byteLength(result) > 1024 * 1024) throw new Error('ASS exceeds 1 MiB');
  return result;
}

export function serializeStyledAss(cues, document, durationMs) {
  const source = cues.map((c,i) => ({cue_id:i+1,start_ms:c.start*10,end_ms:c.end*10,text:c.text}));
  const errors = validateDocument(document, source, durationMs);
  if (errors.length) throw new Error(errors.join('；'));
  const color = c => `&H00${(c.slice(5,7)+c.slice(3,5)+c.slice(1,3)).toUpperCase()}`;
  const flag = b => b ? -1 : 0;
  const resolved = source.map(c => effectiveCue(c,document));
  let out = `[Script Info]\nScriptType: v4.00+\nPlayResX: ${document.play_res_x}\nPlayResY: ${document.play_res_y}\nWrapStyle: 0\nScaledBorderAndShadow: yes\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n`;
  for (const c of resolved) {
    const s = c.style;
    out += `Style: Cue_${c.cue_id},${s.font_family},${s.font_size},${color(s.primary_color)},${color(s.primary_color)},${color(s.outline_color)},&H80000000,${flag(s.bold)},${flag(s.italic)},${flag(s.underline)},0,100,100,0,0,1,${s.outline},${s.shadow},${s.alignment},${s.margin_l},${s.margin_r},${s.margin_v},1\n`;
  }
  out += `\n[Events]\n${ASS_EVENT_FORMAT}\n`;
  for (const c of resolved) {
    const s = c.style, limit = Math.max(2, Math.floor((document.play_res_x-s.margin_l-s.margin_r-20)/s.font_size)*2);
    const effect = s.fade_in_ms || s.fade_out_ms ? `{\\fad(${s.fade_in_ms},${s.fade_out_ms})}` : '';
    out += `Dialogue: 0,${assTime(Math.floor((c.start_ms+5)/10))},${assTime(Math.floor((c.end_ms+5)/10))},Cue_${c.cue_id},,0,0,0,,${effect}${assText(wrapAssText(c.text,limit))}\n`;
  }
  if (Buffer.byteLength(out)>1024*1024) throw new Error('ASS exceeds 1 MiB');
  return out;
}
