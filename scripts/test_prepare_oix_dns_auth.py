import base64
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
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
        marker = MODULE.FINGERPRINT_MARKER_PREFIX + fingerprint + MODULE.marker_checksum(fingerprint) + MODULE.FINGERPRINT_MARKER_SUFFIX
        self.assertIn('BuildSeedFingerprint = ' + json.dumps(marker), generated)
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


    def test_real_go_build_keeps_marker_and_checks_it_in_signing_path(self):
        seed = base64.b64encode(bytes(range(32))).decode()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            package = root / 'oixdnsauth'
            package.mkdir()
            shutil.copy2(HERE.parent / 'core115-source/sing-box/component/oixdnsauth/auth.go', package / 'auth.go')
            fingerprint = MODULE.materialize(seed, package / 'private_seed_generated.go')
            (root / 'go.mod').write_text('module fixture\n\ngo 1.26.0\n', encoding='utf-8')
            (root / 'main.go').write_text(
                'package main\n\n'
                'import (\n'
                '  "fmt"\n'
                '  auth "fixture/oixdnsauth"\n'
                ')\n\n'
                'func main() {\n'
                '  host, err := auth.Host("a.example.cloud-nodes.com", 1700000000)\n'
                '  if err != nil { panic(err) }\n'
                '  fmt.Println(auth.RuntimeSeedFingerprint())\n'
                '  fmt.Println(host)\n'
                '}\n',
                encoding='utf-8',
            )
            binary = root / ('fixture.exe' if os.name == 'nt' else 'fixture')
            subprocess.run(['go', 'build', '-o', str(binary), '.'], cwd=root, check=True)
            payload = binary.read_bytes()
            marker = (MODULE.FINGERPRINT_MARKER_PREFIX + fingerprint + MODULE.marker_checksum(fingerprint) + MODULE.FINGERPRINT_MARKER_SUFFIX).encode()
            self.assertEqual(payload.count(marker), 1)
            output = subprocess.check_output([str(binary)], text=True).splitlines()
            self.assertEqual(output[0], fingerprint)
            self.assertTrue(output[1].endswith('.a.example.cloud-nodes.com'))


if __name__ == '__main__':
    unittest.main()
