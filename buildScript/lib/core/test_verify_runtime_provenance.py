import base64
import copy
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock
import zipfile

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location('runtime_provenance', HERE / 'verify_runtime_provenance.py')
RUNTIME = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RUNTIME)
ROOT = HERE.parents[2]
BASELINE = ROOT / 'buildScript/lib/core/native-baseline.json'
REAL_AAR = ROOT.parents[1] / 'NekoBoxForAndroid-Pskora-Lab-alpha12/core115-source/libcore/libcore.aar'
SEED = base64.b64encode(bytes(range(32))).decode()
FINGERPRINT = '1' * 64


class RuntimeProvenanceTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.aar = self.root / 'libcore.aar'
        shutil.copy2(REAL_AAR, self.aar)
        self.attestation = self.root / 'runtime-attestation.json'
        self.data = RUNTIME.create_runtime_attestation(
            ROOT,
            BASELINE,
            self.aar,
            self.attestation,
            FINGERPRINT,
            require_clean=False,
        )

    def tearDown(self):
        self.temp.cleanup()

    def verify(self, data=None, aar=None, apk=None):
        target = self.attestation
        target.write_text(json.dumps(data or self.data), encoding='utf-8')
        return RUNTIME.verify_runtime_provenance(
            ROOT,
            BASELINE,
            target,
            aar or self.aar,
            apk,
            verify_worktree=False,
            verify_oix=False,
            expected_seed_fingerprint=FINGERPRINT,
        )

    def test_create_and_verify_runtime_attestation(self):
        result = self.verify()
        self.assertEqual(result['status'], 'PASS')
        self.assertEqual(self.data['source_attestation_head'], subprocess.check_output(['git', '-C', str(ROOT), 'rev-parse', 'HEAD']).decode().strip())
        self.assertEqual(self.data['built_source_head'], json.loads(BASELINE.read_text())['built_source_head'])
        self.assertEqual(self.data['seed_fingerprint'], FINGERPRINT)

    def test_seed_and_fingerprint_tamper_fail(self):
        self.assertNotIn(SEED, self.attestation.read_text(encoding='utf-8'))
        for fingerprint in ('0' * 64, 'xyz', '2' * 64):
            with self.subTest(fingerprint=fingerprint):
                data = copy.deepcopy(self.data)
                data['seed_fingerprint'] = fingerprint
                with self.assertRaises(ValueError):
                    self.verify(data)

    def test_aar_so_and_classes_tamper_fail(self):
        mutations = {
            'AAR': lambda name, payload: payload + b'tamper' if name == 'AndroidManifest.xml' else payload,
            'SO': lambda name, payload: payload + b'tamper' if name.endswith('/libgojni.so') else payload,
            'classes': lambda name, payload: payload + b'tamper' if name == 'classes.jar' else payload,
        }
        for label, mutate in mutations.items():
            with self.subTest(label=label):
                target = self.root / (label + '.aar')
                with zipfile.ZipFile(self.aar) as source, zipfile.ZipFile(target, 'w') as output:
                    for info in source.infolist():
                        output.writestr(info, mutate(info.filename, source.read(info.filename)))
                with self.assertRaises(ValueError):
                    self.verify(aar=target)

    def test_source_head_tree_and_committed_attestation_tamper_fail(self):
        cases = {
            'built_source_head': 'f' * 40,
            'built_source_tree': 'f' * 40,
            'source_attestation_sha256': 'f' * 64,
        }
        for field, value in cases.items():
            with self.subTest(field=field):
                data = copy.deepcopy(self.data)
                data[field] = value
                with self.assertRaises(ValueError):
                    self.verify(data)

    def test_create_rejects_dirty_source(self):
        with mock.patch.object(RUNTIME, 'require_clean_exact', side_effect=ValueError('dirty')):
            with self.assertRaisesRegex(ValueError, 'dirty'):
                RUNTIME.create_runtime_attestation(
                    ROOT,
                    BASELINE,
                    self.aar,
                    self.attestation,
                    FINGERPRINT,
                )

    def test_apk_identity_matches_runtime_aar(self):
        apk = self.root / 'app.apk'
        tampered = self.root / 'tampered.apk'
        with zipfile.ZipFile(self.aar) as aar, zipfile.ZipFile(apk, 'w') as archive:
            archive.writestr('AndroidManifest.xml', b'fixture')
            archive.writestr('lib/arm64-v8a/libgojni.so', aar.read('jni/arm64-v8a/libgojni.so'))
        self.assertEqual(self.verify(apk=apk)['status'], 'PASS')
        with zipfile.ZipFile(apk) as source, zipfile.ZipFile(tampered, 'w') as archive:
            for info in source.infolist():
                payload = b'tamper' if info.filename == 'lib/arm64-v8a/libgojni.so' else source.read(info.filename)
                archive.writestr(info, payload)
        with self.assertRaises(ValueError):
            self.verify(apk=tampered)


if __name__ == '__main__':
    unittest.main()
