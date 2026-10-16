from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

PUSH_BRANCHES = (
    'maintenance/core115-validated250-stable',
    'feature/anytls-oix-snell',
    'upgrade/sing-box-1.15.0-alpha.12',
    'feature/blackstone-parity-integration-20261016',
)

REQUIRED = (
    "maintenance/core115-validated250-stable",
    "feature/anytls-oix-snell",
    "upgrade/sing-box-1.15.0-alpha.12",
    "workflow_dispatch:",
    "id: oix-input",
    "runtime-attestation.json",
    "verify_runtime_provenance.py",
    "--expected-seed-fingerprint",
    "--verify-oix",
    "--aapt",
    "runtime-provenance",
    "17d6af34f6bd6d7e1e428e0c652c8b54a46bda4f",
    "with_gvisor,with_quic,with_wireguard,with_utls,with_clash_api",
    "Verify exact runtime checkout after private build",
    "verify_runtime_clean.py",
    "$RUNNER_TEMP/runtime-attestation.json",
    "go-version: '1.26.5'",
    "java-version: '21'",
    "core115-source",
    "testOssDebugUnitTest",
    "testOssReleaseUnitTest",
    "verify_provenance.py --source-only",
    "assembleOssRelease",
    "arm64-v8a",
    "apksigner",
    "sha256sum",
)


def verify(root: Path, workflow: Path) -> list[str]:
    errors: list[str] = []
    if not workflow.is_file():
        return [f"missing workflow: {workflow}"]
    text = workflow.read_text(encoding="utf-8")
    branches = re.search(r'^  push:\n    branches:\n(?P<branches>(?:      - [^\n]+\n)+)    tags:', text, re.M)
    if branches is None or tuple(re.findall(r'^      - (.+)$', branches['branches'], re.M)) != PUSH_BRANCHES:
        errors.append('push branches must match the exact maintenance/Blackstone allowlist')
    for token in REQUIRED:
        if token not in text:
            errors.append(f"missing workflow contract token: {token}")
    if re.search(r"(?i)(password|token|secret)\s*[:=]\s*['\"]?[A-Za-z0-9_-]{16,}", text):
        errors.append("literal credential-like value in workflow")
    ignore = (root / ".gitignore").read_text(encoding="utf-8")
    for token in ("*.apk", "*.log", "evidence/", "hs_err_pid*", ".kotlin/"):
        if token not in ignore:
            errors.append(f"missing ignore rule: {token}")
    baseline = (root / "MAINTENANCE_BASELINE.md")
    if not baseline.is_file() or "d8cc10463bc14a99184c9c80bfc2d54232203d9d280cc5f3647ac2069d8e2765" not in baseline.read_text(encoding="utf-8"):
        errors.append("missing d8cc maintenance baseline")
    return errors


RACE_TESTS = {
    'github.com/sagernet/sing-box/protocol/xhttp': 'TestWireTCPAndUOTRealLoopback',
    'github.com/sagernet/sing-box/protocol/vless': 'TestBlackstoneProductionLoopbackTCPUDP',
    'github.com/sagernet/sing-vmess/vless': 'TestBlackstoneCurrentHAtFirstWriteAndGenerationIsolation',
}


def verify_race_log(path: Path) -> list[str]:
    # Supplement go test's exit code: package success with zero selected tests
    # is not protocol coverage. Logs are emitted by the real -race invocation.
    events = [json.loads(line) for line in path.read_text(encoding='utf-8').splitlines() if line.strip()]
    errors = []
    if any(event.get('Action') == 'fail' for event in events):
        errors.append('race log contains a failed test/package')
    for package, test in RACE_TESTS.items():
        if not any(e.get('Package') == package and e.get('Action') == 'pass' and 'Test' not in e for e in events):
            errors.append('missing race package pass: ' + package)
        if not any(e.get('Package') == package and e.get('Action') == 'pass' and e.get('Test') == test for e in events):
            errors.append('missing race protocol test pass: ' + test)
    return errors


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument('root', nargs='?', type=Path, default=Path.cwd())
    parser.add_argument('--race-log', type=Path)
    args = parser.parse_args()
    base = args.root.resolve()
    failures = (verify_race_log(args.race_log) if args.race_log
                else verify(base, base / '.github/workflows/maintenance-core115.yml'))
    if failures:
        print("\n".join(failures), file=sys.stderr)
        raise SystemExit(1)
    print('Blackstone race coverage: PASS' if args.race_log else 'maintenance CI contract: PASS')
