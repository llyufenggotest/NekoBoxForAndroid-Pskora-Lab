import base64
import json
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
TARGET = ROOT / 'core115-source/sing-box/component/oixdnsauth/private_seed_generated.go'

def materialize(value: str, target: Path = TARGET):
    value = value.strip()
    try: raw = base64.b64decode(value, validate=True)
    except Exception: raise ValueError('invalid Oix DNS-Auth seed') from None
    if len(raw) != 32: raise ValueError('invalid Oix DNS-Auth seed')
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text('// Generated private build input. Never commit.\npackage oixdnsauth\n\nfunc init() { BuildSeed = '+json.dumps(value)+' }\n', encoding='utf-8')

if __name__ == '__main__':
    materialize(os.environ.get('OIX_DNS_AUTH_SEED', ''))
    print('Oix DNS-Auth private build input prepared (value omitted)')
