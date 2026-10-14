import copy
import importlib.util
import json
from pathlib import Path
import shutil
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
        manifest = p.verify_snapshot(self.baseline, ROOT)
        self.assertEqual(len(manifest), self.baseline['source_file_count'])

    def test_source_attestation_can_be_verified_without_committed_aar(self):
        p.verify_source_attestation(self.baseline, ROOT)

    def test_built_source_head_tamper_rejected(self):
        self.baseline['built_source_head'] = '0' * 40
        with self.assertRaisesRegex(ValueError, 'built source HEAD'):
            p.verify_provenance(self.baseline, ROOT, verify_source_content=False, verify_attestation_head=False)

    def test_built_source_head_must_be_nonzero_ancestor(self):
        self.baseline['built_source_head'] = '0' * 40
        with self.assertRaisesRegex(ValueError, 'built source HEAD'):
            p.verify_provenance(self.baseline, ROOT, verify_source_content=False, verify_attestation_head=False)

    def test_source_tree_tamper_rejected(self):
        self.baseline['built_source_tree'] = '0' * 40
        with self.assertRaisesRegex(ValueError, 'source tree'):
            p.verify_provenance(self.baseline, ROOT, verify_source_content=False, verify_attestation_head=False)

    def test_aar_and_members_are_cross_bound(self):
        for field in ('aar_sha256', 'aar_content_sha256', 'classes_jar_sha256'):
            with self.subTest(field=field):
                baseline = copy.deepcopy(self.baseline)
                baseline[field] = '0' * 64
                with self.assertRaises(ValueError):
                    p.verify_provenance(baseline, ROOT, verify_source_content=False, verify_attestation_head=False)
        for abi in self.baseline['native_sha256']:
            baseline = copy.deepcopy(self.baseline)
            baseline['native_sha256'][abi] = '0' * 64
            with self.assertRaisesRegex(ValueError, 'native identity'):
                p.verify_provenance(baseline, ROOT, verify_source_content=False, verify_attestation_head=False)

    def test_snapshot_member_tamper_rejected_even_when_outer_hash_updated(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            snapshot_rel = Path(self.baseline['source_snapshot_path'])
            manifest_rel = Path(self.baseline['source_manifest_path'])
            target = root / snapshot_rel
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / snapshot_rel, target)
            target = root / manifest_rel
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / manifest_rel, target)
            with zipfile.ZipFile(root / snapshot_rel, 'a') as archive:
                first = archive.namelist()[0]
                archive.writestr(first, b'tampered')
            baseline = copy.deepcopy(self.baseline)
            baseline['source_snapshot_sha256'] = p.sha((root / snapshot_rel).read_bytes())
            with self.assertRaisesRegex(ValueError, 'snapshot inventory|snapshot member'):
                p.verify_snapshot(baseline, root)

    def test_changed_manifest_rejected(self):
        self.baseline['source_manifest_sha256'] = '0' * 64
        with self.assertRaisesRegex(ValueError, 'manifest SHA256'):
            p.verify_snapshot(self.baseline, ROOT)

    def test_changed_snapshot_rejected(self):
        self.baseline['source_snapshot_sha256'] = '0' * 64
        with self.assertRaisesRegex(ValueError, 'snapshot SHA256'):
            p.verify_snapshot(self.baseline, ROOT)

    def test_source_file_count_rejected(self):
        self.baseline['source_file_count'] += 1
        manifest = p.verify_snapshot(self.baseline, ROOT)
        self.assertNotEqual(self.baseline['source_file_count'], len(manifest))

    def test_changed_base_commit_rejected(self):
        self.baseline['built_source_head'] = 'f' * 40
        with self.assertRaises(ValueError):
            p.verify_provenance(self.baseline, ROOT, verify_source_content=False, verify_attestation_head=False)

    def test_unapproved_native_rejected(self):
        self.baseline['native_sha256']['arm64-v8a'] = 'f' * 64
        with self.assertRaisesRegex(ValueError, 'native identity'):
            p.verify_provenance(self.baseline, ROOT, verify_source_content=False, verify_attestation_head=False)

    def test_classes_jar_rejected(self):
        self.baseline['classes_jar_sha256'] = 'f' * 64
        with self.assertRaisesRegex(ValueError, 'classes_jar_sha256'):
            p.verify_provenance(self.baseline, ROOT, verify_source_content=False, verify_attestation_head=False)

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
