// Copy this module for the real implementation. Every port fails explicitly
// until its media and timebase semantics have been defined and verified.
const pending = (name) => ({
  async run() { throw new Error(`${name} adapter is not implemented`); }
});

export function createAdapters() {
  return {
    cuts: pending('cuts'),
    timebase: pending('timebase'),
    tracks: pending('tracks'),
    sync: pending('sync'),
    formats: pending('formats'),
    audio: pending('audio'),
    subtitles: pending('subtitles')
  };
}
