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

    print("alpha12 dispatcher contract: PASS")


if __name__ == "__main__":
    try:
        main()
    except AssertionError as error:
        print(f"alpha12 dispatcher contract: FAIL: {error}", file=sys.stderr)
        raise SystemExit(1)
