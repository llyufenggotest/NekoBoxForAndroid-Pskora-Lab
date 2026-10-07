import base64
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('gate', Path(__file__).with_name('verify_oix_native.py'))
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)

class NativeGateTests(unittest.TestCase):
    def test_reject_missing_and_malformed(self):
        seed = base64.b64encode(bytes(range(32))).decode()
        for data, text in [(b'', seed), (b'', ''), (b'', '!'), (seed.encode(), seed)]:
            with self.assertRaises(ValueError):
                gate.verify(data, text)
    def test_inclusion(self):
        seed = base64.b64encode(bytes(range(32))).decode()
        gate.verify(seed.encode() + b'EXPORTER-Dler-Snell-Identity-v2', seed)

if __name__ == '__main__':
    unittest.main()
