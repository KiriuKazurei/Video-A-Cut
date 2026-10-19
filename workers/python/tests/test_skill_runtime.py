import unittest
import tempfile
from pathlib import Path
from unittest import mock
from vac_worker.stages import StageContext, Tools, narrate, tts, subtitle
from vac_worker.skill_runtime import select_frames, extract, ToolFailure


class SkillSamplingTests(unittest.TestCase):
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
