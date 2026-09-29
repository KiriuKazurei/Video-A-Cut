/** Seconds in the input EDL. Adapters must convert to exact rational/frame time internally. */
export interface Edl {
  timeline: { fps: number; sample_rate: number };
  video: Array<{ src: string; in: number; out: number; timeline_in: number }>;
  game_audio: Array<{ src: string; in: number; out: number; timeline_in: number; gain_db?: number }>;
  voice: Array<{ id: string; src: string; start: number; end: number; gain_db?: number }>;
  subtitle: Array<{ id: string; text: string; start: number; end: number }>;
  music: Array<{ src: string; start: number; end: number; gain_db?: number; duck?: boolean }>;
}

export interface Context {
  edl: Edl;
  /** Absolute path to source EDL, if the caller supplied one. Resolve media paths relative to its directory. */
  edlPath?: string;
  /** Temporary package directory. Do not write outside it. */
  stagingDir: string;
  /** Final package directory; use this when serializing absolute media URLs. */
  outputDir: string;
}

export interface Artifact {
  kind: 'xmeml' | 'otio' | 'fcpxml' | 'game_audio' | 'voice' | 'music' | 'srt' | 'ass' | string;
  /** Relative to stagingDir; file must exist when returned. */
  path: string;
}

/** Define concrete types in adapter implementation; keep all temporal values exact after conversion. */
export interface DeliveryAdapters<Cuts = unknown, Timebase = unknown, Timeline = unknown> {
  cuts: { run(context: Context): Promise<Cuts> };
  timebase: { run(context: Context & { cuts: Cuts }): Promise<Timebase> };
  tracks: { run(context: Context & { cuts: Cuts; timebase: Timebase }): Promise<Timeline> };
  sync: { run(context: Context & { cuts: Cuts; timebase: Timebase; timeline: Timeline }): Promise<{ ok: boolean; issues: unknown[] }> };
  formats: { run(context: Context & { timeline: Timeline; verification: { ok: boolean; issues: unknown[] } }): Promise<Artifact[]> };
  audio: { run(context: Context & { timeline: Timeline; verification: { ok: boolean; issues: unknown[] } }): Promise<Artifact[]> };
  subtitles: { run(context: Context & { timeline: Timeline; verification: { ok: boolean; issues: unknown[] } }): Promise<Artifact[]> };
}

/** Adapter modules loaded by CLI export this function. */
export declare function createAdapters(): DeliveryAdapters | Promise<DeliveryAdapters>;
