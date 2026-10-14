#!/usr/bin/env python3
"""Create or verify a source-commit/AAR attestation without self-reference."""
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import subprocess
import zipfile


def sha(data):
    return hashlib.sha256(data).hexdigest()


def git(root, *args, check=True):
    result = subprocess.run(
        ['git', '-c', 'core.fsmonitor=false', '-C', str(root), *args],
        check=check,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return result.stdout.decode().strip()


def require_digest(value, label, size):
    if not isinstance(value, str) or len(value) != size or set(value) - set('0123456789abcdef') or set(value) == {'0'}:
        raise ValueError(label + ' must be a non-zero lowercase hexadecimal digest')


def canonical_zip_digest(path):
    digest = hashlib.sha256()
    with zipfile.ZipFile(path) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)):
            raise ValueError('Duplicate ZIP entries')
        for name in sorted(names):
            payload = archive.read(name)
            encoded = name.encode()
            digest.update(len(encoded).to_bytes(8, 'big'))
            digest.update(encoded)
            digest.update(len(payload).to_bytes(8, 'big'))
            digest.update(payload)
    return digest.hexdigest()


def artifact_identity(aar_path):
    aar_path = Path(aar_path)
    with zipfile.ZipFile(aar_path) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)):
            raise ValueError('Duplicate AAR entries')
        native = {
            name.removeprefix('jni/').removesuffix('/libgojni.so'): sha(archive.read(name))
            for name in names
            if name.startswith('jni/') and name.endswith('/libgojni.so')
        }
        if not native:
            raise ValueError('AAR contains no libgojni.so')
        classes = sha(archive.read('classes.jar'))
    return {
        'aar_sha256': sha(aar_path.read_bytes()),
        'aar_content_sha256': canonical_zip_digest(aar_path),
        'classes_jar_sha256': classes,
        'native_sha256': dict(sorted(native.items())),
    }


def tracked_source_manifest(root, source_head):
    output = git(root, 'ls-tree', '-r', '--full-tree', source_head, 'core115-source').splitlines()
    manifest = {}
    objects = []
    for line in output:
        metadata, name = line.split('\t', 1)
        mode, kind, object_id = metadata.split()
        if kind == 'blob':
            objects.append((object_id, name.replace('\\', '/')))
    process = subprocess.Popen(
        ['git', '-c', 'core.fsmonitor=false', '-C', str(root), 'cat-file', '--batch'],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
    )
    input_data = ''.join(object_id + '\n' for object_id, _ in objects).encode()
    output_data, _ = process.communicate(input_data)
    if process.returncode != 0:
        raise ValueError('git cat-file --batch failed')
    offset = 0
    for object_id, name in objects:
        newline = output_data.index(b'\n', offset)
        header = output_data[offset:newline].decode().split()
        if len(header) != 3 or header[0] != object_id or header[1] != 'blob':
            raise ValueError('Unexpected git object while hashing ' + name)
        size = int(header[2])
        start = newline + 1
        end = start + size
        payload = output_data[start:end]
        if output_data[end:end + 1] != b'\n':
            raise ValueError('Malformed git batch stream')
        manifest[name] = sha(payload)
        offset = end + 1
    if not manifest:
        raise ValueError('Source commit contains no tracked core115-source files')
    return dict(sorted(manifest.items()))


