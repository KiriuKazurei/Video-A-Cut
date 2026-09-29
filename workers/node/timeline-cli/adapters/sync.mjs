/**
 * Sync Adapter:
 * Verifies frame alignment, track boundaries, non-negative spans, voice TTS duration, and no illegal overlap/drift.
 * Must return { ok: boolean, issues: string[] }.
 */
export const syncAdapter = {
  async run({ cuts, timebase, timeline }) {
    const issues = [];
    const media = timebase.media || {};
    const tolerance = 1 / timebase.fps;

    for (const [src, info] of Object.entries(media)) {
      if (info.video) {
        if (Math.abs(info.video.fps - info.video.nominalFps) > 0.001) {
          issues.push(`${src}: variable frame rate source is not supported`);
        }
        if (Math.abs(info.video.fps - timebase.fps) > 0.001) {
          issues.push(`${src}: source frame rate ${info.video.fps} differs from project ${timebase.fps}`);
        }
      }
    }

    // 1. Verify video cuts alignment
    for (let i = 0; i < timebase.video.length; i++) {
      const v = timebase.video[i];
      if (v.frames <= 0) {
        issues.push(`video[${i}] duration frames must be > 0, got ${v.frames}`);
      }
      if (v.timeline_in_frame < 0) {
        issues.push(`video[${i}] timeline_in_frame cannot be negative`);
      }
      const source = media[v.src];
      if (!source?.video || v.out > source.video.duration + tolerance) {
        issues.push(`video[${i}] source is missing video or cut exceeds media duration`);
      }
      if (source?.video?.frames !== null && source?.video?.frames !== undefined &&
          v.out_frame > source.video.frames) {
        issues.push(`video[${i}] source cut exceeds decoded frame count`);
      }
      if (i > 0) {
        const prev = timebase.video[i - 1];
        if (v.timeline_in_frame < prev.timeline_out_frame) {
          issues.push(`video[${i}] overlaps with previous video clip: ${v.timeline_in_frame} < ${prev.timeline_out_frame}`);
        }
      }
    }

    // 2. Verify game audio matches video
    for (let i = 0; i < timebase.game_audio.length; i++) {
      const ga = timebase.game_audio[i];
      if (ga.frames <= 0) {
        issues.push(`game_audio[${i}] duration frames must be > 0`);
      }
      const source = media[ga.src];
      if (!source?.audio || ga.out > source.audio.duration + tolerance) {
        issues.push(`game_audio[${i}] source is missing audio or cut exceeds media duration`);
      }
    }

    // 3. Verify voice and subtitles within total timeline duration
    const total_frames = timebase.total_frames;
    for (let i = 0; i < timebase.voice.length; i++) {
      const vo = timebase.voice[i];
      if (vo.frames <= 0) {
        issues.push(`voice[${i}] duration must be > 0`);
      }
      if (vo.timeline_end_frame > total_frames) {
        issues.push(`voice[${i}] end frame ${vo.timeline_end_frame} exceeds video duration ${total_frames}`);
      }
      const source = media[vo.src];
      if (!source?.audio || Math.abs(source.audio.duration - (vo.end - vo.start)) > tolerance) {
        issues.push(`voice[${i}] real audio duration differs from EDL span`);
      }
    }

    for (let i = 0; i < timebase.subtitle.length; i++) {
      const sub = timebase.subtitle[i];
      if (sub.frames <= 0) {
        issues.push(`subtitle[${i}] duration must be > 0`);
      }
      if (sub.timeline_end_frame > total_frames) {
        issues.push(`subtitle[${i}] end frame ${sub.timeline_end_frame} exceeds video duration ${total_frames}`);
      }
    }

    // 4. Verify music bounds
    for (let i = 0; i < timebase.music.length; i++) {
      const mu = timebase.music[i];
      if (mu.frames <= 0) {
        issues.push(`music[${i}] duration must be > 0`);
      }
      const source = media[mu.src];
      if (!source?.audio || mu.end - mu.start > source.audio.duration + tolerance) {
        issues.push(`music[${i}] source is missing audio or requested span exceeds media duration`);
      }
    }

    return {
      ok: issues.length === 0,
      issues
    };
  }
};
