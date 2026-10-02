"""Regression gates for process isolation and genuine cross-execution adoption."""
import json, os, shutil, subprocess, sys, tempfile, threading, time, unittest
from pathlib import Path
from unittest.mock import patch
from test_f7_02 import RecordingClient, _cfg, _scope, _ingest_input
from vac_worker.ingest.common import Exec, IngestFailure, Runner
from vac_worker.ingest.supervise import Halt, LocalLease, ProcessRegistry
from vac_worker.ingest.loop import _ack_stopped, run_ingest_loop
from vac_worker.ingest.runtime import process_identity
from vac_worker.mcp import TransientTransportError, TransportError


class RecoveryRegressionTest(unittest.TestCase):
    def setUp(self):
        self.root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.root, True)

    def ex(self, execution, client):
        inp = _ingest_input(self.root, {})
        inp.update(execution_id=execution, output_dir=f"ingest/ing_1/tk_1/{execution}", package_dir=f"ingest/ing_1/tk_1/{execution}/package")
        return Exec(client, "tk_1", inp, self.root, {}, "ffmpeg", "ffprobe", threading.Event(), lambda _:None, scope=_scope(execution=execution, generation=1 if execution=='old' else 2))

    def candidate(self):
        return {"former_execution_id":"old", "generation":1, "stage":"media_probe", "input_sha256":"a"*64,
                "policy_sha256":"b"*64, "task_id":"tk_1", "output_dir":"ingest/ing_1/tk_1/old",
                "package_dir":"ingest/ing_1/tk_1/old/package", "journal_ref":"ingest/ing_1/tk_1/old/journal.v1.json", "checkpoint_count":0}

    def test_live_instance_registry_is_never_reclaimed(self):
        old = ProcessRegistry(self.root, 'live-old')
        proc = subprocess.Popen([sys.executable, '-c', 'import time;time.sleep(30)'])
        self.addCleanup(lambda: proc.poll() is not None or (proc.kill(),proc.wait(timeout=5)))
        old.note(proc.pid)
        new = ProcessRegistry(self.root, 'new-instance')
        with patch('vac_worker.ingest.supervise.taskkill_tree') as kill:
            self.assertEqual(new.recover_orphans(), 'clear')
            kill.assert_not_called()
        self.assertIsNone(proc.poll())
        self.assertNotEqual(new.path, old.path)

    def test_expired_business_lease_does_not_block_stop_ack(self):
        client=RecordingClient(lambda t,a: {'ok':True})
        lease=LocalLease(); lease.deadline=time.monotonic()-1
        _ack_stopped(client, _scope(), Halt(), lease, lambda _:None, 'stopped')
        self.assertEqual(client.names(), ['ack_execution_stopped'])

    def test_inaccessible_process_is_not_treated_as_dead_or_killed(self):
        from unittest.mock import MagicMock
        if os.name == 'nt':
            kernel = MagicMock()
            kernel.OpenProcess.return_value = 0
            with patch('vac_worker.ingest.runtime.ctypes.WinDLL', return_value=kernel), patch('vac_worker.ingest.runtime.ctypes.get_last_error', return_value=5):
                with self.assertRaises(OSError):
                    process_identity(12345)
            with patch('vac_worker.ingest.runtime.ctypes.WinDLL', return_value=kernel), patch('vac_worker.ingest.runtime.ctypes.get_last_error', return_value=87):
                self.assertIsNone(process_identity(12345))
        old = ProcessRegistry(self.root, 'unknown-owner')
        old.entries = [{'pid':12345, 'created':'1', 'path':'unknown', 'job_enforced':True}]
        old._publish()
        new = ProcessRegistry(self.root, 'replacement')
        with patch('vac_worker.ingest.isolation.matches_process', side_effect=PermissionError('identity inaccessible')), patch('vac_worker.ingest.supervise.taskkill_tree') as kill:
            self.assertEqual(new.recover_orphans(), 'cleanup_blocked')
            self.assertEqual(old.stop_owned(Halt(), grace=0, force_wait=0), 'cleanup_blocked')
            kill.assert_not_called()

    def test_unknown_job_drainage_closes_handle_but_reports_blocked(self):
        from vac_worker.ingest.runtime import OwnedJob
        from unittest.mock import MagicMock
        job = object.__new__(OwnedJob)
        job.handle = 123
        job.k = MagicMock()
        job.k.QueryInformationJobObject.return_value = 0
        with patch('vac_worker.ingest.runtime.ctypes.get_last_error', return_value=5, create=True):
            with self.assertRaises(OSError):
                job.close()
        job.k.CloseHandle.assert_called_once_with(123)
        self.assertIsNone(job.handle)

    def test_connection_resume_checks_deferred_stop_before_acknowledging(self):
        registry=ProcessRegistry(self.root,'conditional-ack')
        scope=_scope();registry.queue_ack(scope,'stopped',conditional=True)
        client=RecordingClient(lambda name,args:{'status':'running','command':'none'} if name=='get_execution_control' else {'ok':True})
        registry.flush_acks(client,lambda _:None)
        self.assertNotIn('ack_execution_stopped',client.names())
        self.assertEqual(len(registry.pending_acks),1)
        client.handler=lambda name,args:{'status':'stop_requested','command':'stop'} if name=='get_execution_control' else {'ok':True}
        registry.flush_acks(client,lambda _:None)
        self.assertEqual(client.names().count('ack_execution_stopped'),1)
        self.assertEqual(registry.pending_acks,[])

    def test_startup_disconnect_recovers_inside_worker(self):
        client=RecordingClient(lambda t,a: {'claimed':False})
        calls=[]
        def listing():
            calls.append(1)
            if len(calls)==1: raise TransientTransportError('injected startup outage')
            from vac_worker.ingest.loop import REQUIRED_TOOLS
            return list(REQUIRED_TOOLS)
        client.list_tools=listing
        stop=threading.Event()
        def handler(t,a):
            if t=='claim_task': stop.set()
            return {'claimed':False}
        client.handler=handler
        with patch('vac_worker.ingest.loop.backoff_delay',return_value=0.001):
            self.assertEqual(run_ingest_loop(client,_cfg(self.root),lambda _:None,stop)['outcome'],'stopped')
        self.assertEqual(len(calls),2)

    def test_sealed_package_is_adopted_without_mutating_historical_receipt(self):
        offline=RecordingClient(lambda t,a: (_ for _ in ()).throw(TransientTransportError('publish disconnected')))
        old=self.ex('old',offline)
        old.fresh_staging()
        (old.staging/'probe.json').write_text('{"fixture":true}',encoding='utf-8')
        with self.assertRaises(TransportError):
            with patch('vac_worker.ingest.supervise.RETRY_BUDGET_SECONDS',0.01): old.publish()
        receipt=(old.package_dir/'worker-receipt.json').read_bytes()
        online=RecordingClient(lambda t,a: {'status':'not_committed'} if t=='get_execution_result_status' else {'ok':True})
        new=self.ex('new',online)
        self.assertTrue(new.recover([self.candidate()]))
        self.assertEqual((old.package_dir/'worker-receipt.json').read_bytes(),receipt)
        self.assertEqual(json.loads((new.package_dir/'worker-receipt.json').read_text())['execution_id'],'new')
        self.assertEqual(new.journal.data['recovered_from_execution_id'],'old')
        self.assertEqual(online.names().count('submit_ingest_result'),1)

    def test_unregistered_local_checkpoint_is_recovered_and_tamper_rejected(self):
        offline=RecordingClient(lambda t,a: (_ for _ in ()).throw(TransientTransportError('checkpoint disconnected')))
        old=self.ex('old',offline)
        with self.assertRaises(TransportError):
            with patch('vac_worker.ingest.supervise.RETRY_BUDGET_SECONDS',0.01): old.save_checkpoint('scan_chunk',0,{'range_us':[0,1,1],'cuts':[]})
        new=self.ex('new',RecordingClient(lambda t,a: {'ok':True}))
        self.assertFalse(new.recover([self.candidate()]))
        self.assertEqual(len(new.prior_checkpoints('scan_chunk')),1)
        row=new.inp['checkpoints'][0]
        (self.root/row['ref']).write_text('{}',encoding='utf-8')
        self.assertEqual(new.prior_checkpoints('scan_chunk'),[])

    def test_same_execution_replays_pending_checkpoint_before_advancing_sequence(self):
        offline=RecordingClient(lambda t,a: (_ for _ in ()).throw(TransientTransportError('checkpoint disconnected')))
        old=self.ex('old',offline)
        with self.assertRaises(TransportError):
            with patch('vac_worker.ingest.supervise.RETRY_BUDGET_SECONDS',0.01):old.save_checkpoint('scan_chunk',0,{'range_us':[0,1,1],'cuts':[]})
        request=old.journal.pending_args('save_ingest_checkpoint')['request_id']
        client=RecordingClient(lambda t,a:{'ok':True})
        resumed=self.ex('old',client)
        self.assertFalse(resumed.recover([]))
        self.assertEqual(len(resumed.prior_checkpoints('scan_chunk')),1)
        self.assertEqual(client.calls[0][1],old.journal.pending_args('save_ingest_checkpoint')['args'])
        self.assertEqual(resumed.journal.pending_args('save_ingest_checkpoint')['request_id'],request)
        resumed.save_checkpoint('scan_chunk',1,{'range_us':[1,2,2],'cuts':[]})
        self.assertEqual(client.calls[-1][1]['sequence'],2)

    @unittest.skipUnless(os.name=='nt','Windows kernel-owned tree')
    def test_worker_crash_ends_owned_descendants(self):
        script=self.root/'crash-owner.py'
        script.write_text("import sys,threading\nfrom pathlib import Path\nsys.path.insert(0,sys.argv[1])\nfrom vac_worker.ingest.common import Runner\nfrom vac_worker.ingest.supervise import ProcessRegistry\nr=ProcessRegistry(Path(sys.argv[2]),'crash')\nRunner(threading.Event(),registry=r).run([sys.executable,'-c',\"import subprocess,sys,time;from pathlib import Path;p=subprocess.Popen([sys.executable,'-c','import time;time.sleep(90)']);Path(sys.argv[1]).write_text(str(p.pid));time.sleep(90)\",str(Path(sys.argv[2])/'grandchild.pid')],timeout=90)\n",encoding='utf-8')
        worker=subprocess.Popen([sys.executable,str(script),str(Path(__file__).resolve().parents[1]),str(self.root)],stdout=subprocess.DEVNULL)
        self.addCleanup(lambda: worker.poll() is not None or (worker.kill(),worker.wait(timeout=5)))
        record=self.root/'.vac-runtime/instances/crash.json'
        deadline=time.monotonic()+10
        while time.monotonic()<deadline:
            if record.exists():
                doc=json.loads(record.read_text())
                if doc.get('entries') and (self.root/'grandchild.pid').exists(): break
            time.sleep(0.05)
        else: self.fail('owned child not registered')
        child=doc['entries'][0]
        grandchild=int((self.root/'grandchild.pid').read_text())
        self.assertIsNotNone(process_identity(grandchild))
        self.assertTrue(child['job_enforced'])
        worker.kill(); worker.wait(timeout=5)
        deadline=time.monotonic()+5
        while process_identity(child['pid']) and time.monotonic()<deadline: time.sleep(0.05)
        self.assertIsNone(process_identity(child['pid']))
        while process_identity(grandchild) and time.monotonic()<deadline: time.sleep(0.05)
        self.assertIsNone(process_identity(grandchild))
        self.assertEqual(ProcessRegistry(self.root,'successor').recover_orphans(),'clear')

    @unittest.skipUnless(os.name=='nt','Windows kernel-owned tree')
    def test_root_exit_drains_pipe_holding_grandchild_before_registry_release(self):
        registry=ProcessRegistry(self.root,'root-exit')
        pidfile=self.root/'pipe-grandchild.pid'
        code="import subprocess,sys;from pathlib import Path;p=subprocess.Popen([sys.executable,'-c','import time;time.sleep(90)']);Path(sys.argv[1]).write_text(str(p.pid))"
        started=time.monotonic()
        Runner(threading.Event(),registry=registry).run([sys.executable,'-c',code,str(pidfile)],timeout=10)
        self.assertLess(time.monotonic()-started,10)
        self.assertIsNone(process_identity(int(pidfile.read_text())))
        self.assertEqual(json.loads(registry.path.read_text())['entries'],[])

    def test_stdout_write_failure_propagates_after_owned_child_drains(self):
        registry=ProcessRegistry(self.root,'write-failure')
        def failed_write(_): raise OSError('injected disk write failure')
        with self.assertRaisesRegex(OSError,'disk write failure'):
            Runner(threading.Event(),registry=registry).run([sys.executable,'-c','print("bytes")'],timeout=10,stdout_sink=failed_write)
        self.assertEqual(json.loads(registry.path.read_text())['entries'],[])
