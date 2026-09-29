import { copyFile, mkdir, writeFile } from 'node:fs/promises';
import { basename, join } from 'node:path';
import { pathToFileURL } from 'node:url';

const escapeXML = (value) => String(value).replaceAll('&', '&amp;').replaceAll('<', '&lt;')
  .replaceAll('>', '&gt;').replaceAll('"', '&quot;').replaceAll("'", '&apos;');
// Premiere's Windows FCP XML importer expects the drive separator escaped in pathurl.
function xmemlFileURL(path) {
  const url = pathToFileURL(path).href;
  return process.platform === 'win32'
    ? url.replace(/^file:\/\/\/(?=[A-Za-z]:)/, 'file://localhost/').replace(/^file:\/\/localhost\/([A-Za-z]):/, 'file://localhost/$1%3a')
    : url;
}
const time = (value, rate) => ({ OTIO_SCHEMA: 'RationalTime.1', value, rate });
const range = (start, duration, rate) => ({
  OTIO_SCHEMA: 'TimeRange.1', start_time: time(start, rate), duration: time(duration, rate)
});
const gap = (duration, rate) => ({
  OTIO_SCHEMA: 'Gap.1', name: 'Gap', source_range: range(0, duration, rate),
  effects: [], markers: [], metadata: {}, enabled: true
});
function clip(name, url, start, duration, rate, availableDuration) {
  return {
    OTIO_SCHEMA: 'Clip.1', name, source_range: range(start, duration, rate),
    media_reference: {
      OTIO_SCHEMA: 'ExternalReference.1', name, target_url: url,
      available_range: range(0, availableDuration, rate), metadata: {}
    },
    effects: [], markers: [], metadata: {}, enabled: true
  };
}
function track(name, kind, entries, totalFrames, rate) {
  const children = [];
  let cursor = 0;
  for (const entry of entries) {
    if (entry.start < cursor) throw new Error(`${name}: overlapping clips cannot be exported`);
    if (entry.start > cursor) children.push(gap(entry.start - cursor, rate));
    children.push(entry.clip);
    cursor = entry.start + entry.duration;
  }
  if (cursor < totalFrames) children.push(gap(totalFrames - cursor, rate));
  return {
    OTIO_SCHEMA: 'Track.1', name, kind, children, source_range: null,
    effects: [], markers: [], metadata: {}, enabled: true
  };
}

function xmlFile(file) {
  const rate = `<rate><timebase>${file.rate}</timebase><ntsc>FALSE</ntsc></rate>`;
  const characteristics = file.kind === 'video'
    ? `<video><duration>${file.duration}</duration><samplecharacteristics>` +
      `<width>${file.width}</width><height>${file.height}</height>` +
      `<anamorphic>FALSE</anamorphic><pixelaspectratio>square</pixelaspectratio>` +
      `<fielddominance>none</fielddominance>${rate}</samplecharacteristics></video>`
    : `<audio><duration>${file.duration}</duration><samplecharacteristics>` +
      `<depth>16</depth><samplerate>${file.sampleRate}</samplerate>` +
      `</samplecharacteristics><channelcount>2</channelcount>` +
      `<audiochannel><sourcechannel>1</sourcechannel></audiochannel>` +
      `<audiochannel><sourcechannel>2</sourcechannel></audiochannel></audio>`;
  return `<file id="${escapeXML(file.id)}"><name>${escapeXML(file.name)}</name>` +
    `${rate}<duration>${file.duration}</duration>` +
    `<pathurl>${escapeXML(file.xmemlURL)}</pathurl><media>${characteristics}</media></file>`;
}

function xmlClip(id, name, start, end, sourceIn, sourceOut, file) {
  const rate = `<rate><timebase>${file.rate}</timebase><ntsc>FALSE</ntsc></rate>`;
  return `<clipitem id="${escapeXML(id)}"><name>${escapeXML(name)}</name>` +
    `<duration>${end - start}</duration>${rate}<enabled>TRUE</enabled>` +
    `<start>${start}</start><end>${end}</end>` +
    `<in>${sourceIn}</in><out>${sourceOut}</out>${xmlFile(file)}` +
    `<sourcetrack><mediatype>${file.kind}</mediatype><trackindex>1</trackindex></sourcetrack>` +
    `</clipitem>`;
}

