export const defaultStyle = () => ({ font_family: 'Microsoft YaHei', font_size: 36, bold: false, italic: false, underline: false, outline: 2, shadow: 1, primary_color: '#FFFFFF', outline_color: '#000000', alignment: 2, margin_l: 64, margin_r: 64, margin_v: 36, fade_in_ms: 0, fade_out_ms: 0 });
export function canonicalJSON(value) {
  if (Array.isArray(value)) return `[${value.map(canonicalJSON).join(',')}]`;
  if (value && typeof value === 'object') return `{${Object.keys(value).sort().map(k => `${JSON.stringify(k)}:${canonicalJSON(value[k])}`).join(',')}}`;
  return JSON.stringify(value);
}
export function effectiveCue(cue, document) {
  const override = document.overrides.find(o => o.cue_id === cue.cue_id);
  return { ...cue, start_ms: override?.start_ms ?? cue.start_ms, end_ms: override?.end_ms ?? cue.end_ms, style: { ...document.style, ...override?.style } };
}
export function validateDocument(document, cues, duration) {
  const errors = [];
  if (!document || !Array.isArray(document.overrides) || !document.style) return ['样式数据结构无效'];
  if (!/^[0-9a-f]{64}$/.test(document.source_srt_sha256) || document.play_res_y !== 720 || !Number.isInteger(document.play_res_x) || document.play_res_x < 180 || document.play_res_x > 10000) errors.push('字幕来源或画布无效');
  const keys = Object.keys(defaultStyle());
  const validateStyle = s => {
    if (Object.keys(s).some(k => !keys.includes(k)) || typeof s.font_family !== 'string' || !s.font_family.trim() || new TextEncoder().encode(s.font_family).length > 128 || /[,\x00-\x1f\x7f\\{}]/.test(s.font_family)) return false;
    if (!['bold', 'italic', 'underline'].every(k => typeof s[k] === 'boolean')) return false;
    if (!Number.isInteger(s.font_size) || s.font_size < 12 || s.font_size > 120 || !['outline','shadow'].every(k => Number.isFinite(s[k]) && s[k] >= 0 && s[k] <= 10 && s[k] === Math.round(s[k]*100)/100)) return false;
    if (!['primary_color','outline_color'].every(k => /^#[0-9a-f]{6}$/i.test(s[k]))) return false;
    return Number.isInteger(s.alignment) && s.alignment >= 1 && s.alignment <= 9 && ['margin_l','margin_r','margin_v'].every(k => Number.isSafeInteger(s[k]) && s[k] >= 0) && s.margin_l + s.margin_r < document.play_res_x && s.margin_v < document.play_res_y && ['fade_in_ms','fade_out_ms'].every(k => Number.isSafeInteger(s[k]) && s[k] >= 0);
  };
  if (!validateStyle(document.style)) errors.push('全局字体、颜色或位置参数无效');
  const ids = new Set();
  for (const o of document.overrides) {
    if (!Number.isInteger(o.cue_id) || o.cue_id < 1 || o.cue_id > cues.length || ids.has(o.cue_id)) errors.push('字幕编号无效或重复');
    ids.add(o.cue_id);
    for (const key of ['start_ms','end_ms']) if (key in o && (!Number.isSafeInteger(o[key]) || o[key] < 0 || o[key] > duration)) errors.push(`第 ${o.cue_id} 条时间超出成片范围`);
    if (o.style && Object.keys(o.style).some(k => !keys.includes(k) || o.style[k] == null)) errors.push(`第 ${o.cue_id} 条样式字段无效`);
  }
  for (const cue of cues) {
    const c = effectiveCue(cue, document), start = Math.floor((c.start_ms + 5) / 10), end = Math.floor((c.end_ms + 5) / 10);
    if (!validateStyle(c.style) || start < 0 || end <= start || end > Math.floor((duration + 5) / 10) || c.style.fade_in_ms + c.style.fade_out_ms > (end - start) * 10) errors.push(`第 ${cue.cue_id} 条时间、样式或渐入渐出无效`);
  }
  return [...new Set(errors)];
}
