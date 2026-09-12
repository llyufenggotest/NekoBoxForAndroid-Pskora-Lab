from __future__ import annotations

import re
import sys
from pathlib import Path

REQUIRED = (
    "maintenance/core115-validated250-stable",
    "go-version: '1.26.5'",
    "java-version: '21'",
    "core115-source",
    "testOssDebugUnitTest",
    "testOssReleaseUnitTest",
    "verify_native.py",
    "verify_provenance.py",
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


if __name__ == "__main__":
    base = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else Path.cwd()
    failures = verify(base, base / ".github" / "workflows" / "maintenance-core115.yml")
    if failures:
        print("\n".join(failures), file=sys.stderr)
        raise SystemExit(1)
    print("maintenance CI contract: PASS")
