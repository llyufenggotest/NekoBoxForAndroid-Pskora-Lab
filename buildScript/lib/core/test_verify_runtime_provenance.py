import copy
import importlib.util
import json
import os
from pathlib import Path
import struct
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
FINGERPRINT = '1' * 64
OTHER_FINGERPRINT = '2' * 64


def native_fixture(fingerprint=FINGERPRINT):
    header = bytearray(64)
    header[:4] = b'\x7fELF'
    header[18:20] = struct.pack('<H', 183)
    evidence = '\0'.join((
        '1.15.0-alpha.12-private-lab',
        '-tags=' + RUNTIME.EXPECTED_BUILD_TAGS,
        'GOOS=android',
        'GOARCH=arm64',
        '-buildmode=c-shared',
        'CGO_ENABLED=1',
        'go' + RUNTIME.EXPECTED_GO_VERSION,
        'EXPORTER-Dler-Snell-Identity-v2',
    )).encode()
    return bytes(header) + evidence + b'\0' + RUNTIME.FINGERPRINT_MARKER + fingerprint.encode() + b'\0'


def write_fixture_aar(path, fingerprint=FINGERPRINT):
    with zipfile.ZipFile(path, 'w') as archive:
        archive.writestr('AndroidManifest.xml', b'fixture-manifest')
        archive.writestr('classes.jar', b'fixture-java-bridge')
        archive.writestr('jni/arm64-v8a/libgojni.so', native_fixture(fingerprint))


def toolchain_fixture():
    return {
        'go_version': RUNTIME.EXPECTED_GO_VERSION,
        'go_version_output': 'go version go' + RUNTIME.EXPECTED_GO_VERSION + ' linux/amd64',
        'java_major': RUNTIME.EXPECTED_JAVA_MAJOR,
        'java_version_output': 'openjdk version "' + RUNTIME.EXPECTED_JAVA_MAJOR + '.0.2" 2026-01-20',
        'gomobile_commit': RUNTIME.EXPECTED_GOMOBILE_COMMIT,
        'build_tags': RUNTIME.EXPECTED_BUILD_TAGS,
    }


class RuntimeProvenanceTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.aar = self.root / 'libcore.aar'
        write_fixture_aar(self.aar)
        self.attestation = self.root / 'runtime-attestation.json'
        with mock.patch.object(RUNTIME, 'verify_source_contract'), mock.patch.object(
            RUNTIME, 'collect_toolchain', return_value=toolchain_fixture()
        ):
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

    def verify(self, data=None, aar=None, apk=None, fingerprint=FINGERPRINT):
        target = self.attestation
        target.write_text(json.dumps(data or self.data), encoding='utf-8')
        with mock.patch.object(RUNTIME, 'verify_source_contract'), mock.patch.object(
            RUNTIME, 'collect_toolchain', return_value=toolchain_fixture()
        ):
            return RUNTIME.verify_runtime_provenance(
                ROOT,
                BASELINE,
                target,
                aar or self.aar,
                apk,
                verify_worktree=False,
                verify_oix=False,
                expected_seed_fingerprint=fingerprint,
            )

    def test_fixture_is_checkout_local_and_synthetic(self):
        self.assertTrue(self.aar.is_relative_to(self.root))
        self.assertNotIn('NekoBoxForAndroid-Pskora-Lab-alpha12', str(self.aar))

    def test_create_and_verify_runtime_attestation(self):
        result = self.verify()
        self.assertEqual(result['status'], 'PASS')
        self.assertEqual(self.data['source_attestation_head'], subprocess.check_output(['git', '-C', str(ROOT), 'rev-parse', 'HEAD']).decode().strip())
        self.assertEqual(self.data['built_source_head'], json.loads(BASELINE.read_text())['built_source_head'])
        self.assertEqual(self.data['seed_fingerprint'], FINGERPRINT)

    def test_seed_and_fingerprint_tamper_fail_even_when_expected_changes(self):
        for fingerprint in ('0' * 64, 'xyz', OTHER_FINGERPRINT):
            with self.subTest(fingerprint=fingerprint):
                data = copy.deepcopy(self.data)
                data['seed_fingerprint'] = fingerprint
                with self.assertRaises(ValueError):
                    self.verify(data, fingerprint=fingerprint if len(fingerprint) == 64 else FINGERPRINT)

    def test_same_aar_rejected_for_arbitrary_fingerprint(self):
        data = copy.deepcopy(self.data)
        data['seed_fingerprint'] = OTHER_FINGERPRINT
        with self.assertRaisesRegex(ValueError, 'binary fingerprint'):
            self.verify(data, fingerprint=OTHER_FINGERPRINT)

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

    def test_toolchain_is_collected_and_fixed_not_caller_forged(self):
        self.assertNotIn('toolchain', RUNTIME.create_runtime_attestation.__code__.co_varnames[:RUNTIME.create_runtime_attestation.__code__.co_argcount])
        for field, value in (
            ('go_version', '1.26.4'),
            ('java_major', '17'),
            ('gomobile_commit', 'f' * 40),
            ('build_tags', 'with_utls'),
            ('go_version_output', 'forged'),
            ('java_version_output', 'forged'),
        ):
            forged = toolchain_fixture()
            forged[field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                RUNTIME.validate_toolchain(forged)

    def test_attested_toolchain_output_must_parse_and_match_fixed_contract(self):
        forged = copy.deepcopy(self.data)
        forged['build_toolchain']['go_version_output'] = 'go version go1.26.5 forged/amd64'
        with self.assertRaises(ValueError):
            self.verify(forged)

    def test_generated_seed_file_must_be_removed_before_clean_check(self):
        generated = ROOT / RUNTIME.GENERATED_SEED_PATH
        with mock.patch.object(Path, 'exists', autospec=True, side_effect=lambda path: path == generated):
            with self.assertRaisesRegex(ValueError, 'generated private seed'):
                RUNTIME.require_clean_exact(ROOT, json.loads(BASELINE.read_text()), RUNTIME.load_provenance())

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
