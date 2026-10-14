import base64
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location('prepare_oix_dns_auth', HERE / 'prepare_oix_dns_auth.py')
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class PrepareOixDnsAuthTest(unittest.TestCase):
    def test_materialize_returns_domain_separated_nonzero_fingerprint(self):
        seed = base64.b64encode(bytes(range(32))).decode()
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / 'private_seed_generated.go'
            fingerprint = MODULE.materialize(seed, target)
            generated = target.read_text(encoding='utf-8')
        self.assertRegex(fingerprint, r'^[0-9a-f]{64}$')
        self.assertNotEqual(fingerprint, '0' * 64)
        self.assertNotEqual(fingerprint, MODULE.hashlib.sha256(bytes(range(32))).hexdigest())
        self.assertIn('BuildSeedFingerprint = ' + json.dumps(MODULE.FINGERPRINT_MARKER + fingerprint), generated)
        self.assertIn(seed, generated)

    def test_cli_writes_only_fingerprint_to_github_output(self):
        seed = base64.b64encode(bytes(range(32))).decode()
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / 'github-output'
            target = Path(directory) / 'private_seed_generated.go'
            stdout = io.StringIO()
            with contextlib.redirect_stdout(stdout), mock.patch.dict(
                os.environ,
                {'OIX_DNS_AUTH_SEED': seed, 'GITHUB_OUTPUT': str(output)},
                clear=False,
            ), mock.patch.object(MODULE, 'TARGET', target):
                MODULE.main()
            self.assertNotIn(seed, stdout.getvalue())
            self.assertNotIn(seed, output.read_text(encoding='utf-8'))
            self.assertRegex(output.read_text(encoding='utf-8'), r'^fingerprint=[0-9a-f]{64}\n$')


if __name__ == '__main__':
    unittest.main()
