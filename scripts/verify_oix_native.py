"""Fail closed if any shipped native library lacks the private Oix seed.
Only reports artifact paths/ABI labels, never seed values. This verifies build
input inclusion, not live DNS authorization or network reachability.
"""
import argparse
import base64
import os
from pathlib import Path
import zipfile


def verify(data: bytes, seed_text: str) -> None:
    seed_text = seed_text.strip()
    try:
        seed = base64.b64decode(seed_text, validate=True)
    except Exception:
        raise ValueError('invalid private Oix seed input') from None
    if len(seed) != 32:
        raise ValueError('invalid private Oix seed input')
    if seed_text.encode('ascii') not in data:
        raise ValueError('native artifact does not contain the injected Oix seed')
    if b'EXPORTER-Dler-Snell-Identity-v2' not in data:
        raise ValueError('native artifact does not contain the Oix identity exporter')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--aar', type=Path)
    parser.add_argument('--binary', type=Path)
    args = parser.parse_args()
    if bool(args.aar) == bool(args.binary):
        parser.error('provide exactly one of --aar or --binary')
    seed = os.environ.get('OIX_DNS_AUTH_SEED', '')
    if args.binary:
        verify(args.binary.read_bytes(), seed)
        print('Oix native build-input artifact gate passed')
        return
    with zipfile.ZipFile(args.aar) as archive:
        libraries = [name for name in archive.namelist()
                     if name.startswith('jni/') and name.endswith('/libgojni.so')]
        if not libraries:
            raise ValueError('AAR has no native libgojni.so')
        for name in libraries:
            verify(archive.read(name), seed)
            print('Oix native build-input artifact gate passed: ' + name)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, zipfile.BadZipFile) as error:
        raise SystemExit(str(error)) from None
