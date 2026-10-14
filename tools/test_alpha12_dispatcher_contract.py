#!/usr/bin/env python
"""Contract tests for the privileged Alpha12 build dispatcher."""
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = ROOT / ".github/workflows/alpha12-integration-build-dispatcher.yml"
EXPECTED_BRANCH = "upgrade/sing-box-1.15.0-alpha.12"


def require(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def run_blocks(text: str) -> list[str]:
    """Read literal run blocks without depending on a YAML installation."""
    return re.findall(r"^        run: \|\n((?:^          .*\n|^\n)+)", text, re.MULTILINE)


def require_order(text: str, *tokens: str) -> None:
    cursor = 0
    for token in tokens:
        position = text.find(token, cursor)
        require(position >= 0, f"missing or out-of-order strict gate token: {token}")
        cursor = position + len(token)


def verify_strict_gates(text: str) -> None:
    require('PYTHONDONTWRITEBYTECODE: "1"' in text, "Python imports may dirty the checkout")
    require("chmod " not in text, "tracked executable modes must not be changed")
    require(re.search(r"\bgit\s+(?:clean|reset)\b", text) is None, "broad checkout cleanup is forbidden")
    require("permissions:\n  contents: read\n" in text, "dispatcher must stay read-only")
    for action in re.findall(r"uses: (\S+)", text):
        require(re.fullmatch(r"actions/[\w-]+@[0-9a-f]{40}", action) is not None, f"unpinned action: {action}")

    native, android = text.split("\n  android:\n", 1)
    clean = "python buildScript/lib/core/verify_runtime_clean.py"
    require_order(native, clean, "run: python scripts/prepare_oix_dns_auth.py",
                  "bash ./init.sh && bash ./build.sh",
                  'test -z "$(git ls-files --',
                  "rm -f core115-source/sing-box/component/oixdnsauth/private_seed_generated.go",
                  "rm -rf core115-source/libcore/.build", "rm -f core115-source/libcore/libcore.aar",
                  clean, "--create", "--verify-oix", clean, "- name: Upload native AAR")
    require('export GOMOBILE_SOURCE="$RUNNER_TEMP/gomobile"' in native, "gomobile must stay external")
    require('tee "$RUNNER_TEMP/aar-files.txt"' in native, "AAR listing must stay external")
    require('HEV_SRC: ${{ runner.temp }}/hev-socks5-tunnel' in android, "HEV source must stay external")
    require_order(android, clean, "- name: Install and verify runtime AAR", clean,
                  "--runtime-attestation", "bash buildScript/init/action/gradle.sh",
                  "bash ./gradlew --no-daemon --refresh-dependencies",
                  "bash ./gradlew --no-daemon app:assembleOssRelease",
                  'cp "$APK" "$RUNNER_TEMP/runtime-release.apk"',
                  'APK="$RUNNER_TEMP/runtime-release.apk"',
                  "bash ./gradlew --stop", 'test -z "$(git ls-files --',
                  "rm -rf .gradle .kotlin app/build buildSrc/build buildSrc/.gradle",
                  "rm -rf .hev-build app/src/main/jniLibs",
                  "rm -rf app/src/main/assets/sing-box", "rm -f local.properties",
                  clean, "--apk", 'cp "$APK" "$ANDROID_APK_ARTIFACT"',
                  clean, "python tools/verify_maintenance_ci.py .", "- name: Upload verified unsigned Alpha12 APK")
    for name in ("gradle-unit-tests.log", "gradle-release-build.log"):
        require(f'tee "$RUNNER_TEMP/{name}"' in android, f"{name} must stay external")
        require(f"${{{{ runner.temp }}}}/{name}" in android, f"{name} upload must use external path")

    # Each checkout deletion must first prove the entire removal set is untracked.
    for block in run_blocks(text):
        deletions = re.findall(r"^          rm -(?:f|rf) (?![\"$])(.+)$", block, re.MULTILINE)
        if not deletions:
            continue
        guard = re.search(r'test -z "\$\(git ls-files -- (.*?)\)"', block, re.DOTALL)
        require(guard is not None, "checkout deletion has no tracked-file guard")
        guarded = set(guard.group(1).replace("\\\n", " ").split())
        for deletion in deletions:
            require(set(deletion.split()).issubset(guarded), f"unguarded cleanup paths: {deletion}")
        first_delete = re.search(r'^          rm -(?:f|rf) (?!["$])', block, re.MULTILINE)
        require(block.index("git ls-files --") < first_delete.start(), "cleanup precedes tracked-file guard")


def verify_strict_gate_regressions(text: str) -> None:
    """Prove the contract rejects representative dirty-checkout/order regressions."""
    cases = {
        "bytecode": text.replace('PYTHONDONTWRITEBYTECODE: "1"', 'PYTHONDONTWRITEBYTECODE: "0"'),
        "HEV source": text.replace('HEV_SRC: ${{ runner.temp }}/hev-socks5-tunnel', 'HEV_SRC: hev-socks5-tunnel'),
        "tracked cleanup guard": text.replace('git ls-files --', 'git status --'),
        "APK staging": text.replace('          cp "$APK" "$RUNNER_TEMP/runtime-release.apk"\n', ''),
        "post-clean gate": text.replace(
            '          python buildScript/lib/core/verify_runtime_clean.py\n'
            '          python buildScript/lib/core/verify_runtime_provenance.py ',
            '          python buildScript/lib/core/verify_runtime_provenance.py ',
        ),
        "Gradle log": text.replace('tee "$RUNNER_TEMP/gradle-unit-tests.log"', 'tee gradle-unit-tests.log'),
        "native cleanup": text.replace('          rm -rf core115-source/libcore/.build\n', ''),
        "stop before stage": text.replace('          bash ./gradlew --stop\n', '').replace(
            '          cp "$APK" "$RUNNER_TEMP/runtime-release.apk"\n',
            '          bash ./gradlew --stop\n          cp "$APK" "$RUNNER_TEMP/runtime-release.apk"\n',
        ),
    }
    for name, mutant in cases.items():
        require(mutant != text, f"ineffective regression test: {name}")
        try:
            verify_strict_gates(mutant)
        except AssertionError:
            continue
        raise AssertionError(f"strict gate regression was accepted: {name}")


def main() -> None:
    text = WORKFLOW.read_text(encoding="utf-8")

    require("expected_sha:" in text, "missing required expected_sha input")
    require(
        re.search(r"expected_sha:\s*\n(?:\s+.*\n)*?\s+required:\s*true\b", text) is not None,
        "expected_sha input is not required",
    )
    require("EXPECTED_SHA: ${{ inputs.expected_sha }}" in text, "expected_sha is not passed to the resolver")
    require("if (targetRef !== allowedRef)" in text, "resolver does not reject the wrong branch")
    require("startsWith" not in text, "prefix branch allowlist is forbidden")
    require(
        "if (!/^[0-9a-f]{40}$/.test(expectedSha))" in text,
        "resolver does not reject invalid expected_sha input",
    )
    require(
        "if (sha !== expectedSha)" in text,
        "resolver does not reject a branch SHA different from expected_sha",
    )
    require(f"const allowedRef = '{EXPECTED_BRANCH}';" in text, "exact Alpha12 branch is not pinned")
    require("RUNTIME_PROVENANCE=$RUNNER_TEMP/runtime-attestation.json" in text, "runtime JSON is not externalized")
    require("NATIVE_BUNDLE_ROOT=$RUNNER_TEMP/alpha12-native-bundle" in text, "native artifact staging is not externalized")
    require("ANDROID_OUTPUT_ROOT=$RUNNER_TEMP/alpha12-android-output" in text, "APK artifact staging is not externalized")
    require("path: ${{ env.NATIVE_AAR_ARTIFACT }}" in text, "native AAR upload is not an explicit external file")
    require("path: ${{ env.NATIVE_RUNTIME_ARTIFACT }}" in text, "runtime JSON upload is not an explicit external file")
    require("path: ${{ env.ANDROID_APK_ARTIFACT }}" in text, "APK upload is not an explicit external file")
    require("path: ${{ env.ANDROID_SHA256_ARTIFACT }}" in text, "APK checksum upload is not an explicit external file")
    require("actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09" in text, "checkout action is not pinned")
    require("actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9" in text, "upload action is not pinned")
    require("actions/download-artifact@634f93cb2916e3fdff6788551b99b062d0335ce0" in text, "download action is not pinned")
    require("Target checkout scripts are trusted only after exact branch and SHA validation" in text, "trusted target-script boundary is undocumented")

    verify_strict_gates(text)
    verify_strict_gate_regressions(text)
    print("alpha12 dispatcher contract: PASS (strict clean gate order; 8 regression cases)")


if __name__ == "__main__":
    try:
        main()
    except AssertionError as error:
        print(f"alpha12 dispatcher contract: FAIL: {error}", file=sys.stderr)
        raise SystemExit(1)
