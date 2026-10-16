import copy
import importlib.util
import json
from pathlib import Path
import struct
import shutil
import subprocess
import sys
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
        self.temp = tempfile.TemporaryDirectory()
        self.fixture_aar = Path(self.temp.name) / 'libcore.aar'
        header = bytearray(64)
        header[:4] = b'\x7fELF'
        header[18:20] = struct.pack('<H', 183)
        native = bytes(header) + '\0'.join((
            self.baseline['core_version'],
            '-tags=' + self.baseline['build_tags'],
            'GOOS=' + self.baseline['build_goos'],
            'GOARCH=' + self.baseline['build_goarch'],
            '-buildmode=c-shared',
            'CGO_ENABLED=1',
        )).encode()
        with zipfile.ZipFile(self.fixture_aar, 'w') as archive:
            archive.writestr('AndroidManifest.xml', b'fixture')
            archive.writestr('classes.jar', b'fixture-classes')
            archive.writestr('jni/arm64-v8a/libgojni.so', native)
        identity = p.artifact_identity(self.fixture_aar)
        self.baseline.update(identity)
        self.baseline['aar_path'] = str(self.fixture_aar)

    def tearDown(self):
        self.temp.cleanup()

    def test_attestation_bytes_survive_real_git_autocrlf_checkouts(self):
        # Invoke the actual CLI in separate processes: Windows write_text newline
        # translation must not be hidden by mocks or in-process JSON reads.
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            source = base / 'source'
            source.mkdir()
            core = Path('buildScript/lib/core')
            (source / core).mkdir(parents=True)
            shutil.copy2(HERE / 'verify_provenance.py', source / core / 'verify_provenance.py')
            (source / 'core115-source').mkdir()
            (source / 'core115-source/example.go').write_bytes(b'package example\n')
            (source / '.gitignore').write_bytes(b'app/libs/\n')
            (source / '.gitattributes').write_bytes((ROOT / '.gitattributes').read_bytes())

            def command(*args, cwd=source, check=True):
                return subprocess.run(args, cwd=cwd, check=check, capture_output=True, text=True)

            command('git', 'init')
            command('git', 'config', 'core.autocrlf', 'true')
            command('git', 'config', 'user.name', 'Provenance regression')
            command('git', 'config', 'user.email', 'provenance@example.invalid')
            command('git', 'add', '.')
            command('git', 'commit', '-m', 'source')
            source_head = command('git', 'rev-parse', 'HEAD').stdout.strip()
            aar = source / 'app/libs/libcore.aar'
            aar.parent.mkdir(parents=True)
            shutil.copy2(self.fixture_aar, aar)
            manifest = core / 'core115-source.manifest.json'
            baseline = core / 'native-baseline.json'
            snapshot = core / 'core115-source.snapshot.tar'
            command(sys.executable, str(source / core / 'verify_provenance.py'), '--create',
                    '--root', str(source), '--source-head', source_head,
                    '--aar', str(aar), '--snapshot', str(source / snapshot),
                    '--manifest', str(source / manifest), '--output', str(source / baseline))
            generated = {path: (source / path).read_bytes() for path in (manifest, baseline)}
            command('git', 'add', str(core))
            command('git', 'commit', '-m', 'direct-child attestation')
            self.assertEqual(command('git', 'rev-parse', 'HEAD^').stdout.strip(), source_head)
            for autocrlf in ('false', 'true'):
                with self.subTest(autocrlf=autocrlf):
                    checkout = base / ('checkout-' + autocrlf)
                    command('git', 'clone', '--no-hardlinks', '-c', 'core.autocrlf=' + autocrlf,
                            str(source), str(checkout))
                    verified = command(sys.executable, str(checkout / core / 'verify_provenance.py'),
                                       '--source-only', '--root', str(checkout), cwd=checkout)
                    self.assertIn('SOURCE ATTESTATION PASS', verified.stdout)
                    loaded = json.loads((checkout / baseline).read_bytes())
                    self.assertEqual(p.sha((checkout / manifest).read_bytes()), loaded['source_manifest_sha256'])
                    for path, expected in generated.items():
                        self.assertEqual((checkout / path).read_bytes(), expected)
                        blob = subprocess.run(['git', 'show', 'HEAD:' + path.as_posix()],
                                              cwd=checkout, check=True, capture_output=True).stdout
                        self.assertEqual(blob, expected)
                    # A byte mutation must still fail the strict SHA gate.
                    (checkout / manifest).write_bytes(generated[manifest] + b' ')
                    rejected = command(sys.executable, str(checkout / core / 'verify_provenance.py'),
                                       '--source-only', '--root', str(checkout), cwd=checkout, check=False)
                    self.assertNotEqual(rejected.returncode, 0)
                    self.assertIn('Source manifest identity mismatch', rejected.stderr)
                    for path, expected in generated.items():
                        self.assertNotIn(b'\r', expected, str(path) + ' must be canonical LF')

    def test_real_snapshot(self):
        manifest = p.verify_snapshot(self.baseline, ROOT)
        self.assertEqual(len(manifest), self.baseline['source_file_count'])

    def test_source_attestation_can_be_verified_without_committed_aar(self):
        baseline = json.loads((HERE / 'native-baseline.json').read_text())
        p.verify_source_attestation(baseline, ROOT, verify_attestation_head=False)

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
        with zipfile.ZipFile(self.fixture_aar) as z:
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

