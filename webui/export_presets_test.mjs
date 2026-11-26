import test from 'node:test';
import assert from 'node:assert/strict';
import { SEQUENCE_EXPORT_PRESETS, sequenceExportRequest } from './src/exportPresets.ts';

test('sequence export defaults to the current program and keeps named presets', () => {
  assert.equal(SEQUENCE_EXPORT_PRESETS[0].id, 'program-native');
  assert.deepEqual(SEQUENCE_EXPORT_PRESETS.map(item => item.id), ['program-native', 'mp4-720p-v1', '1080p-landscape', '1080x1920-vertical', '2160p-4k', 'custom']);
  assert.equal(sequenceExportRequest({ presetId: 'program-native', codec: 'h264', rateMode: 'crf', crf: 20, bitrate: '8M', subtitles: 'burn', loudness: '' }), undefined);
  const vertical = sequenceExportRequest({ presetId: '1080x1920-vertical', codec: 'hevc', rateMode: 'bitrate', crf: 20, bitrate: '8M', subtitles: 'sidecar', loudness: '-14', threads: 2 });
  assert.equal(vertical.preset_id, '1080x1920-vertical');
  assert.equal(vertical.codec, 'hevc');
  assert.equal(vertical.video_bitrate, '8M');
  assert.equal(vertical.crf, undefined);
  assert.equal(vertical.subtitles, 'sidecar');
  assert.equal(vertical.loudness, '-14');
  assert.equal(vertical.threads, 2);
  const custom = sequenceExportRequest({ presetId: 'custom', width: 1080, height: 1920, codec: 'h264', rateMode: 'crf', crf: 18, bitrate: '8M', subtitles: 'burn', loudness: '' });
  assert.deepEqual([custom.width, custom.height, custom.crf], [1080, 1920, 18]);
});
