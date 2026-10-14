import importlib.util
import os
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("verify_maintenance_ci", ROOT / "tools" / "verify_maintenance_ci.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class MaintenanceCIContractTest(unittest.TestCase):
    def test_workflow_contract(self):
        workflow = ROOT / ".github" / "workflows" / "maintenance-core115.yml"
        self.assertEqual([], MODULE.verify(ROOT, workflow))

    def test_workflow_uses_one_exact_clean_gate_and_no_filetree(self):
        workflow = (ROOT / ".github" / "workflows" / "maintenance-core115.yml").read_text(encoding="utf-8")
        gradle = (ROOT / "app" / "build.gradle.kts").read_text(encoding="utf-8")
        self.assertIn("python buildScript/lib/core/verify_runtime_clean.py", workflow)
        self.assertNotIn("allowed = ('app/libs/', 'out/', 'artifacts/')", workflow)
        self.assertNotIn("fileTree(\"libs\")", gradle)
        self.assertIn('implementation(files("libs/libcore.aar"))', gradle)

    def test_workflow_clean_gate_stage_boundaries(self):
        workflow = (ROOT / ".github/workflows/maintenance-core115.yml").read_text(encoding="utf-8")
        self.assertIn('PYTHONDONTWRITEBYTECODE: "1"', workflow)
        self.assertNotIn('chmod +x core115-source/libcore/', workflow)
        self.assertNotIn('chmod +x ./run', workflow)
        self.assertIn('bash ./init.sh && bash ./build.sh', workflow)
        self.assertIn('rm -rf core115-source/libcore/.build', workflow)
        self.assertIn('rm -f core115-source/libcore/libcore.aar', workflow)
        self.assertIn('path: ${{ runner.temp }}/runtime-provenance', workflow)
        self.assertIn('cp "$APK" "$RUNNER_TEMP/runtime-release.apk"', workflow)
        self.assertIn('rm -rf .gradle .kotlin app/build buildSrc/build buildSrc/.gradle', workflow)
        self.assertIn('rm -rf app/src/main/assets/sing-box', workflow)
        self.assertIn('rm -f local.properties', workflow)
        self.assertIn('bash buildScript/init/action/gradle.sh', workflow)
        self.assertIn('bash ./gradlew', workflow)
        self.assertIn('HEV_SRC: ${{ runner.temp }}/hev-socks5-tunnel', workflow)
        self.assertIn('rm -rf .hev-build app/src/main/jniLibs', workflow)
        self.assertIn('path: ${{ runner.temp }}/runtime-native-bundle', workflow)
        self.assertIn('cp "$RUNNER_TEMP/runtime-provenance/libcore.aar" app/libs/libcore.aar', workflow)
        self.assertNotIn('mkdir -p out', workflow)
        self.assertIn('path: ${{ runner.temp }}/runtime-output', workflow)

    def test_gradle_runtime_verifier_contract(self):
        workflow = (ROOT / '.github/workflows/maintenance-core115.yml').read_text(encoding='utf-8')
        gradle = (ROOT / 'app/build.gradle.kts').read_text(encoding='utf-8')
        self.assertIn('RUNTIME_PROVENANCE: ${{ runner.temp }}/runtime-attestation.json', workflow)
        self.assertIn('providers.environmentVariable("RUNTIME_PROVENANCE")', gradle)
        self.assertIn('if (runtimeProvenance.isPresent)', gradle)
        self.assertIn('else "buildScript/lib/core/verify_native.py"', gradle)
        self.assertIn('"--gradle-artifact-only", "--aar"', gradle)
        self.assertIn('nativeVerificationArgs()', gradle)
        self.assertIn('nativeVerificationArgs(apk)', gradle)
        line = next(line.strip() for line in workflow.splitlines() if line.strip().startswith("printf 'GOMOBILE_SOURCE="))
        with tempfile.TemporaryDirectory() as directory:
            envfile = Path(directory) / 'github-env'
            env = dict(os.environ, GITHUB_ENV=envfile.as_posix(), GOMOBILE_SOURCE='external/pinned')
            subprocess.run(['bash', '-euc', line], env=env, check=True)
            self.assertEqual(b'GOMOBILE_SOURCE=external/pinned\n', envfile.read_bytes())

    def test_clean_gate_real_git_status_matrix(self):
        gate = ROOT / "buildScript" / "lib" / "core" / "verify_runtime_clean.py"
        cases = {
            "only-libcore": (["app/libs/libcore.aar"], True),
            "evil-jar": (["app/libs/evil.jar"], False),
            "evil-aar": (["app/libs/evil.aar"], False),
            "source-go": (["source.go"], False),
            "script-sh": (["script.sh"], False),
            "ignored-other": (["app/libs/libcore.aar", "app/libs/evil.jar"], False),
            "out": (["out/release.apk"], False),
            "artifacts": (["artifacts/report.txt"], False),
            "ignored-source": (["app/libs/injected.go"], False),
            "ignored-script": (["app/libs/injected.sh"], False),
            "nested-jar": (["app/libs/nested/evil.jar"], False),
            "ignored-gradle-jar": ([".gradle/evil.jar"], False),
            "ignored-pyc": (["tools/__pycache__/cached.pyc"], False),
            "untracked-libcore": (["untracked/libcore.aar"], False),
        }
        for name, (paths, expected) in cases.items():
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / ".gitignore").write_text("/app/libs/\n/out/\n/artifacts/\n.gradle/\n**/__pycache__/\n", encoding="utf-8")
                import subprocess
                subprocess.run(["git", "init", "-q", str(root)], check=True)
                subprocess.run(["git", "-C", str(root), "config", "user.email", "test@example.invalid"], check=True)
                subprocess.run(["git", "-C", str(root), "config", "user.name", "test"], check=True)
                (root / "tracked.txt").write_text("clean\n", encoding="utf-8")
                subprocess.run(["git", "-C", str(root), "add", ".gitignore", "tracked.txt"], check=True)
                subprocess.run(["git", "-C", str(root), "commit", "-qm", "fixture"], check=True)
                for path in paths:
                    target = root / path
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_text("fixture\n", encoding="utf-8")
                result = subprocess.run([sys.executable, str(gate), "--root", str(root)], capture_output=True, text=True)
                self.assertEqual(expected, result.returncode == 0, result.stdout + result.stderr)

    def test_clean_gate_workflow_stage_simulation(self):
        """Execute workflow install/cleanup seams on a real disposable Git fixture.

        AAR/APK bytes here are sentinel fixtures, not claimed build artifacts.
        """
        workflow = (ROOT / ".github/workflows/maintenance-core115.yml").read_text(encoding="utf-8")
        gate = ROOT / "buildScript/lib/core/verify_runtime_clean.py"
        ignore = "/app/libs/\n/core115-source/libcore/.build/\n*.aar\n*.apk\n*.log\n**/__pycache__/\n.gradle/\n.kotlin/\nbuild/\n/app/src/main/assets/sing-box/\nlocal.properties\n"
        counts = {"pass": 0, "reject": 0}
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "source"
            runtime = base / "runner-temp"
            root.mkdir()
            runtime.mkdir()
            (root / ".gitignore").write_text(ignore, encoding="utf-8")
            (root / "tracked.txt").write_text("clean\n", encoding="utf-8")
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            subprocess.run(["git", "-C", str(root), "add", "."], check=True)
            subprocess.run(["git", "-C", str(root), "-c", "user.name=fixture", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture"], check=True)
            env = dict(os.environ, RUNNER_TEMP=runtime.as_posix(), PYTHONDONTWRITEBYTECODE="1")

            def write(path):
                target = root / path
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(b"explicit phase-test sentinel\n")

            def run_gate(expected):
                result = subprocess.run([sys.executable, str(gate), "--root", str(root)], env=env, capture_output=True, text=True)
                self.assertEqual(expected, result.returncode == 0, result.stdout + result.stderr)
                counts["pass" if expected else "reject"] += 1

            def shell(lines):
                subprocess.run(["bash", "-euc", "\n".join(lines)], cwd=root, env=env, check=True)

            def block(name):
                match = re.search(r"      - name: " + re.escape(name) + r"\n(?P<body>.*?)(?=      - |\Z)", workflow, re.S)
                self.assertIsNotNone(match)
                return [line[10:] for line in match.group("body").splitlines() if line.startswith("          ")]

            run_gate(True)  # before private input
            for path in ("app/libs/libcore.aar", "core115-source/libcore/libcore.aar", "core115-source/libcore/.build/generated/bridge.java", "core115-source/sing-box/component/oixdnsauth/private_seed_generated.go"):
                write(path)
            run_gate(False)  # post-build must clean seed and native intermediates
            shell([line for line in block("Pin and build native source") if line.startswith("rm -")])
            run_gate(True)
            # Real import does not leave ignored pyc after tests / runtime verify.
            subprocess.run([sys.executable, "-c", "import importlib.util; s=importlib.util.spec_from_file_location('fixture_gate',r'" + str(gate) + "');m=importlib.util.module_from_spec(s);s.loader.exec_module(m);m.verify(r'" + str(root) + "')"], env=env, check=True)
            run_gate(True)
            payload = runtime / "runtime-provenance/libcore.aar"
            payload.parent.mkdir(parents=True)
            payload.write_bytes(b"explicit AAR install sentinel\n")
            (runtime / "runtime-provenance/runtime-attestation.json").write_text("{}", encoding="utf-8")
            install = block("Install verified runtime AAR")
            shell(install[:install.index("python buildScript/lib/core/verify_runtime_provenance.py \\")])
            run_gate(True)
            for path in (".gradle/cache.bin", ".kotlin/cache.bin", "app/build/outputs/apk/oss/release/fixture-arm64-v8a.apk", "buildSrc/build/generated.bin", "buildSrc/.gradle/cache.bin", ".hev-build/obj/native.o", "app/src/main/jniLibs/arm64-v8a/libhev-socks5-tunnel.so", "app/src/main/assets/sing-box/geoip.db.xz", "local.properties"):
                write(path)
            run_gate(False)
            verify = block("Verify target APK against runtime provenance")
            shell(verify[:verify.index("python buildScript/lib/core/verify_runtime_provenance.py \\")])
            self.assertEqual(b"explicit phase-test sentinel\n", (runtime / "runtime-release.apk").read_bytes())
            run_gate(True)
            # Cleanup does not hide a foreign packaging input or tracked edit.
            write("app/libs/evil.jar")
            run_gate(False)
            (root / "app/libs/evil.jar").unlink()
            write("tracked.txt")
            run_gate(False)
        self.assertEqual({"pass": 5, "reject": 4}, counts)
        print("workflow-stage clean-gate checks: 5 pass / 4 expected rejects")

    def test_clean_gate_rejects_staged_and_unstaged_tracked_changes(self):
        gate = ROOT / "buildScript" / "lib" / "core" / "verify_runtime_clean.py"
        import subprocess
        for staged, filename in ((False, "tracked.aar"), (True, "tracked.aar"), (False, "libcore.aar"), (True, "libcore.aar")):
            with self.subTest(staged=staged, filename=filename), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                subprocess.run(["git", "init", "-q", str(root)], check=True)
                subprocess.run(["git", "-C", str(root), "config", "user.email", "test@example.invalid"], check=True)
                subprocess.run(["git", "-C", str(root), "config", "user.name", "test"], check=True)
                tracked = root / "app" / "libs" / filename
                tracked.parent.mkdir(parents=True)
                tracked.write_text("clean\n", encoding="utf-8")
                subprocess.run(["git", "-C", str(root), "add", "."], check=True)
                subprocess.run(["git", "-C", str(root), "commit", "-qm", "fixture"], check=True)
                tracked.write_text("dirty\n", encoding="utf-8")
                if staged:
                    subprocess.run(["git", "-C", str(root), "add", "app/libs/" + filename], check=True)
                result = subprocess.run([sys.executable, str(gate), "--root", str(root)], capture_output=True, text=True)
                self.assertNotEqual(0, result.returncode, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
