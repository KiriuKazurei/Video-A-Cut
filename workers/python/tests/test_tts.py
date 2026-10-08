"""Transport contracts and real DSP, with no paid request or test-tone fallback."""
import array
import io
import json
import math
import os
import shutil
import tempfile
import unittest
import urllib.error
import wave
from pathlib import Path
from unittest.mock import patch

from vac_worker.stages import Tools, StageContext, StageFailure, STAGES
from vac_worker.tts import TTSError, validate_settings, synthesize_raw, synthesize, legacy_settings


def settings(**changes):
    return {**dict(engine='stepfun', endpoint='https://api.stepfun.com/step_plan/v1',
                model='stepaudio-2.5-tts', token_env='VAC_TTS_UNIT_TOKEN', voice='vibrant-youth',
                language='zh', speed=1.25, volume=.7, pitch_semitones=0, sapi_rate=0,
                instruction='温柔地讲述'), **changes}


def tone(seconds=2):
    stream = io.BytesIO()
    with wave.open(stream, 'wb') as audio:
        audio.setnchannels(1); audio.setsampwidth(2); audio.setframerate(24000)
        samples = array.array('h', (round(10000*math.sin(2*math.pi*440*i/24000)) for i in range(round(24000*seconds))))
        audio.writeframes(samples.tobytes())
    return stream.getvalue()


class TransportTest(unittest.TestCase):
    def test_request_preserves_plan_path_and_parameters(self):
        seen = []
        def open_request(req, timeout):
            seen.append((req.full_url, json.loads(req.data), req.headers, timeout))
            return io.BytesIO(tone(.1))
        with patch.dict(os.environ, {'VAC_TTS_UNIT_TOKEN': 'test-only-token'}), tempfile.TemporaryDirectory() as tmp:
            synthesize_raw('测试文本', Path(tmp)/'out.wav', settings(), Tools(timeout=20), opener=open_request)
        url, payload, headers, deadline = seen[0]
        self.assertEqual(url, 'https://api.stepfun.com/step_plan/v1/audio/speech')
        self.assertEqual((payload['speed'],payload['volume'],payload['instruction']), (1.25,.7,'温柔地讲述'))
        self.assertEqual((payload['model'],payload['response_format'],payload['sample_rate']), ('stepaudio-2.5-tts','wav',24000))
        self.assertNotIn('pitch_semitones',payload)
        self.assertEqual(deadline,20)

    def test_missing_key_empty_text_and_limits_do_not_call_network(self):
        with patch.dict(os.environ, {'VAC_TTS_UNIT_TOKEN': ''}), tempfile.TemporaryDirectory() as tmp:
            for text in ('', '  ', '声'*1001, '测试'):
                with self.subTest(text=text[:10]), self.assertRaises(TTSError):
                    synthesize_raw(text,Path(tmp)/'a.wav',settings(),Tools(),opener=lambda *_:self.fail('unexpected network'))
        with self.assertRaises(TTSError): validate_settings(settings(instruction='声'*201))

    def test_authentication_timeout_and_corrupt_audio_are_bounded(self):
        for error in (urllib.error.HTTPError('url',401,'secret echoed by provider',{},None), TimeoutError('secret')):
            def fail(*args,**kwargs):raise error
            with patch.dict(os.environ, {'VAC_TTS_UNIT_TOKEN':'secret'}), tempfile.TemporaryDirectory() as tmp, self.assertRaises(TTSError) as caught:
                synthesize_raw('测试',Path(tmp)/'out.wav',settings(),Tools(),opener=fail)
            self.assertNotIn('secret',str(caught.exception))
        with patch.dict(os.environ, {'VAC_TTS_UNIT_TOKEN':'test'}), tempfile.TemporaryDirectory() as tmp, self.assertRaisesRegex(TTSError,'WAV'):
            synthesize_raw('测试',Path(tmp)/'out.wav',settings(),Tools(),opener=lambda *_args,**_kwargs:io.BytesIO(b'not audio'))

    def test_legacy_settings_keep_native_rate(self):
        self.assertEqual(legacy_settings('selected')['sapi_rate'],5)
        validate_settings(legacy_settings(''),legacy=True)


