const finite = value => typeof value === 'number' && Number.isFinite(value);
const invalid = message => Object.assign(new Error(message), {code:'audio_interval_mismatch'});
const timestamp = value => {
  if (typeof value === 'string' && /^-?\d+$/.test(value)) return Number(value);
  if (typeof value === 'number') return value;
  throw invalid('invalid audio stream timestamp type');
};

function timeBase(value) {
  if (typeof value !== 'string' || !/^\d+\/\d+$/.test(value)) throw invalid('missing audio stream time base');
  const [num, den] = value.split('/').map(Number);
  if (![num, den].every(n => Number.isSafeInteger(n) && n > 0)) throw invalid('invalid audio stream time base');
  return num / den;
}

// Use the demuxed MP4 stream presentation timestamps, not container duration or
// AAC decoded sample counts (which can include encoder priming and padding).
export function audioIntervalFromStream(stream) {
  if (stream?.start_pts == null || stream?.duration_ts == null) throw invalid('missing audio stream timestamps');
  const start = timestamp(stream.start_pts), duration = timestamp(stream.duration_ts);
  if (!Number.isSafeInteger(start) || !Number.isSafeInteger(duration) || duration <= 0) throw invalid('invalid audio stream timestamps');
  const unit = timeBase(stream.time_base);
  const interval = {start_pts:start, duration_ts:duration, time_base:stream.time_base,
    start_seconds:start * unit, duration_seconds:duration * unit};
  if (!finite(interval.start_seconds) || !finite(interval.duration_seconds)) throw invalid('invalid audio stream interval');
  return interval;
}

export function validateAudioInterval(interval, targetSamples, sampleRate, fps) {
  if (!interval || !Number.isSafeInteger(targetSamples) || targetSamples <= 0 ||
      !Number.isSafeInteger(sampleRate) || sampleRate <= 0 || !finite(fps) || fps <= 0) throw invalid('missing audio interval or target');
  const actual = audioIntervalFromStream(interval);
  if (!finite(interval.start_seconds) || !finite(interval.duration_seconds) ||
      Math.abs(interval.start_seconds - actual.start_seconds) > 1 / sampleRate ||
      Math.abs(interval.duration_seconds - actual.duration_seconds) > 1 / sampleRate) throw invalid('audio interval disagrees with stream timestamps');
  const target = targetSamples / sampleRate;
  const tolerance = Math.max(1024 / sampleRate, 1 / fps) + 1 / sampleRate;
  const end = actual.start_seconds + actual.duration_seconds;
  if (Math.abs(actual.start_seconds) > tolerance || Math.abs(actual.duration_seconds - target) > tolerance || Math.abs(end - target) > tolerance) {
    throw invalid(`audio stream interval ${actual.start_seconds}..${end}s does not cover target 0..${target}s`);
  }
}
