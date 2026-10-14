import base64
import hashlib
import json
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
TARGET = ROOT / 'core115-source/sing-box/component/oixdnsauth/private_seed_generated.go'
FINGERPRINT_DOMAIN = b'nekoboxyf:oix-dns-auth:runtime-provenance:v1\0'


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


def materialize(value: str, target: Path = TARGET) -> str:
    value, raw = decode_seed(value)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(
        '// Generated private build input. Never commit.\n'
        'package oixdnsauth\n\n'
        'func init() { BuildSeed = ' + json.dumps(value) + ' }\n',
        encoding='utf-8',
    )
    return seed_fingerprint(raw)


def main() -> None:
    fingerprint = materialize(os.environ.get('OIX_DNS_AUTH_SEED', ''), TARGET)
    github_output = os.environ.get('GITHUB_OUTPUT')
    if github_output:
        with Path(github_output).open('a', encoding='utf-8') as output:
            output.write('fingerprint=' + fingerprint + '\n')
    print('Oix DNS-Auth private build input prepared (value omitted)')


if __name__ == '__main__':
    main()
