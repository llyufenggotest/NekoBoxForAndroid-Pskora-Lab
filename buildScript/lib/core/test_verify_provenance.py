import copy
import importlib.util
import json
from pathlib import Path
import unittest
import zipfile
import tempfile

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('provenance', HERE / 'verify_provenance.py')
p = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p)
ROOT = HERE.parents[2]

class ProvenanceTest(unittest.TestCase):
    def setUp(self):
        self.baseline = json.loads((HERE / 'native-baseline.json').read_text())

    def test_real_snapshot(self):
        p.verify_provenance(self.baseline, ROOT)

    def test_changed_manifest_rejected(self):
        self.baseline['source_manifest_sha256'] = '0' * 64
        with self.assertRaisesRegex(ValueError, 'manifest identity'):
            p.verify_provenance(self.baseline, ROOT)

    def test_changed_snapshot_rejected(self):
        self.baseline['source_snapshot_sha256'] = '0' * 64
        with self.assertRaisesRegex(ValueError, 'snapshot identity'):
            p.verify_provenance(self.baseline, ROOT)

    def test_changed_dependency_rejected(self):
        self.baseline['source_dependencies']['sing-box']['go_mod_sha256'] = '0' * 64
        with self.assertRaisesRegex(ValueError, 'dependency mismatch'):
            p.verify_provenance(self.baseline, ROOT)

    def test_changed_base_commit_rejected(self):
        self.baseline['source_dependencies']['sing-box']['commit'] = '0' * 40
        with self.assertRaisesRegex(ValueError, 'Source identity'):
            p.verify_provenance(self.baseline, ROOT)

    def test_unapproved_jni_rejected(self):
        self.baseline['jni_evidence']['added_jni'].append('Java_unreviewed')
        with self.assertRaisesRegex(ValueError, 'JNI changes'):
            p.verify_provenance(self.baseline, ROOT)

    def test_unapproved_java_rejected(self):
        self.baseline['jni_evidence']['added_java'] = []
        with self.assertRaisesRegex(ValueError, 'Java additions'):
            p.verify_provenance(self.baseline, ROOT)

    def test_runtime_getter_allowlist_is_exact(self):
        getter = ['libcore/BoxInstance', 'methods', ['runtimeSelection', '(Ljava/lang/String;)Ljava/lang/String;', 256]]
        self.assertIn(getter, self.baseline['jni_evidence']['added_java'])
        self.baseline['jni_evidence']['added_java'].remove(getter)
        with self.assertRaisesRegex(ValueError, 'Java additions'):
            p.verify_provenance(self.baseline, ROOT)

    def test_runtime_getter_jni_required(self):
        self.baseline['jni_evidence']['added_jni'].remove('Java_libcore_BoxInstance_runtimeSelection')
        with self.assertRaisesRegex(ValueError, 'JNI changes'):
            p.verify_provenance(self.baseline, ROOT)

    def test_log_reconfigure_java_allowlist_exact(self):
        entry = ['libcore/Libcore', 'methods', ['nekoLogReconfigure', '(ZZ)V', 264]]
        self.assertIn(entry, self.baseline['jni_evidence']['added_java'])
        self.baseline['jni_evidence']['added_java'].remove(entry)
        with self.assertRaisesRegex(ValueError, 'Java additions'):
            p.verify_provenance(self.baseline, ROOT)

    def test_log_reconfigure_jni_required(self):
        self.baseline['jni_evidence']['added_jni'].remove('Java_libcore_Libcore_nekoLogReconfigure')
        with self.assertRaisesRegex(ValueError, 'JNI changes'):
            p.verify_provenance(self.baseline, ROOT)

    def test_real_build_and_changed_version_tags_arch(self):
        with zipfile.ZipFile(ROOT / 'app/libs/libcore.aar') as z:
            so = z.read('jni/arm64-v8a/libgojni.so')
        p.verify_build(so, self.baseline)
        for field in ('core_version', 'build_tags', 'build_goos', 'build_goarch'):
            with self.subTest(field=field):
                b = copy.deepcopy(self.baseline)
                b[field] = 'unattested'
                with self.assertRaisesRegex(ValueError, 'build evidence'):
                    p.verify_build(so, b)
        with self.assertRaisesRegex(ValueError, 'arm64 ELF'):
            p.verify_build(so[:18] + b'\x03\x00' + so[20:], self.baseline)

if __name__ == '__main__':
    unittest.main()
