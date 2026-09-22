#!/usr/bin/env python3
"""Fail closed on stale/unattested native artifacts (no network or Gradle)."""
import argparse
import hashlib
import json
from pathlib import Path
import zipfile

ROOT = Path(__file__).resolve().parents[3]
LOCK = Path(__file__).with_name('native-baseline.json')
MARKERS = {
    'Juzi': ['#juzi', 'hello_pidun'],
    'X365': ['#x365'],
    'SL': ['Shanlian AnyTLS', '64 hexadecimal characters before #sl'],
    'Fastup': ['fastup'],
    'TunNet': ['libcore/tunnetcontrol'],
    'Oppa': ['libcore/protocol/oppa'],
}

def sha(data):
    return hashlib.sha256(data).hexdigest()

def verify(aar, apk=None, lock=LOCK):
    baseline = json.loads(Path(lock).read_text(encoding='utf-8'))
    if 'dependency_pins' in baseline:
        pins = (ROOT / 'buildScript/lib/core/get_source_env.sh').read_text(encoding='utf-8')
        for dependency, commit in baseline['dependency_pins'].items():
            variable = 'COMMIT_' + dependency.upper().replace('-', '_')
            if variable + '=\"' + commit + '\"' not in pins:
                raise ValueError('Source pin changed without native re-audit: ' + dependency)
    with zipfile.ZipFile(aar) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)):
            raise ValueError('Duplicate AAR entries')
        if sha(Path(aar).read_bytes()) != baseline['aar_sha256']:
            raise ValueError('Unattested AAR: rebuild/recover and explicitly re-audit native-baseline.json')
        if sha(archive.read('classes.jar')) != baseline['classes_jar_sha256']:
            raise ValueError('Java bridge identity mismatch')
        actual = sorted(n for n in names if n.endswith('/libgojni.so'))
        expected = sorted('jni/' + abi + '/libgojni.so' for abi in baseline['native_sha256'])
        if actual != expected:
            raise ValueError('Unexpected/missing ABI; never combine old cores with the recovered core')
        for abi, digest in baseline['native_sha256'].items():
            data = archive.read('jni/' + abi + '/libgojni.so')
            if sha(data) != digest:
                raise ValueError('Native identity mismatch: ' + abi)
            for protocol, markers in MARKERS.items():
                for marker in markers:
                    if marker.encode() not in data:
                        raise ValueError('Missing ' + protocol + ' binary evidence: ' + marker)
    if apk:
        with zipfile.ZipFile(apk) as archive:
            names = archive.namelist()
            cores = [n for n in names if n.startswith('lib/') and n.endswith('/libgojni.so')]
            if not cores or len(names) != len(set(names)):
                raise ValueError('Missing core or duplicate APK entries')
            for name in cores:
                abi = name.split('/')[1]
                if abi not in baseline['native_sha256'] or sha(archive.read(name)) != baseline['native_sha256'][abi]:
                    raise ValueError('APK/AAR native mismatch: ' + name)
    return {'status': 'PASS', 'aar_sha256': baseline['aar_sha256'], 'abis': list(baseline['native_sha256']), 'apk': str(apk) if apk else None, 'scope': 'binary identity/static protocol evidence only; no live network verification'}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--aar', type=Path, default=ROOT / 'app/libs/libcore.aar')
    parser.add_argument('--apk', type=Path)
    args = parser.parse_args()
    try:
        print(json.dumps(verify(args.aar, args.apk), indent=2))
    except (ValueError, KeyError, OSError, zipfile.BadZipFile) as error:
        parser.exit(1, 'NATIVE GATE FAILED: ' + str(error) + '\n')

if __name__ == '__main__':
    main()
