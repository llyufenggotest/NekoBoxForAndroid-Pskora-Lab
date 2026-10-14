#!/usr/bin/env python3
"""Create and verify private CI runtime native provenance."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import zipfile

HERE = Path(__file__).resolve().parent
GENERATOR_NAME = 'verify_runtime_provenance.py'
GENERATOR_VERSION = 1


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def load_provenance():
    return load_module('committed_provenance', HERE / 'verify_provenance.py')


def load_oix_verifier(root):
    return load_module('verify_oix_native', Path(root) / 'scripts/verify_oix_native.py')


def require_digest(value, label, size=64):
    if not isinstance(value, str) or len(value) != size or set(value) - set('0123456789abcdef') or set(value) == {'0'}:
        raise ValueError(label + ' must be a non-zero lowercase hexadecimal digest')


def require_clean_exact(root, baseline, provenance):
    status = provenance.git(root, 'status', '--porcelain', '--untracked-files=all')
    allowed_ignored = {baseline['aar_path']}
    unexpected = []
    for line in status.splitlines():
        path = line[3:].replace('\\', '/')
        if path not in allowed_ignored:
            unexpected.append(line)
    if unexpected:
        raise ValueError('Runtime provenance requires a clean worktree')
    provenance.verify_source_attestation(baseline, root)


def aar_members(aar_path, provenance):
    with zipfile.ZipFile(aar_path) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)):
            raise ValueError('Duplicate AAR entries')
        return {name: provenance.sha(archive.read(name)) for name in sorted(names)}


def application_identity(root):
    properties = {}
    for line in (Path(root) / 'nb4a.properties').read_text(encoding='utf-8').splitlines():
        if '=' in line:
            key, value = line.split('=', 1)
            properties[key] = value
    return {
        'application_id': properties['PACKAGE_NAME'],
        'version_name': properties['VERSION_NAME'],
        'version_code': str(int(properties['VERSION_CODE']) * 5),
    }


def default_toolchain(root):
    def output(command):
        return subprocess.run(command, check=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True).stdout.strip()
    gomobile = Path(os.environ.get('GOMOBILE_SOURCE', Path(root) / 'core115-source/gomobile'))
    return {
        'go': output(['go', 'version']),
        'java': output(['java', '-version']),
        'gomobile_commit': subprocess.run(
            ['git', '-C', str(gomobile), 'rev-parse', 'HEAD'],
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        ).stdout.strip(),
    }


def validate_toolchain(toolchain):
    if not isinstance(toolchain, dict) or not toolchain:
        raise ValueError('build toolchain must be a non-empty object')
    for key, value in toolchain.items():
        if not isinstance(key, str) or not key or not isinstance(value, str) or not value.strip():
            raise ValueError('build toolchain entries must be non-empty strings')


def create_runtime_attestation(root, baseline_path, aar_path, output_path, seed_fingerprint, require_clean=True, toolchain=None):
    root = Path(root)
    baseline_path = Path(baseline_path)
    aar_path = Path(aar_path)
    output_path = Path(output_path)
    provenance = load_provenance()
    baseline = json.loads(baseline_path.read_text(encoding='utf-8'))
    require_digest(seed_fingerprint, 'seed fingerprint')
    if require_clean:
        require_clean_exact(root, baseline, provenance)
    else:
        provenance.verify_source_attestation(baseline, root)
    identity = provenance.artifact_identity(aar_path)
    toolchain = toolchain or {'fixture': 'explicit-test-toolchain'}
    validate_toolchain(toolchain)
    data = {
        'schema': 1,
        'kind': 'oix-secret-runtime-native-provenance',
        'generator': {'name': GENERATOR_NAME, 'version': GENERATOR_VERSION},
        'source_attestation_path': str(baseline_path.relative_to(root)).replace('\\', '/'),
        'source_attestation_sha256': provenance.sha(baseline_path.read_bytes()),
        'source_attestation_head': provenance.git(root, 'rev-parse', 'HEAD'),
        'source_attestation_tree': provenance.git(root, 'rev-parse', 'HEAD^{tree}'),
        'built_source_head': baseline['built_source_head'],
        'built_source_tree': baseline['built_source_tree'],
        'seed_fingerprint': seed_fingerprint,
        'aar_members_sha256': aar_members(aar_path, provenance),
        **identity,
        'core_version': baseline['core_version'],
        'build_tags': baseline['build_tags'],
        'build_goos': baseline['build_goos'],
        'build_goarch': baseline['build_goarch'],
        'build_toolchain': toolchain,
        'application': application_identity(root),
    }
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(data, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    return data


def verify_apk(apk_path, aar_path, data, provenance, aapt=None):
    with zipfile.ZipFile(aar_path) as aar, zipfile.ZipFile(apk_path) as apk:
        names = apk.namelist()
        if len(names) != len(set(names)):
            raise ValueError('Duplicate APK entries')
        cores = sorted(name for name in names if name.startswith('lib/') and name.endswith('/libgojni.so'))
        expected = sorted('lib/' + abi + '/libgojni.so' for abi in data['native_sha256'])
        if cores != expected:
            raise ValueError('APK ABI set mismatch')
        for name in cores:
            abi = name.split('/')[1]
            if provenance.sha(apk.read(name)) != provenance.sha(aar.read('jni/' + abi + '/libgojni.so')):
                raise ValueError('APK/AAR native mismatch: ' + name)
    if aapt:
        badging = subprocess.run([str(aapt), 'dump', 'badging', str(apk_path)], check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True).stdout
        match = re.search(r"^package: name='([^']+)' versionCode='([^']+)' versionName='([^']+)'", badging, re.MULTILINE)
        if not match:
            raise ValueError('Unable to read APK application identity')
        actual = dict(zip(('application_id', 'version_code', 'version_name'), match.groups()))
        if actual != data['application']:
            raise ValueError('APK application identity mismatch')


def verify_runtime_provenance(root, baseline_path, runtime_path, aar_path, apk_path=None, verify_worktree=True, verify_oix=False, aapt=None, expected_seed_fingerprint=None):
    root = Path(root)
    baseline_path = Path(baseline_path)
    runtime_path = Path(runtime_path)
    aar_path = Path(aar_path)
    provenance = load_provenance()
    baseline = json.loads(baseline_path.read_text(encoding='utf-8'))
    data = json.loads(runtime_path.read_text(encoding='utf-8'))
    if data.get('schema') != 1 or data.get('kind') != 'oix-secret-runtime-native-provenance':
        raise ValueError('Unsupported runtime provenance schema')
    if data.get('generator') != {'name': GENERATOR_NAME, 'version': GENERATOR_VERSION}:
        raise ValueError('Runtime provenance generator mismatch')
    require_digest(data.get('seed_fingerprint'), 'seed fingerprint')
    if expected_seed_fingerprint is not None:
        require_digest(expected_seed_fingerprint, 'expected seed fingerprint')
        if data['seed_fingerprint'] != expected_seed_fingerprint:
            raise ValueError('Seed fingerprint mismatch')
    require_digest(data.get('source_attestation_sha256'), 'source attestation SHA256')
    require_digest(data.get('source_attestation_head'), 'source attestation HEAD', 40)
    require_digest(data.get('source_attestation_tree'), 'source attestation tree', 40)
    require_digest(data.get('built_source_head'), 'built source HEAD', 40)
    require_digest(data.get('built_source_tree'), 'built source tree', 40)
    if verify_worktree:
        require_clean_exact(root, baseline, provenance)
    else:
        provenance.verify_source_attestation(baseline, root)
    if data['source_attestation_path'] != str(baseline_path.relative_to(root)).replace('\\', '/'):
        raise ValueError('Source attestation path mismatch')
    if provenance.sha(baseline_path.read_bytes()) != data['source_attestation_sha256']:
        raise ValueError('Source attestation identity mismatch')
    if data['source_attestation_head'] != provenance.git(root, 'rev-parse', 'HEAD'):
        raise ValueError('Source attestation HEAD mismatch')
    if data['source_attestation_tree'] != provenance.git(root, 'rev-parse', 'HEAD^{tree}'):
        raise ValueError('Source attestation tree mismatch')
    if data['built_source_head'] != baseline['built_source_head'] or data['built_source_tree'] != baseline['built_source_tree']:
        raise ValueError('Built source identity mismatch')
    for field in ('core_version', 'build_tags', 'build_goos', 'build_goarch'):
        if data.get(field) != baseline[field]:
            raise ValueError('Build metadata mismatch: ' + field)
    validate_toolchain(data.get('build_toolchain'))
    if data.get('application') != application_identity(root):
        raise ValueError('Application source identity mismatch')
    actual = provenance.artifact_identity(aar_path)
    for field in ('aar_sha256', 'aar_content_sha256', 'classes_jar_sha256', 'native_sha256'):
        if data.get(field) != actual[field]:
            raise ValueError('Runtime AAR identity mismatch: ' + field)
    if data.get('aar_members_sha256') != aar_members(aar_path, provenance):
        raise ValueError('Runtime AAR member identity mismatch')
    with zipfile.ZipFile(aar_path) as archive:
        for abi in data['native_sha256']:
            native = archive.read('jni/' + abi + '/libgojni.so')
            provenance.verify_build(native, data)
            if b'EXPORTER-Dler-Snell-Identity-v2' not in native:
                raise ValueError('Missing Oix identity exporter: ' + abi)
            if verify_oix:
                load_oix_verifier(root).verify(native, os.environ.get('OIX_DNS_AUTH_SEED', ''))
    if apk_path:
        verify_apk(Path(apk_path), aar_path, data, provenance, aapt)
    return {'status': 'PASS', 'built_source_head': data['built_source_head'], 'aar_sha256': data['aar_sha256'], 'apk': str(apk_path) if apk_path else None}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=HERE.parents[2])
    parser.add_argument('--baseline', type=Path)
    parser.add_argument('--aar', type=Path, required=True)
    parser.add_argument('--runtime-attestation', type=Path, required=True)
    parser.add_argument('--create', action='store_true')
    parser.add_argument('--seed-fingerprint')
    parser.add_argument('--expected-seed-fingerprint')
    parser.add_argument('--apk', type=Path)
    parser.add_argument('--verify-oix', action='store_true')
    parser.add_argument('--aapt', type=Path)
    args = parser.parse_args()
    baseline = args.baseline or args.root / 'buildScript/lib/core/native-baseline.json'
    try:
        if args.create:
            data = create_runtime_attestation(args.root, baseline, args.aar, args.runtime_attestation, args.seed_fingerprint, toolchain=default_toolchain(args.root))
            print(json.dumps({'status': 'CREATED', 'built_source_head': data['built_source_head'], 'aar_sha256': data['aar_sha256']}, indent=2))
        else:
            print(json.dumps(verify_runtime_provenance(args.root, baseline, args.runtime_attestation, args.aar, args.apk, verify_oix=args.verify_oix, aapt=args.aapt, expected_seed_fingerprint=args.expected_seed_fingerprint), indent=2))
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError, zipfile.BadZipFile) as error:
        parser.exit(1, 'RUNTIME PROVENANCE FAILED: ' + str(error) + '\n')


if __name__ == '__main__':
    main()