class DirectChildIntegrityTest(unittest.TestCase):
    setUp = ProvenanceTest.setUp
    tearDown = ProvenanceTest.tearDown

    # Use actual Git commits and snapshot verification, not mocked git output.
    def fixture(self, extra=None, reuse_snapshot=False):
        root = Path(self.temp.name) / 'source'
        root.mkdir()
        def git(*args):
            return p.git(root, *args)
        git('init', '-q')
        git('config', 'user.name', 'Provenance fixture')
        git('config', 'user.email', 'fixture@example.invalid')
        git('config', 'core.autocrlf', 'false')
        for name in ('core115-source/example.go', 'app/example.kt',
                     '.github/workflows/example.yml', 'extra.txt'):
            target = root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(b'original\n')
        git('add', '.')
        git('commit', '-qm', 'synthetic initial source')
        core = root / 'buildScript/lib/core'
        snapshot = core / 'core115-source.snapshot.tar'
        manifest = core / 'core115-source.manifest.json'
        baseline_path = core / 'native-baseline.json'
        if reuse_snapshot:
            p.write_snapshot(root, git('rev-parse', 'HEAD'), snapshot, manifest)
            baseline_path.write_text('{}\n', encoding='utf-8')
            git('add', '.')
            git('commit', '-qm', 'synthetic source with unchanged snapshot')
        source = git('rev-parse', 'HEAD')
        files = p.write_snapshot(root, source, snapshot, manifest)
        baseline = dict(self.baseline, built_source_head=source,
                        built_source_tree=git('rev-parse', source + '^{tree}'),
                        source_manifest_path=manifest.relative_to(root).as_posix(),
                        source_snapshot_path=snapshot.relative_to(root).as_posix(),
                        source_manifest_sha256=p.sha(manifest.read_bytes()),
                        source_snapshot_sha256=p.sha(snapshot.read_bytes()),
                        source_file_count=len(files))
        baseline_path.write_text(json.dumps(baseline), encoding='utf-8')
        if extra:
            (root / extra).write_bytes(b'changed in attestation child\n')
        git('add', '.')
        git('commit', '-qm', 'synthetic direct-child attestation')
        self.assertEqual(source, git('rev-parse', 'HEAD^'))
        return root, baseline

    def reject_extra(self, path):
        root, baseline = self.fixture(path)
        # Both entry paths must enforce the child boundary, including the mode
        # that skips expensive snapshot content verification.
        for source_content in (True, False):
            with self.subTest(path=path, source_content=source_content):
                with self.assertRaisesRegex(ValueError, 'attestation paths|core subtree'):
                    p.verify_provenance(baseline, root, verify_source_content=source_content)

    def test_child_core_change_rejected(self):
        self.reject_extra('core115-source/example.go')

    def test_child_app_change_rejected(self):
        self.reject_extra('app/example.kt')

    def test_child_workflow_change_rejected(self):
        self.reject_extra('.github/workflows/example.yml')

    def test_child_extra_file_change_rejected(self):
        self.reject_extra('extra.txt')

    def test_child_three_attestation_paths_pass(self):
        root, baseline = self.fixture()
        self.assertEqual({
            'buildScript/lib/core/native-baseline.json',
            'buildScript/lib/core/core115-source.snapshot.tar',
            'buildScript/lib/core/core115-source.manifest.json',
        }, set(p.git(root, 'diff', '--name-only', 'HEAD^', 'HEAD').splitlines()))
        p.verify_source_attestation(baseline, root)
        p.verify_provenance(baseline, root, verify_source_content=False)

    def test_child_baseline_only_with_identical_snapshot_passes(self):
        root, baseline = self.fixture(reuse_snapshot=True)
        self.assertEqual(['buildScript/lib/core/native-baseline.json'],
                         p.git(root, 'diff', '--name-only', 'HEAD^', 'HEAD').splitlines())
        p.verify_source_attestation(baseline, root)
        p.verify_provenance(baseline, root, verify_source_content=False)


if __name__ == '__main__':
    unittest.main()
