import type { StyleDocument, SourceCue, SubtitleStyle } from './contracts';
export function defaultStyle(): SubtitleStyle;
export function canonicalJSON(value: unknown): string;
export function effectiveCue(cue: SourceCue, document: StyleDocument): SourceCue & {style: SubtitleStyle};
export function validateDocument(document: StyleDocument, cues: SourceCue[], duration: number): string[];
