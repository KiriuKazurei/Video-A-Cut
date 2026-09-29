export const cutsAdapter = {
  async run({ edl }) {
    const video_cuts = (edl.video || []).map((clip, index) => {
      const duration = clip.out - clip.in;
      const timeline_in = clip.timeline_in;
      const timeline_out = timeline_in + duration;
      return { index, src: clip.src, in: clip.in, out: clip.out, duration, timeline_in, timeline_out };
    }).sort((a, b) => a.timeline_in - b.timeline_in);

    const game_audio_cuts = (edl.game_audio || []).map((clip, index) => {
      const duration = clip.out - clip.in;
      const timeline_in = clip.timeline_in;
      const timeline_out = timeline_in + duration;
      return { index, src: clip.src, in: clip.in, out: clip.out, duration, timeline_in, timeline_out, gain_db: clip.gain_db ?? 0 };
    }).sort((a, b) => a.timeline_in - b.timeline_in);

    const voice_cuts = (edl.voice || []).map((item, index) => ({
      index, id: item.id, src: item.src, start: item.start, end: item.end, duration: item.end - item.start, gain_db: item.gain_db ?? 0
    })).sort((a, b) => a.start - b.start);

    const subtitle_cuts = (edl.subtitle || []).map((item, index) => ({
      index, id: item.id, text: item.text, start: item.start, end: item.end, duration: item.end - item.start
    })).sort((a, b) => a.start - b.start);

    const music_cuts = (edl.music || []).map((item, index) => ({
      index, src: item.src, start: item.start, end: item.end, duration: item.end - item.start, gain_db: item.gain_db ?? 0, duck: item.duck ?? false
    })).sort((a, b) => a.start - b.start);

    const total_duration = video_cuts.length > 0 ? Math.max(...video_cuts.map(c => c.timeline_out)) : 0;

    return { video_cuts, game_audio_cuts, voice_cuts, subtitle_cuts, music_cuts, total_duration };
  }
};
