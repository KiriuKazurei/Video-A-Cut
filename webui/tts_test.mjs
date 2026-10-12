import test from 'node:test';
import assert from 'node:assert/strict';
import {DEFAULT_TTS_SETTINGS, currentTtsPreview, ttsDraftError, ttsInstruction, ttsLivePlaybackRate, ttsLiveVolumeGain} from './src/ttsContracts.ts';

test('late results stay stale when any text or synthesis parameter changes',()=>{
 const s=DEFAULT_TTS_SETTINGS,p={status:'succeeded',expired:false,text:'当前文本',settings:{...s}};
 assert.equal(currentTtsPreview(p,'当前文本',s),true);
 for(const change of [{voice:'other'},{speed:1.5},{volume:.5},{pitch_semitones:3},{instruction:'轻松'},{endpoint:'https://other.example/v1'},{token_env:'OTHER'}])
  assert.equal(currentTtsPreview(p,'当前文本',{...s,...change}),false);
 assert.equal(currentTtsPreview(p,'已修改文本',s),false);
 assert.equal(currentTtsPreview({...p,expired:true},'当前文本',s),false);
 assert.equal(currentTtsPreview({...p,status:'running'},'当前文本',s),false);
});
test('text and combined instruction validate Unicode limits without truncation',()=>{
 const s=DEFAULT_TTS_SETTINGS;
 assert.equal(ttsDraftError('🎵'.repeat(1000),s),'');
 assert.match(ttsDraftError('文'.repeat(1001),s),/1,000/);
 assert.match(ttsDraftError(' ',s),/1,000/);
 assert.match(ttsDraftError('测试',{...s,instruction:ttsInstruction(1,'声'.repeat(200))}),/200/);
 assert.equal(ttsDraftError('',s,false),'');
});

test('live playbackRate maps StepFun speed and SAPI rate', () => {
  const s = DEFAULT_TTS_SETTINGS;
  assert.equal(ttsLivePlaybackRate({...s, engine: 'stepfun', speed: 1}), 1);
  assert.equal(ttsLivePlaybackRate({...s, engine: 'stepfun', speed: 1.5}), 1.5);
  assert.equal(ttsLivePlaybackRate({...s, engine: 'stepfun', speed: 0.5}), 0.5);
  assert.equal(ttsLivePlaybackRate({...s, engine: 'sapi', sapi_rate: 0}), 1);
  assert.equal(ttsLivePlaybackRate({...s, engine: 'sapi', sapi_rate: 10}), 2);
  assert.equal(ttsLivePlaybackRate({...s, engine: 'sapi', sapi_rate: -10}), 0.5);
  assert.equal(ttsLivePlaybackRate({...s, engine: 'sapi', sapi_rate: 5}), 1.5);
});
test('live volume gain clamps to 0–2 for GainNode', () => {
  const s = DEFAULT_TTS_SETTINGS;
  assert.equal(ttsLiveVolumeGain({...s, volume: 1}), 1);
  assert.equal(ttsLiveVolumeGain({...s, volume: 1.5}), 1.5);
  assert.equal(ttsLiveVolumeGain({...s, volume: 0.1}), 0.1);
});