export const formatsAdapter = {
  async run({ stagingDir, outputDir, timeline }) {
    const video = timeline.tracks.find((item) => item.kind === 'video');
    if (!video?.items.length) throw new Error('timeline has no video clips');
    const rate = timeline.fps;
    const mediaDir = join(stagingDir, 'media');
    await mkdir(mediaDir, { recursive: true });
    const media = new Map();
    const artifacts = [];
    for (const [index, src] of [...new Set(video.items.map((item) => item.src))].entries()) {
      const info = timeline.media[src];
      if (!info?.video) throw new Error(`video source was not probed: ${src}`);
      const filename = `video_${index + 1}_${basename(info.path)}`;
      await copyFile(info.path, join(mediaDir, filename));
      media.set(src, {
        id: `file_video_${index + 1}`, name: filename, kind: 'video',
        url: pathToFileURL(join(outputDir, 'media', filename)).href,
        xmemlURL: xmemlFileURL(join(outputDir, 'media', filename)),
        duration: Math.round(info.video.duration * rate), rate,
        width: info.video.width, height: info.video.height
      });
      artifacts.push({ kind: 'source_video', path: `media/${filename}` });
    }

    const videoEntries = video.items.map((item) => ({
      start: item.timeline_in_frame, duration: item.frames,
      clip: clip(item.id, media.get(item.src).url, item.in_frame, item.frames,
        rate, media.get(item.src).duration)
    }));
    const audioTracks = timeline.tracks.filter((item) => item.kind === 'audio');
    const otioTracks = [track(video.name, 'Video', videoEntries, timeline.total_frames, rate)];
    for (const audio of audioTracks) {
      const filename = `audio_${audio.id}.wav`;
      otioTracks.push(track(audio.name, 'Audio', [{
        start: 0, duration: timeline.total_frames,
        clip: clip(audio.name, pathToFileURL(join(outputDir, filename)).href,
          0, timeline.total_frames, rate, timeline.total_frames)
      }], timeline.total_frames, rate));
    }
    const otio = {
      OTIO_SCHEMA: 'Timeline.1', name: 'Video Auto Cut',
      global_start_time: time(0, rate), metadata: {},
      tracks: {
        OTIO_SCHEMA: 'Stack.1', name: 'Tracks', children: otioTracks,
        source_range: null, effects: [], markers: [], metadata: {}, enabled: true
      }
    };
    await writeFile(join(stagingDir, 'timeline.otio'), `${JSON.stringify(otio, null, 2)}\n`, 'utf8');

    const rateXML = `<rate><timebase>${rate}</timebase><ntsc>FALSE</ntsc></rate>`;
    const videoXML = video.items.map((item) => xmlClip(item.id, basename(item.src),
      item.timeline_in_frame, item.timeline_out_frame, item.in_frame, item.out_frame,
      media.get(item.src))).join('');
    const audioXML = audioTracks.map((item) => {
      const filename = `audio_${item.id}.wav`;
      const file = {
        id: `file_audio_${item.id}`, name: filename, kind: 'audio',
        xmemlURL: xmemlFileURL(join(outputDir, filename)),
        duration: timeline.total_frames, rate, sampleRate: timeline.sample_rate
      };
      return `<track premiereTrackType="Stereo" currentExplodedTrackIndex="0" totalExplodedTrackCount="1">` +
        xmlClip(`stem_${item.id}`, item.name, 0, timeline.total_frames,
          0, timeline.total_frames, file) + `</track>`;
    }).join('');
    const xml = `<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE xmeml>\n` +
      `<xmeml version="4"><sequence id="sequence_1"><name>Video Auto Cut</name>` +
      `<duration>${timeline.total_frames}</duration>${rateXML}` +
      `<timecode>${rateXML}<string>00:00:00:00</string><frame>0</frame>` +
      `<displayformat>NDF</displayformat></timecode><media>` +
      `<video><format><samplecharacteristics><width>${timeline.media[video.items[0].src].video.width}</width>` +
      `<height>${timeline.media[video.items[0].src].video.height}</height>` +
      `<anamorphic>FALSE</anamorphic><pixelaspectratio>square</pixelaspectratio>` +
      `<fielddominance>none</fielddominance><colordepth>24</colordepth>${rateXML}` +
      `</samplecharacteristics></format><track>${videoXML}</track></video>` +
      `<audio><format><samplecharacteristics><depth>16</depth>` +
      `<samplerate>${timeline.sample_rate}</samplerate></samplecharacteristics>` +
      `</format><channelcount>2</channelcount>${audioXML}</audio></media></sequence></xmeml>\n`;
    await writeFile(join(stagingDir, 'edit.xml'), xml, 'utf8');
    return [...artifacts, { kind: 'xmeml', path: 'edit.xml' }, { kind: 'otio', path: 'timeline.otio' }];
  }
};
