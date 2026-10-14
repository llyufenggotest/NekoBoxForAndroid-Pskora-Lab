import base64
import hashlib
import json
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
TARGET = ROOT / 'core115-source/sing-box/component/oixdnsauth/private_seed_generated.go'
FINGERPRINT_DOMAIN = b'nekoboxyf:oix-dns-auth:runtime-provenance:v1\0'
FINGERPRINT_MARKER_PREFIX = 'NEKOBOX-OIX-SEED-FINGERPRINT-v1:'
FINGERPRINT_MARKER_SUFFIX = ':END-NEKOBOX-OIX-SEED-FINGERPRINT-v1'
FINGERPRINT_CHECKSUM_DOMAIN = b'nekoboxyf:oix-dns-auth:marker-checksum:v1\0'
FINGERPRINT_CHECKSUM_SIZE = 16


def decode_seed(value: str) -> tuple[str, bytes]:
    value = value.strip()
    try:
        raw = base64.b64decode(value, validate=True)
    except Exception:
        raise ValueError('invalid Oix DNS-Auth seed') from None
    if len(raw) != 32:
        raise ValueError('invalid Oix DNS-Auth seed')
    return value, raw


def seed_fingerprint(raw: bytes) -> str:
    return hashlib.sha256(FINGERPRINT_DOMAIN + raw).hexdigest()


def marker_checksum(fingerprint: str) -> str:
    return hashlib.sha256(FINGERPRINT_CHECKSUM_DOMAIN + fingerprint.encode('ascii')).hexdigest()[:FINGERPRINT_CHECKSUM_SIZE]


def materialize(value: str, target: Path = TARGET) -> str:
    value, raw = decode_seed(value)
    fingerprint = seed_fingerprint(raw)
    marker = FINGERPRINT_MARKER_PREFIX + fingerprint + marker_checksum(fingerprint) + FINGERPRINT_MARKER_SUFFIX
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(
        '// Generated private build input. Never commit.\n'
        'package oixdnsauth\n\n'
        'func init() {\n'
        '\tBuildSeed = ' + json.dumps(value) + '\n'
        '\tBuildSeedFingerprint = ' + json.dumps(marker) + '\n'
        '}\n',
        encoding='utf-8',
    )
    return fingerprint


def main() -> None:
    fingerprint = materialize(os.environ.get('OIX_DNS_AUTH_SEED', ''), TARGET)
    github_output = os.environ.get('GITHUB_OUTPUT')
    if github_output:
        with Path(github_output).open('a', encoding='utf-8') as output:
            output.write('fingerprint=' + fingerprint + '\n')
    print('Oix DNS-Auth private build input prepared (value omitted)')


if __name__ == '__main__':
    main()
