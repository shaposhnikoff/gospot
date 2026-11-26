"""Regression tests for the Debian Configuration and Expert editor transactions."""
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from test_config import h


class DMRServicesTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = Path(tmp.name)
        p = patch.multiple(h, STATE=root / 'state', HOST=root / 'MMDVM-Host.ini', GW=root / 'DMRGateway.ini')
        p.start()
        self.addCleanup(p.stop)
        self.host = b'[DMR]\r\nEnable=1\r\nColorCode=1\r\n[DMR Network]\r\nEnable=1\r\nSlot1=0\r\nSlot2=1\r\n'
        self.gw = (b'[DMR Network 1]\r\nEnabled=1\r\nAddress=master.invalid\r\nPort=62031\r\n'
                   b'TGRewrite0=2,91,2,91,1\r\nTGRewrite0=  2,92,2,92,1  # fixture\r\n'
                   b'[DMR Network 2]\r\nTGRewrite0=2,92,2,92,1')
        h.HOST.write_bytes(self.host)
        h.GW.write_bytes(self.gw)
        h.HOST.chmod(0o640)
        h.GW.chmod(0o644)
        self.restarts = []
        self.config = h.execute('services-read')

    @staticmethod
    def metadata(path):
        s = path.stat()
        return s.st_uid, s.st_gid, s.st_mode & 0o777

    def test_color_does_not_reinstall_or_chmod_gateway(self):
        prev = h.GW.stat()
        owner_mode = self.metadata(h.HOST)
        h.execute('services-apply', dict(self.config, cc=12), self.restarts.append)
        self.assertEqual(h.HOST.read_bytes(), self.host.replace(b'ColorCode=1\r', b'ColorCode=12\r'))
        self.assertEqual(self.metadata(h.HOST), owner_mode)
        self.assertEqual(h.GW.read_bytes(), self.gw)
        now = h.GW.stat()
        self.assertEqual((now.st_ino, now.st_ctime_ns, now.st_mode), (prev.st_ino, prev.st_ctime_ns, prev.st_mode))
        self.assertEqual(self.restarts, ['mmdvmhost.service'])

    def test_second_physical_rule_only_and_stale_form(self):
        changed = dict(self.config, rules=['2,91,2,91,1', '2,92,2,310,1'])
        owner_mode = self.metadata(h.GW)
        h.execute('services-apply', changed, self.restarts.append)
        saved = h.GW.stat()
        with self.assertRaises(h.Stale):
            h.execute('services-apply', self.config, self.restarts.append)
        self.assertEqual(h.GW.read_bytes(), self.gw.replace(b'2,92,2,92,1  #', b'2,92,2,310,1  #'))
        self.assertEqual(self.metadata(h.GW), owner_mode)
        self.assertEqual(h.GW.stat().st_ctime_ns, saved.st_ctime_ns)
        self.assertEqual(h.HOST.read_bytes(), self.host)
        self.assertEqual(h.execute('services-read')['rules'], changed['rules'])
        self.assertEqual(self.restarts, ['dmrgateway.service'])

    def test_restart_failure_restores_both_inputs(self):
        modes = [self.metadata(p) for p in (h.HOST, h.GW)]
        def restart(unit):
            self.restarts.append(unit)
            if len(self.restarts) == 1:
                raise OSError('fixture restart failure')
        with self.assertRaisesRegex(h.Error, 'restored'):
            h.execute('services-apply', dict(self.config, cc=9, rules=['2,91,2,310,1','2,92,2,92,1']), restart)
        self.assertEqual([h.HOST.read_bytes(), h.GW.read_bytes()], [self.host, self.gw])
        self.assertEqual([self.metadata(p) for p in (h.HOST, h.GW)], modes)
        self.assertEqual(self.restarts, ['dmrgateway.service', 'dmrgateway.service'])
        self.assertFalse((h.STATE/'pending.json').exists())
