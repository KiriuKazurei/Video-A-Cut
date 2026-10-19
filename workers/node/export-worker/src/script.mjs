// Exact script revisions use the same renderer as direct workbench/Skill exports.
import { dirname, join, relative } from 'node:path';
import { renderScript } from '../../timeline-cli/src/script-workbench.mjs';
import { runTool, probeMedia } from '../../timeline-cli/src/media.mjs';

export async function renderFixedScript({ snapshot, edl, edlPath, cfg, outputDir, signal, resolveUnderRoot,
  render = renderScript }) {
  const w = snapshot.workbench;
  if (!snapshot.script_id || snapshot.version !== w.version || w.script.status !== 'finalized') throw new Error('script revision is not finalized');
  const voices = new Map((edl.voice ?? []).map(v => [v.id, v]));
  const segments = [];
  let cursor = 0;
  for (const [index, segment] of w.segments.entries()) {
    const path = await resolveUnderRoot(cfg.deliveryRoot, snapshot.sources[segment.media_id]);
    const row = { ...segment, path, source_ref: snapshot.sources[segment.media_id],
      subtitles_enabled: segment.subtitles_enabled ?? w.subtitles_enabled };
    const voice = voices.get(`vo${index+1}`);
    if (voice) {
      const rel = relative(cfg.deliveryRoot, join(dirname(edlPath), voice.src)).replaceAll('\\', '/');
      row.voice_file = await resolveUnderRoot(cfg.deliveryRoot, rel);
      row.voice_offset = voice.start-cursor;
    }
    segments.push(row);
    cursor += segment.end-segment.start;
  }
  return render({ script: { script_id: snapshot.script_id, version: snapshot.version,
    visible_set_sha: snapshot.visible_set_sha, finalized_by: w.script.finalized_by,
    finalized_at: w.script.finalized_at }, segments, document: w.document, sources: w.sources,
    subtitles_enabled: w.subtitles_enabled, output_dir: outputDir },
    { run: (command, args, options) => runTool(command, args, { ...options, signal }),
      probe: path => probeMedia(path, { signal }) });
}
