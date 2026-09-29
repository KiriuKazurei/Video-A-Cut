import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';

function formatSrtTime(totalSeconds) {
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = Math.floor(totalSeconds % 60);
  const ms = Math.floor((totalSeconds % 1) * 1000);
  return `${String(hours).padStart(2, '0')}:${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')},${String(ms).padStart(3, '0')}`;
}

export const subtitlesAdapter = {
  async run({ stagingDir, timeline }) {
    const subTrack = (timeline.tracks || []).find(t => t.kind === 'subtitle');
    const items = subTrack ? subTrack.items : [];

    let srtContent = '';
    items.forEach((item, index) => {
      const idx = index + 1;
      const startStr = formatSrtTime(item.start);
      const endStr = formatSrtTime(item.end);
      srtContent += `${idx}\n${startStr} --> ${endStr}\n${item.text || ''}\n\n`;
    });

    const relPath = 'subtitles.srt';
    await writeFile(join(stagingDir, relPath), srtContent, 'utf8');

    return [
      { kind: 'subtitles', path: relPath }
    ];
  }
};
