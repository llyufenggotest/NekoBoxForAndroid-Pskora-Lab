import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
import zipfile

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('native_gate', HERE / 'verify_native.py')
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)

class NativeGateTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.aar = self.root / 'core.aar'
        self.apk = self.root / 'app.apk'
        self.lock = self.root / 'lock.json'
        self.core = b'\x7fELF' + b'|'.join(m.encode() for ms in gate.MARKERS.values() for m in ms)
        self.write_aar(self.core)
        self.attest()

    def write_aar(self, core, extra=False):
        with zipfile.ZipFile(self.aar, 'w') as z:
            z.writestr('classes.jar', b'fixture-java-bridge')
            z.writestr('jni/arm64-v8a/libgojni.so', core)
            if extra:
                z.writestr('jni/x86/libgojni.so', b'stale core')

    def attest(self):
        # Synthetic negative-test fixture only, never production attestation.
        self.lock.write_text(json.dumps({'aar_sha256': gate.sha(self.aar.read_bytes()), 'classes_jar_sha256': gate.sha(b'fixture-java-bridge'), 'native_sha256': {'arm64-v8a': gate.sha(self.core)}}))

    def test_fixture_success(self):
        self.assertEqual(gate.verify(self.aar, lock=self.lock)['status'], 'PASS')

    def test_stale_aar_rejected(self):
        self.write_aar(b'old-no-juzi')
        with self.assertRaisesRegex(ValueError, 'Unattested AAR'):
            gate.verify(self.aar, lock=self.lock)

    def test_zip_metadata_drift_accepted_when_content_attested(self):
        b = json.loads(self.lock.read_text())
        b['aar_content_sha256'] = gate.canonical_zip_digest(self.aar.read_bytes())
        self.lock.write_text(json.dumps(b))
        with zipfile.ZipFile(self.aar) as source:
            members = [(item.filename, source.read(item.filename)) for item in source.infolist()]
        with zipfile.ZipFile(self.aar, 'w') as target:
            for name, payload in reversed(members):
                info = zipfile.ZipInfo(name, date_time=(2026, 9, 12, 0, 0, 0))
                target.writestr(info, payload)
        self.assertNotEqual(gate.sha(self.aar.read_bytes()), b['aar_sha256'])
        self.assertEqual(gate.verify(self.aar, lock=self.lock)['status'], 'PASS')

    def test_content_drift_rejected_with_canonical_attestation(self):
        b = json.loads(self.lock.read_text())
        b['aar_content_sha256'] = gate.canonical_zip_digest(self.aar.read_bytes())
        self.lock.write_text(json.dumps(b))
        self.write_aar(self.core + b'changed')
        with self.assertRaisesRegex(ValueError, 'Unattested AAR content'):
            gate.verify(self.aar, lock=self.lock)

    def test_marker_loss_rejected_even_when_hashes_updated(self):
        self.core = self.core.replace(b'hello_pidun', b'hello_missing')
        self.write_aar(self.core)
        self.attest()
        with self.assertRaisesRegex(ValueError, 'Missing Juzi'):
            gate.verify(self.aar, lock=self.lock)

    def test_all_custom_families_have_static_gate(self):
        for family in ('ViewTurbo', 'Blackstone', 'XHTTP'):
            self.assertIn(family, gate.MARKERS)

    def test_extra_stale_abi_rejected(self):
        self.write_aar(self.core, extra=True)
        self.attest()
        with self.assertRaisesRegex(ValueError, 'Unexpected/missing ABI'):
            gate.verify(self.aar, lock=self.lock)

    def test_apk_aar_mismatch_rejected(self):
        with zipfile.ZipFile(self.apk, 'w') as z:
            z.writestr('lib/arm64-v8a/libgojni.so', b'stale-apk')
        with self.assertRaisesRegex(ValueError, 'APK/AAR native mismatch'):
            gate.verify(self.aar, self.apk, self.lock)

    def test_core115_schema_downgrade_rejected(self):
        b = json.loads(self.lock.read_text())
        b.update(source_kind='local-core115-snapshot', schema=1)
        self.lock.write_text(json.dumps(b))
        with self.assertRaisesRegex(ValueError, 'bypass provenance'):
            gate.verify(self.aar, lock=self.lock)

    def test_apk_extra_abi_rejected(self):
        with zipfile.ZipFile(self.apk, 'w') as z:
            z.writestr('lib/arm64-v8a/libgojni.so', self.core)
            z.writestr('lib/x86/libgojni.so', self.core)
        with self.assertRaisesRegex(ValueError, 'APK ABI set'):
            gate.verify(self.aar, self.apk, self.lock)

    def test_matching_apk(self):
        with zipfile.ZipFile(self.apk, 'w') as z:
            z.writestr('lib/arm64-v8a/libgojni.so', self.core)
        self.assertEqual(gate.verify(self.aar, self.apk, self.lock)['status'], 'PASS')

if __name__ == '__main__':
    unittest.main()