def write_snapshot(root, source_head, snapshot_path, manifest_path):
    manifest = tracked_source_manifest(root, source_head)
    snapshot_path = Path(snapshot_path)
    manifest_path = Path(manifest_path)
    snapshot_path.parent.mkdir(parents=True, exist_ok=True)
    manifest_path.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(snapshot_path, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for name in manifest:
            payload = subprocess.run(
                ['git', '-c', 'core.fsmonitor=false', '-C', str(root), 'show', f'{source_head}:{name}'],
                check=True,
                stdout=subprocess.PIPE,
            ).stdout
            info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o100644 << 16
            archive.writestr(info, payload)
    manifest_path.write_text(json.dumps(manifest, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    return manifest


def verify_snapshot(baseline, root, expected_manifest=None):
    root = Path(root)
    manifest_path = root / baseline['source_manifest_path']
    snapshot_path = root / baseline['source_snapshot_path']
    require_digest(baseline['source_manifest_sha256'], 'source manifest SHA256', 64)
    require_digest(baseline['source_snapshot_sha256'], 'source snapshot SHA256', 64)
    if sha(manifest_path.read_bytes()) != baseline['source_manifest_sha256']:
        raise ValueError('Source manifest identity mismatch')
    if sha(snapshot_path.read_bytes()) != baseline['source_snapshot_sha256']:
        raise ValueError('Source snapshot identity mismatch')
    manifest = json.loads(manifest_path.read_text(encoding='utf-8'))
    if expected_manifest is not None and manifest != expected_manifest:
        raise ValueError('Source manifest does not describe built source HEAD')
    with zipfile.ZipFile(snapshot_path) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)) or set(names) != set(manifest):
            raise ValueError('Source snapshot inventory mismatch')
        for name, digest in manifest.items():
            pure = PurePosixPath(name)
            if '..' in pure.parts or pure.is_absolute():
                raise ValueError('Unsafe snapshot path')
            if sha(archive.read(name)) != digest:
                raise ValueError('Source snapshot member mismatch: ' + name)
    return manifest


def verify_source_attestation(baseline, root, verify_attestation_head=True):
    root = Path(root)
    if baseline.get('schema') != 3:
        raise ValueError('Native provenance requires schema 3')
    source_head = baseline['built_source_head']
    source_tree = baseline['built_source_tree']
    require_digest(source_head, 'built source HEAD', 40)
    require_digest(source_tree, 'built source tree', 40)
    if verify_attestation_head:
        attestation_head = git(root, 'rev-parse', 'HEAD')
        if git(root, 'rev-parse', 'HEAD^') != source_head:
            raise ValueError('Current attestation HEAD must be the direct child of built source HEAD')
        if git(root, 'rev-list', '--count', source_head + '..' + attestation_head) != '1':
            raise ValueError('Current attestation HEAD chain mismatch')
    try:
        actual_source_tree = git(root, 'rev-parse', source_head + '^{tree}')
    except subprocess.CalledProcessError as error:
        raise ValueError('Built source HEAD is not a local commit') from error
    if actual_source_tree != source_tree:
        raise ValueError('Built source tree mismatch')
    expected_manifest = tracked_source_manifest(root, source_head)
    manifest = verify_snapshot(baseline, root, expected_manifest)
    if baseline.get('source_file_count') != len(manifest):
        raise ValueError('Source file count mismatch')


def verify_provenance(baseline, root, verify_source_content=True, verify_attestation_head=True):
    root = Path(root)
    if verify_source_content:
        verify_source_attestation(baseline, root, verify_attestation_head)
    else:
        if baseline.get('schema') != 3:
            raise ValueError('Native provenance requires schema 3')
        source_head = baseline['built_source_head']
        source_tree = baseline['built_source_tree']
        require_digest(source_head, 'built source HEAD', 40)
        require_digest(source_tree, 'built source tree', 40)
        if verify_attestation_head:
            attestation_head = git(root, 'rev-parse', 'HEAD')
            if git(root, 'rev-parse', 'HEAD^') != source_head:
                raise ValueError('Current attestation HEAD must be the direct child of built source HEAD')
            if git(root, 'rev-list', '--count', source_head + '..' + attestation_head) != '1':
                raise ValueError('Current attestation HEAD chain mismatch')
        try:
            actual_source_tree = git(root, 'rev-parse', source_head + '^{tree}')
        except subprocess.CalledProcessError as error:
            raise ValueError('Built source HEAD is not a local commit') from error
        if actual_source_tree != source_tree:
            raise ValueError('Built source tree mismatch')

    aar_path = root / baseline['aar_path']
    actual = artifact_identity(aar_path)
    for field in ('aar_sha256', 'aar_content_sha256', 'classes_jar_sha256'):
        require_digest(baseline[field], field, 64)
        if actual[field] != baseline[field]:
            raise ValueError(field + ' mismatch')
    if actual['native_sha256'] != baseline['native_sha256']:
        raise ValueError('AAR native identity mismatch')
    for abi, digest in baseline['native_sha256'].items():
        require_digest(digest, 'native identity ' + abi, 64)


def verify_build(data, baseline):
    for value in [baseline['core_version'], '-tags=' + baseline['build_tags'], 'GOOS=' + baseline['build_goos'], 'GOARCH=' + baseline['build_goarch'], '-buildmode=c-shared', 'CGO_ENABLED=1']:
        if value.encode() not in data:
            raise ValueError('Missing native build evidence: ' + value)
    if data[:4] != b'\x7fELF' or int.from_bytes(data[18:20], 'little') != 183:
        raise ValueError('Expected arm64 ELF')


def create_attestation(root, source_head, aar_path, snapshot_path, manifest_path, output_path):
    root = Path(root)
    if git(root, 'status', '--porcelain'):
        raise ValueError('Source build attestation requires a clean worktree')
    if git(root, 'rev-parse', 'HEAD') != source_head:
        raise ValueError('Source build must run at the exact source HEAD')
    manifest = write_snapshot(root, source_head, snapshot_path, manifest_path)
    data = {
        'schema': 3,
        'source_kind': 'clean-git-source-commit',
        'built_source_head': source_head,
        'built_source_tree': git(root, 'rev-parse', source_head + '^{tree}'),
        'source_manifest_path': str(Path(manifest_path).relative_to(root)).replace('\\', '/'),
        'source_manifest_sha256': sha(Path(manifest_path).read_bytes()),
        'source_snapshot_path': str(Path(snapshot_path).relative_to(root)).replace('\\', '/'),
        'source_snapshot_sha256': sha(Path(snapshot_path).read_bytes()),
        'aar_path': str(Path(aar_path).relative_to(root)).replace('\\', '/'),
        **artifact_identity(aar_path),
        'source_file_count': len(manifest),
        'core_version': '1.15.0-alpha.12-private-lab',
        'build_tags': 'with_gvisor,with_quic,with_wireguard,with_utls,with_clash_api',
        'build_goos': 'android',
        'build_goarch': 'arm64',
    }
    with zipfile.ZipFile(aar_path) as archive:
        verify_build(archive.read('jni/arm64-v8a/libgojni.so'), data)
    Path(output_path).write_text(json.dumps(data, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    return data


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[3])
    parser.add_argument('--verify', type=Path)
    parser.add_argument('--source-only', action='store_true')
    parser.add_argument('--create', action='store_true')
    parser.add_argument('--source-head')
    parser.add_argument('--aar', type=Path)
    parser.add_argument('--snapshot', type=Path)
    parser.add_argument('--manifest', type=Path)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    if args.create:
        data = create_attestation(args.root, args.source_head, args.aar, args.snapshot, args.manifest, args.output)
        print(json.dumps(data, indent=2))
    else:
        path = args.verify or args.root / 'buildScript/lib/core/native-baseline.json'
        baseline = json.loads(path.read_text(encoding='utf-8'))
        if args.source_only:
            verify_source_attestation(baseline, args.root)
            print('SOURCE ATTESTATION PASS')
        else:
            verify_provenance(baseline, args.root)
            print('PROVENANCE PASS')


if __name__ == '__main__':
    main()
