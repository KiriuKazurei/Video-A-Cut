import unittest
import tempfile
from pathlib import Path
from unittest import mock
import http.server
import shutil
import threading
import json
import types
import sys
from decimal import Decimal
from vac_worker.stages import StageContext, Tools, narrate, tts, subtitle
from vac_worker.skill_runtime import agent, select_frames, extract, ToolFailure, run, transcribe, probe_transcription


class LocalTranscriptionTests(unittest.TestCase):
    def execute(self, *, missing=None, words=None, model_error=False):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            model = root/'model'; model.mkdir()
            for name in ('config.json', 'model.bin', 'tokenizer.json'):
                if name != missing:
                    (model/name).write_bytes(b'fixture')
            source = root/'source.mp4'; source.write_bytes(b'media')
            output = root/'transcript.json'
            word = types.SimpleNamespace(word=' hello', start=Decimal('.25'), end=Decimal('.8'), probability=Decimal('.9'))
            engine = mock.Mock()
            engine.transcribe.return_value = (iter([types.SimpleNamespace(text=' hello', words=[word] if words is None else words)]), types.SimpleNamespace(language='en', language_probability=.98))
            constructor = mock.Mock(return_value=engine)
            if model_error:
                constructor.side_effect = RuntimeError('local failure')
            module = types.SimpleNamespace(WhisperModel=constructor, __version__='fixture')
            request = {'provider': {'api_format': 'faster_whisper', 'model_path': str(model), 'device': 'cpu', 'compute_type': 'int8'},
                       'path': str(source), 'start': 5., 'end': 7., 'output': str(output), 'model_fingerprint': 'frozen-model'}
            def extract_audio(args, *_):
                Path(args[-1]).write_bytes(b'audio')
                return b''
            with mock.patch.dict(sys.modules, {'faster_whisper': module}), mock.patch('vac_worker.skill_runtime.run', side_effect=extract_audio), mock.patch('vac_worker.skill_runtime.local_pcm_samples', return_value=object()), mock.patch('urllib.request.build_opener', side_effect=AssertionError('local ASR attempted HTTP')):
                if missing or model_error or words == []:
                    with self.assertRaises(ToolFailure) as caught:
                        transcribe(request)
                    self.assertEqual(caught.exception.code, 'local_asr_model_missing' if missing else 'local_asr_runtime_failed' if model_error else 'no_speech_detected')
                    self.assertFalse(output.exists())
                else:
                    result = transcribe(request)
                    self.assertEqual(result['words'][0]['start'], 5.25)
                    self.assertEqual(result['words'][0]['end'], 5.8)
                    self.assertEqual(result['provider'], 'faster-whisper')
                    self.assertEqual(result, json.loads(output.read_text(encoding='utf-8')))
                    self.assertTrue(constructor.call_args.kwargs['local_files_only'])
                    self.assertTrue(engine.transcribe.call_args.kwargs['word_timestamps'])
                    self.assertEqual(probe_transcription(request)['dependency_version'], 'fixture')
                self.assertFalse(output.with_suffix('.wav').exists())
                self.assertEqual(list(root.glob('*.tmp-*')), [])

    def test_local_word_times_are_offset_and_never_uploaded(self):
        self.execute()

    def test_missing_tokenizer_never_loads_or_downloads(self):
        self.execute(missing='tokenizer.json')

    def test_model_failure_does_not_fallback_or_publish(self):
        self.execute(model_error=True)

    def test_silence_does_not_fabricate_words(self):
        self.execute(words=[])

    def test_missing_dependency_is_actionable(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name in ('config.json', 'model.bin', 'tokenizer.json'):
                (root/name).write_bytes(b'model')
            with mock.patch.dict(sys.modules, {'faster_whisper': None}):
                with self.assertRaises(ToolFailure) as caught:
                    probe_transcription({'provider': {'api_format': 'faster_whisper', 'model_path': str(root)}})
            self.assertEqual(caught.exception.code, 'local_asr_dependency_missing')


class SkillSamplingTests(unittest.TestCase):
    def test_builtin_agent_receives_typed_frame_context_and_current_tool_contract(self):
        reference = {'kind': 'source_marker', 'target_id': 'point', 'clip_id': 'instance',
                     'resource_id': 'media', 'name': '重名', 'source_time': {'value': 441, 'scale': 44100}}
        request = {'history': [], 'run': {'goal': '编辑标记帧', 'sequence_id': 'seq'},
                   'skill': {'content': 'VAC adapter', 'resources': {'vac/tool-contracts.md': 'current typed contract'}},
                   'media': [], 'sequence': {'session': 'session', 'sequence': {'id': 'seq', 'fps': {'num': 30000, 'den': 1001}}},
                   'references': [reference], 'tools': ['timeline.read', 'timeline.edit', 'references.resolve'],
                   'provider': {'endpoint': 'local-fixture'}}
        with mock.patch('vac_worker.skill_runtime.call_model', return_value={'tool': 'timeline.read', 'arguments': {}}) as model:
            result = agent(request)
        prompt = model.call_args.args[1]
        payload = json.loads(prompt.rsplit('\n', 1)[1])
        self.assertEqual(payload['references'], [reference])
        self.assertEqual(payload['sequence'], request['sequence'])
        self.assertIn('current typed contract', prompt)
        self.assertIn('run.finish {sequence_revision,content_version}', prompt)
        self.assertEqual(result['tool'], 'timeline.read')

    def test_media_playlist_cannot_make_hidden_network_requests(self):
        if not shutil.which('ffprobe'):
            self.skipTest('ffprobe not installed')
        requests = []
        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                requests.append(self.path)
                self.send_response(404)
                self.end_headers()
            def log_message(self, *_):
                pass
        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as tmp:
                path = Path(tmp)/'untrusted.mp4'
                path.write_text('#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\n'
                    f'http://127.0.0.1:{server.server_port}/hidden.ts\n#EXT-X-ENDLIST\n')
                with self.assertRaises(ToolFailure):
                    run(['ffprobe', '-v', 'error', '-show_format', str(path)], 10)
            self.assertEqual(requests, [])
        finally:
            server.shutdown()
            server.server_close()
            thread.join(2)

    def test_continuous_changes_thin_one_in_three(self):
        samples = [bytes([0])*576, bytes([255])*576, bytes([0])*576,
                   bytes([255])*576, bytes([0])*576, bytes([255])*576]
        rows = select_frames(samples, 0., 6., coverage=120.)
        self.assertEqual([r['pts'] for r in rows if r['strategy'].startswith('change')], [1., 4.])

    def test_static_scene_keeps_edges_and_coverage(self):
        rows = select_frames([bytes([10])*576]*40, 0., 40., coverage=15.)
        self.assertEqual([r['pts'] for r in rows], [0., 15., 30., 39.95])

    def test_bad_strategy_fails_before_decode(self):
        with self.assertRaises(ToolFailure) as error:
            extract({'every': -1, 'max_frames': 10, 'sources': [], 'output': ''})
        self.assertEqual(error.exception.code, 'invalid_strategy')

    def test_fixed_script_does_not_regenerate_and_subtitles_work_without_tts(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root/'source.mp4').write_bytes(b'fixture')
            out = root/'out'
            out.mkdir()
            source = {'timeline': {'fps': 30, 'sample_rate': 48000}, 'video': [],
                      'game_audio': [], 'voice': [], 'subtitle': [], 'music': []}
            snapshot = {'script_id': 'scr_1', 'version': 4, 'visible_set_sha': 'sha',
                'sources': {'m': 'source.mp4'}, 'workbench': {'tts_enabled': False,
                'subtitles_enabled': True, 'document': {'paragraphs': [{}]},
                'segments': [{'media_id': 'm', 'start': 1., 'end': 2., 'narration': '人工定稿原文'}]}}
            ctx = StageContext(source, root, out, Tools(), script_snapshot=snapshot, delivery_root=root)
            with mock.patch('vac_worker.stages.package') as publish:
                narrate(ctx)
                ctx.edl = publish.call_args.args[1]
                self.assertEqual(ctx.edl['narration'][0]['text'], '人工定稿原文')
                self.assertEqual(ctx.edl['video'][0]['timeline_in'], 0.)
                tts(ctx)
                ctx.edl = publish.call_args.args[1]
                self.assertEqual(ctx.edl['voice'], [])
                subtitle(ctx)
                self.assertEqual(publish.call_args.args[1]['subtitle'][0]['text'], '人工定稿原文')