@unittest.skipUnless(shutil.which('ffmpeg') and shutil.which('ffprobe'), 'requires FFmpeg')
class AudioTest(unittest.TestCase):
    @unittest.skipUnless(os.name == 'nt', 'requires installed Windows SAPI')
    def test_real_sapi_fixed_profile_narration_and_subtitle(self):
        from test_stages import make_source
        from vac_worker.tts import capabilities
        from vac_worker.worker import Config
        from vac_worker.preparation import configured_task
        from vac_worker.content import narration_draft_hash
        voices = capabilities()['sapi_voices']
        if not voices: self.skipTest('no SAPI voices installed')
        config = dict(engine='sapi', voice=voices[0], language='zh', speed=1,
                      volume=.8, pitch_semitones=3, sapi_rate=0, instruction='')
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);src=root/'src';edl=make_source(src)
            edl['narration']=[dict(id='nar_001',text='配音测试。',start=0,end=3,source_scene_label='测试场景')]
            profile=dict(narration={'adapter':'builtin'},sampling={'max_frames':12,'max_bytes':12582912,'timeout_seconds':60},
                         tts_voice='legacy-ignored',tts_template={'template_id':'fixed_real','revision':1,'sha256':'b'*64,'settings':config})
            cfg=configured_task(Config('http://local/mcp','test',root,role='narrator',profile_sha256='a'*64),
                                {'processing_profile':profile,'profile_sha256':'a'*64})
            self.assertEqual(cfg.tts_settings,config)
            out=root/'tts';out.mkdir()
            digest=narration_draft_hash('配音测试。',0,3,'测试场景')
            result=STAGES['tts'](StageContext(edl=edl,source_dir=src,out_dir=out,tools=Tools(),
                                            narration_approvals=(digest,),tts_settings=cfg.tts_settings))
            self.assertEqual(result['engine'],'sapi')
            final=json.loads((out/'edl.json').read_text(encoding='utf-8'));clip=final['voice'][0]
            self.assertAlmostEqual(Tools().duration(out/clip['src']),clip['end']-clip['start'],delta=.0001)
            sub=root/'subtitle';sub.mkdir()
            STAGES['subtitle'](StageContext(edl=final,source_dir=out,out_dir=sub,tools=Tools()))
            captions=json.loads((sub/'edl.json').read_text(encoding='utf-8'))['subtitle']
            self.assertEqual(captions[0]['end'],clip['end'])

    def test_real_rubberband_changes_pitch_and_preserves_duration(self):
        with tempfile.TemporaryDirectory() as tmp, patch('vac_worker.tts.synthesize_raw',side_effect=lambda text,out,*a,**kw:out.write_bytes(tone())):
            out=Path(tmp)/'shifted.wav';result=synthesize('测试',out,settings(pitch_semitones=12),Tools())
            with wave.open(str(out),'rb') as audio:
                self.assertEqual((audio.getframerate(),audio.getnchannels(),audio.getsampwidth()),(48000,2,2))
                samples=array.array('h');samples.frombytes(audio.readframes(audio.getnframes()))
                middle=samples[48000:144000:2]
                crossings=sum(a<=0<b for a,b in zip(middle,middle[1:]))
                frequency=crossings/(len(middle)/48000)
            self.assertAlmostEqual(frequency,880,delta=5)
            self.assertAlmostEqual(result['duration_seconds'],2,delta=.03)

    def test_formal_narration_uses_shared_synthesis_and_final_duration(self):
        from test_stages import make_source
        with tempfile.TemporaryDirectory() as tmp, patch('vac_worker.tts.synthesize_raw',side_effect=lambda text,out,*a,**kw:out.write_bytes(tone(.75))) as synth:
            root=Path(tmp);src=root/'src';edl=make_source(src)
            edl['narration']=[dict(id='nar_001',text='审核后的文本',start=0,end=1.5)]
            out=root/'out';out.mkdir()
            STAGES['tts'](StageContext(edl=edl,source_dir=src,out_dir=out,tools=Tools(),tts_auto_approve=True,tts_settings=settings(pitch_semitones=3)))
            final=json.loads((out/'edl.json').read_text(encoding='utf-8'))
            clip=final['voice'][0];duration=Tools().duration(out/clip['src'])
            self.assertAlmostEqual(clip['end']-clip['start'],duration,delta=.0001)
            self.assertEqual(synth.call_args.args[2]['pitch_semitones'],3)
            rejected=root/'unapproved';rejected.mkdir()
            with self.assertRaisesRegex(StageFailure,'unreviewed'):
                STAGES['tts'](StageContext(edl=edl,source_dir=src,out_dir=rejected,tools=Tools(),tts_settings=settings()))


if __name__=='__main__':unittest.main()
