import { join } from 'node:path';
import { runTool } from '../src/media.mjs';

export const audioAdapter = {
  async run({ stagingDir, timeline }) {
    const audioTracks = (timeline.tracks || []).filter((track) => track.kind === 'audio');
    const artifacts = [];
    for (const track of audioTracks) {
      const filename = `audio_${track.id}.wav`;
      const destination = join(stagingDir, filename);
      const args = ['-hide_banner', '-loglevel', 'error', '-y'];
      const filters = [];
      if (track.items.length === 0) {
        args.push('-f', 'lavfi', '-i', `anullsrc=channel_layout=stereo:sample_rate=${timeline.sample_rate}`);
        filters.push(`[0:a]atrim=end_sample=${timeline.total_samples},asetpts=PTS-STARTPTS[out]`);
      } else {
        const voiceItems = timeline.tracks.find((candidate) => candidate.id === 'a2')?.items || [];
        track.items.forEach((item) => {
          const source = timeline.media[item.src];
          if (!source?.audio) throw new Error(`audio source was not probed: ${item.src}`);
          args.push('-i', source.path);
        });
        const labels = [];
        track.items.forEach((item, index) => {
          const sourceStart = item.in ?? 0;
          const sourceDuration = item.out !== undefined ? item.out - item.in : item.end - item.start;
          const positionSamples = item.sample_in;
          const gain = item.gain_db ?? 0;
          const label = `a${index}`;
          const voiceWindows = item.duck ? voiceItems.map((voice) => ({
            start: Math.max(0, voice.start - item.start),
            end: Math.min(sourceDuration, voice.end - item.start)
          })).filter((window) => window.end > window.start) : [];
          const duck = voiceWindows.length ?
            `,volume='if(${voiceWindows.map((window) =>
              `between(t\\,${window.start}\\,${window.end})`).join('+')}\\,0.2511886432\\,1)':eval=frame` : '';
          filters.push(`[${index}:a]atrim=start=${sourceStart}:duration=${sourceDuration},` +
            `asetpts=PTS-STARTPTS,aresample=${timeline.sample_rate},` +
            `aformat=channel_layouts=stereo,volume=${gain}dB${duck},` +
            `adelay=${positionSamples}S:all=1[${label}]`);
          labels.push(`[${label}]`);
        });
        const mixed = track.items.length === 1 ? labels[0] :
          `${labels.join('')}amix=inputs=${labels.length}:duration=longest:normalize=0[mixed]`;
        if (track.items.length > 1) filters.push(mixed);
        const input = track.items.length === 1 ? labels[0] : '[mixed]';
        filters.push(`${input}apad=whole_len=${timeline.total_samples},` +
          `atrim=end_sample=${timeline.total_samples},asetpts=PTS-STARTPTS[out]`);
      }
      args.push('-filter_complex', filters.join(';'), '-map', '[out]',
        '-ar', String(timeline.sample_rate), '-ac', '2', '-c:a', 'pcm_s16le', destination);
      await runTool('ffmpeg', args);
      artifacts.push({ kind: track.id === 'a1' ? 'game_audio' : track.id === 'a2' ? 'voice' : 'music', path: filename });
    }
    return artifacts;
  }
};
