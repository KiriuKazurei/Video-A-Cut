import { readFile } from 'node:fs/promises';

export class EdlError extends Error {
  constructor(message) {
    super(message);
    this.name = 'EdlError';
  }
}

const object = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);
const finite = (value) => typeof value === 'number' && Number.isFinite(value);
const nonempty = (value) => typeof value === 'string' && value.trim().length > 0;

function requireField(ok, field, expected) {
  if (!ok) throw new EdlError(`${field}: expected ${expected}`);
}

function checkSpan(item, prefix, start, end) {
  requireField(finite(item[start]) && item[start] >= 0, `${prefix}.${start}`, 'a non-negative finite number');
  requireField(finite(item[end]) && item[end] > item[start], `${prefix}.${end}`, `a finite number greater than ${start}`);
}

/** Structural checks only. Frame rounding, cut validity and media probing belong to pipeline ports. */
export function validateEdl(edl) {
  requireField(object(edl), 'edl', 'an object');
  requireField(object(edl.timeline), 'timeline', 'an object');
  requireField(finite(edl.timeline.fps) && edl.timeline.fps > 0, 'timeline.fps', 'a positive finite number');
  requireField(Number.isSafeInteger(edl.timeline.sample_rate) && edl.timeline.sample_rate > 0,
    'timeline.sample_rate', 'a positive integer');

  for (const track of ['video', 'game_audio', 'voice', 'subtitle', 'music']) {
    requireField(Array.isArray(edl[track]), track, 'an array');
  }
  requireField(edl.video.length > 0, 'video', 'at least one clip');

  for (const track of ['video', 'game_audio']) {
    edl[track].forEach((item, index) => {
      const prefix = `${track}[${index}]`;
      requireField(object(item), prefix, 'an object');
      requireField(nonempty(item.src), `${prefix}.src`, 'a non-empty string');
      checkSpan(item, prefix, 'in', 'out');
      requireField(finite(item.timeline_in) && item.timeline_in >= 0,
        `${prefix}.timeline_in`, 'a non-negative finite number');
      if (item.gain_db !== undefined) requireField(finite(item.gain_db), `${prefix}.gain_db`, 'a finite number');
    });
  }
  edl.voice.forEach((item, index) => {
    const prefix = `voice[${index}]`;
    requireField(object(item), prefix, 'an object');
    requireField(nonempty(item.id), `${prefix}.id`, 'a non-empty string');
    requireField(nonempty(item.src), `${prefix}.src`, 'a non-empty string');
    checkSpan(item, prefix, 'start', 'end');
    if (item.gain_db !== undefined) requireField(finite(item.gain_db), `${prefix}.gain_db`, 'a finite number');
  });
  edl.subtitle.forEach((item, index) => {
    const prefix = `subtitle[${index}]`;
    requireField(object(item), prefix, 'an object');
    requireField(nonempty(item.id), `${prefix}.id`, 'a non-empty string');
    requireField(typeof item.text === 'string', `${prefix}.text`, 'a string');
    checkSpan(item, prefix, 'start', 'end');
  });
  edl.music.forEach((item, index) => {
    const prefix = `music[${index}]`;
    requireField(object(item), prefix, 'an object');
    requireField(nonempty(item.src), `${prefix}.src`, 'a non-empty string');
    checkSpan(item, prefix, 'start', 'end');
    if (item.gain_db !== undefined) requireField(finite(item.gain_db), `${prefix}.gain_db`, 'a finite number');
    if (item.duck !== undefined) requireField(typeof item.duck === 'boolean', `${prefix}.duck`, 'a boolean');
  });
  return edl;
}

export async function readEdl(path) {
  let parsed;
  try {
    parsed = JSON.parse(await readFile(path, 'utf8'));
  } catch (error) {
    throw new EdlError(`cannot read EDL ${path}: ${error.message}`);
  }
  return validateEdl(parsed);
}
