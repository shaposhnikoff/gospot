"""Mode selection and conditional MMDVMHost configuration, not gateway emulation."""
import unittest
from test_config import h


class ProtocolTest(unittest.TestCase):
    def setUp(self):
        self.ini = ['[System Fusion]\r\nEnable=0\r\nTXHang=4  # seconds\r\n'
                    '[System Fusion Network]\r\nEnable=0\r\nGatewayPort=42000\r\n'
                    '[DMR]\r\nEnable=1\r\nColorCode=3\r\n[DMR Network]\r\nPassword=private',
                    '# unrelated gateway']
        self.config = h.protocol_config(self.ini)

    def test_only_present_modes_and_fields_roundtrip(self):
        self.assertEqual(set(self.config['modes']), {'System Fusion','DMR'})
        self.assertNotIn('private', str(self.config))
        req=dict(revision=self.config['revision'],changes={'System Fusion':{'Enable':'1','net.Enable':'1','TXHang':'7'}})
        new=h.protocol_config(self.ini, req)
        self.assertEqual(new,[self.ini[0].replace('Enable=0','Enable=1').replace('TXHang=4','TXHang=7'),self.ini[1]])
        self.assertEqual(h.protocol_config(new)['modes']['System Fusion']['TXHang'],'7')
        with self.assertRaises(h.Stale): h.protocol_config(new,req)

    def test_reject_unknown_mode_hidden_key_and_invalid_values(self):
        for diff in [{'M17':{'Enable':'1'}},{'DMR':{'net.Password':'test'}},
                     {'DMR':{'ColorCode':'16'}},{'System Fusion':{'net.GatewayPort':'65536'}},
                     {'DMR':{'ColorCode':'3\nEnable=0'}}]:
            with self.subTest(diff=diff), self.assertRaises(h.Error):
                h.protocol_config(self.ini,dict(revision=self.config['revision'],changes=diff))
