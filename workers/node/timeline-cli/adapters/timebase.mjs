import { probeMedia, sourcePath } from '../src/media.mjs';
import { toUnits } from '../src/time.mjs';

/** Converts independent EDL boundaries to integer frame/sample positions and probes all sources. */
export const timebaseAdapter = {
  async run({ edl, edlPath, cuts }) {
    const fps = edl.timeline.fps;
    const sampleRate = edl.timeline.sample_rate;
    if (!Number.isSafeInteger(fps)) {
      throw new Error('non-integer project fps requires an explicit rational fps schema; decimal approximation is refused');
    }
    const media = {};
    const sources = new Set([
      ...edl.video.map((item) => item.src), ...edl.game_audio.map((item) => item.src),
      ...edl.voice.map((item) => item.src), ...edl.music.map((item) => item.src)
    ]);
    for (const src of sources) media[src] = await probeMedia(sourcePath(edlPath, src));

    const toFrame = (sec) => toUnits(sec, fps);
    const toSample = (sec) => toUnits(sec, sampleRate);

    const video = (cuts.video_cuts || []).map(cut => {
      const in_frame = toFrame(cut.in);
      const out_frame = toFrame(cut.out);
      const frames = out_frame - in_frame;
      const timeline_in_frame = toFrame(cut.timeline_in);
      const timeline_out_frame = timeline_in_frame + frames;
      return {
        ...cut,
        in_frame,
        out_frame,
        frames,
        timeline_in_frame,
        timeline_out_frame,
        timeline_frames: frames
      };
    });

    const game_audio = (cuts.game_audio_cuts || []).map(cut => {
      const in_frame = toFrame(cut.in);
      const out_frame = toFrame(cut.out);
      const frames = out_frame - in_frame;
      const timeline_in_frame = toFrame(cut.timeline_in);
      const timeline_out_frame = timeline_in_frame + frames;
      return {
        ...cut,
        in_frame,
        out_frame,
        frames,
        timeline_in_frame,
        timeline_out_frame,
        timeline_frames: frames,
        sample_in: toSample(cut.timeline_in),
        sample_out: toSample(cut.timeline_out)
      };
    });

    const voice = (cuts.voice_cuts || []).map(cut => {
      const start_frame = toFrame(cut.start);
      const end_frame = toFrame(cut.end);
      const frames = end_frame - start_frame;
      return {
        ...cut,
        start_frame,
        end_frame,
        frames,
        timeline_start_frame: start_frame,
        timeline_end_frame: end_frame,
        timeline_frames: frames,
        sample_in: toSample(cut.start),
        sample_out: toSample(cut.end)
      };
    });

    const subtitle = (cuts.subtitle_cuts || []).map(cut => {
      const start_frame = toFrame(cut.start);
      const end_frame = toFrame(cut.end);
      const frames = end_frame - start_frame;
      return {
        ...cut,
        start_frame,
        end_frame,
        frames,
        timeline_start_frame: start_frame,
        timeline_end_frame: end_frame,
        timeline_frames: frames
      };
    });

    const music = (cuts.music_cuts || []).map(cut => {
      const start_frame = toFrame(cut.start);
      const end_frame = toFrame(cut.end);
      const frames = end_frame - start_frame;
      return {
        ...cut,
        start_frame,
        end_frame,
        frames,
        timeline_start_frame: start_frame,
        timeline_end_frame: end_frame,
        timeline_frames: frames,
        sample_in: toSample(cut.start),
        sample_out: toSample(cut.end)
      };
    });

    const total_frames = video.length > 0 ? Math.max(...video.map(v => v.timeline_out_frame)) : 0;
    const total_samples = toSample(cuts.total_duration || 0);

    return {
      fps,
      sample_rate: sampleRate,
      media,
      video,
      game_audio,
      voice,
      subtitle,
      music,
      total_frames,
      total_samples
    };
  }
};
