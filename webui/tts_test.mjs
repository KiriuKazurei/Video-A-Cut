import test from 'node:test';
import assert from 'node:assert/strict';
import {DEFAULT_TTS_SETTINGS, currentTtsPreview, ttsDraftError, ttsInstruction} from './src/ttsContracts.ts';

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
