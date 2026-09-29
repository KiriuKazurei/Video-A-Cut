/**
 * Tracks Adapter:
 * Assembles video, game_audio, voice, music, subtitle into decoupled multi-track intermediate representation (IR).
 */
export const tracksAdapter = {
  async run({ timebase }) {
    const videoTrack = {
      id: 'v1',
      name: 'Video 1',
      kind: 'video',
      items: timebase.video.map(clip => ({
        id: `vid_${clip.index}`,
        src: clip.src,
        in: clip.in,
        out: clip.out,
        in_frame: clip.in_frame,
        out_frame: clip.out_frame,
        frames: clip.frames,
        timeline_in: clip.timeline_in,
        timeline_out: clip.timeline_out,
        timeline_in_frame: clip.timeline_in_frame,
        timeline_out_frame: clip.timeline_out_frame
      }))
    };

    const gameAudioTrack = {
      id: 'a1',
      name: 'Game Audio',
      kind: 'audio',
      items: timebase.game_audio.map(clip => ({
        id: `ga_${clip.index}`,
        src: clip.src,
        in: clip.in,
        out: clip.out,
        in_frame: clip.in_frame,
        out_frame: clip.out_frame,
        frames: clip.frames,
        timeline_in: clip.timeline_in,
        timeline_out: clip.timeline_out,
        timeline_in_frame: clip.timeline_in_frame,
        timeline_out_frame: clip.timeline_out_frame,
        sample_in: clip.sample_in,
        sample_out: clip.sample_out,
        gain_db: clip.gain_db
      }))
    };

    const voiceTrack = {
      id: 'a2',
      name: 'Voice / Narration',
      kind: 'audio',
      items: timebase.voice.map(item => ({
        id: item.id || `vo_${item.index}`,
        src: item.src,
        start: item.start,
        end: item.end,
        start_frame: item.start_frame,
        end_frame: item.end_frame,
        frames: item.frames,
        timeline_start_frame: item.timeline_start_frame,
        timeline_end_frame: item.timeline_end_frame,
        sample_in: item.sample_in,
        sample_out: item.sample_out,
        gain_db: item.gain_db
      }))
    };

    const musicTrack = {
      id: 'a3',
      name: 'BGM / Music',
      kind: 'audio',
      items: timebase.music.map(item => ({
        id: `bgm_${item.index}`,
        src: item.src,
        start: item.start,
        end: item.end,
        start_frame: item.start_frame,
        end_frame: item.end_frame,
        frames: item.frames,
        timeline_start_frame: item.timeline_start_frame,
        timeline_end_frame: item.timeline_end_frame,
        sample_in: item.sample_in,
        sample_out: item.sample_out,
        gain_db: item.gain_db,
        duck: item.duck
      }))
    };

    const subtitleTrack = {
      id: 's1',
      name: 'Subtitles',
      kind: 'subtitle',
      items: timebase.subtitle.map(item => ({
        id: item.id || `sub_${item.index}`,
        text: item.text,
        start: item.start,
        end: item.end,
        start_frame: item.start_frame,
        end_frame: item.end_frame,
        frames: item.frames,
        timeline_start_frame: item.timeline_start_frame,
        timeline_end_frame: item.timeline_end_frame
      }))
    };

    return {
      fps: timebase.fps,
      sample_rate: timebase.sample_rate,
      media: timebase.media,
      total_frames: timebase.total_frames,
      total_samples: timebase.total_samples,
      tracks: [
        videoTrack,
        gameAudioTrack,
        voiceTrack,
        musicTrack,
        subtitleTrack
      ]
    };
  }
};
