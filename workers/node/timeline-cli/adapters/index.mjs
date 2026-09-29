import { cutsAdapter } from './cuts.mjs';
import { timebaseAdapter } from './timebase.mjs';
import { tracksAdapter } from './tracks.mjs';
import { syncAdapter } from './sync.mjs';
import { formatsAdapter } from './formats.mjs';
import { audioAdapter } from './audio.mjs';
import { subtitlesAdapter } from './subtitles.mjs';

export function createAdapters() {
  return {
    cuts: cutsAdapter,
    timebase: timebaseAdapter,
    tracks: tracksAdapter,
    sync: syncAdapter,
    formats: formatsAdapter,
    audio: audioAdapter,
    subtitles: subtitlesAdapter
  };
}
