"""Verify the archived local migration; base Git pins are not remote reproducibility."""
import json
import zipfile
from pathlib import Path
import hashlib


def sha(data):
    return hashlib.sha256(data).hexdigest()


def verify_provenance(baseline, root):
    if baseline.get('schema') != 2:
        raise ValueError('Local source provenance requires schema 2')
    manifest_path = root / baseline['source_manifest_path']
    snapshot_path = root / baseline['source_snapshot_path']
    if sha(manifest_path.read_bytes()) != baseline['source_manifest_sha256']:
        raise ValueError('Source manifest identity mismatch')
    if sha(snapshot_path.read_bytes()) != baseline['source_snapshot_sha256']:
        raise ValueError('Source snapshot identity mismatch')
    manifest = json.loads(manifest_path.read_text())
    with zipfile.ZipFile(snapshot_path) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)) or set(names) != set(manifest):
            raise ValueError('Source snapshot inventory mismatch')
        for name, digest in manifest.items():
            if '..' in Path(name).parts or Path(name).is_absolute():
                raise ValueError('Unsafe snapshot path')
            if sha(archive.read(name)) != digest:
                raise ValueError('Source snapshot member mismatch: ' + name)
        for name, dep in baseline['source_dependencies'].items():
            if sha(archive.read(name + '/go.mod')) != dep['go_mod_sha256']:
                raise ValueError('Source dependency mismatch: ' + name)
    evidence = root / baseline['bridge_evidence_path']
    if sha(evidence.read_bytes()) != baseline['bridge_evidence_sha256']:
        raise ValueError('Bridge evidence mismatch')
    report = json.loads(evidence.read_text())
    built = report['built']
    if built['sha256'] != baseline['aar_sha256'] or built['classes_jar_sha256'] != baseline['classes_jar_sha256']:
        raise ValueError('Bridge artifact mismatch')
    for name, dep in baseline['source_dependencies'].items():
        for key in ('commit', 'tree', 'go_mod_sha256'):
            if report['sources'][name][key] != dep[key]:
                raise ValueError('Source identity mismatch: ' + name)
    old, new = report['restored'], report['built']
    added = []
    for cls, data in old['classes'].items():
        for kind in ('fields', 'methods'):
            if any(x not in new['classes'].get(cls, {}).get(kind, []) for x in data[kind]):
                raise ValueError('Removed Java API')
    for cls, data in new['classes'].items():
        for kind in ('fields', 'methods'):
            added += [[cls, kind, x] for x in data[kind] if x not in old['classes'].get(cls, {}).get(kind, [])]
    allowed = [['libcore/BoxInstance', 'methods', ['closeIdleConnections', '()V', 256]], ['libcore/BoxPlatformInterface', 'methods', ['closeDefaultInterfaceMonitor', '(Llibcore/InterfaceUpdateListener;)V', 0]], ['libcore/BoxPlatformInterface', 'methods', ['networkInterfacesJSON', '()Ljava/lang/String;', 0]], ['libcore/BoxPlatformInterface', 'methods', ['startDefaultInterfaceMonitor', '(Llibcore/InterfaceUpdateListener;)V', 0]], ['libcore/InterfaceUpdateListener', 'methods', ['networkMonitorID', '()J', 0]], ['libcore/InterfaceUpdateListener', 'methods', ['updateDefaultInterface', '(Ljava/lang/String;IZZ)V', 0]], ['libcore/Libcore$proxyBoxPlatformInterface', 'methods', ['closeDefaultInterfaceMonitor', '(Llibcore/InterfaceUpdateListener;)V', 256]], ['libcore/Libcore$proxyBoxPlatformInterface', 'methods', ['networkInterfacesJSON', '()Ljava/lang/String;', 256]], ['libcore/Libcore$proxyBoxPlatformInterface', 'methods', ['startDefaultInterfaceMonitor', '(Llibcore/InterfaceUpdateListener;)V', 256]], ['libcore/Libcore$proxyInterfaceUpdateListener', 'fields', ['refnum', 'I', 0]], ['libcore/Libcore$proxyInterfaceUpdateListener', 'methods', ['<init>', '(I)V', 0]], ['libcore/Libcore$proxyInterfaceUpdateListener', 'methods', ['incRefnum', '()I', 0]], ['libcore/Libcore$proxyInterfaceUpdateListener', 'methods', ['networkMonitorID', '()J', 256]], ['libcore/Libcore$proxyInterfaceUpdateListener', 'methods', ['updateDefaultInterface', '(Ljava/lang/String;IZZ)V', 256]]]
    allowed.insert(1, ['libcore/BoxInstance', 'methods', ['runtimeSelection', '(Ljava/lang/String;)Ljava/lang/String;', 256]])
    allowed.append(['libcore/Libcore', 'methods', ['nekoLogReconfigure', '(ZZ)V', 264]])
    allowed.sort()
    if sorted(added) != allowed or baseline['jni_evidence']['added_java'] != allowed:
        raise ValueError('Unreviewed Java additions')
    lib = 'jni/arm64-v8a/libgojni.so'
    before, after = set(old['libs'][lib]['jni_exports']), set(new['libs'][lib]['jni_exports'])
    expected = ['JNI_OnLoad', 'Java_libcore_BoxInstance_closeIdleConnections', 'Java_libcore_Libcore_00024proxyBoxPlatformInterface_closeDefaultInterfaceMonitor', 'Java_libcore_Libcore_00024proxyBoxPlatformInterface_networkInterfacesJSON', 'Java_libcore_Libcore_00024proxyBoxPlatformInterface_startDefaultInterfaceMonitor', 'Java_libcore_Libcore_00024proxyInterfaceUpdateListener_networkMonitorID', 'Java_libcore_Libcore_00024proxyInterfaceUpdateListener_updateDefaultInterface']
    expected.insert(2, 'Java_libcore_BoxInstance_runtimeSelection')
    expected.append('Java_libcore_Libcore_nekoLogReconfigure')
    expected.sort()
    if before - after or sorted(after - before) != expected or baseline['jni_evidence']['added_jni'] != expected:
        raise ValueError('Unreviewed JNI changes')
    if baseline['jni_evidence']['export_count'] != len(after):
        raise ValueError('JNI count mismatch')


def verify_build(data, baseline):
    for value in [baseline['core_version'], '-tags=' + baseline['build_tags'], 'GOOS=' + baseline['build_goos'], 'GOARCH=' + baseline['build_goarch'], '-buildmode=c-shared', 'CGO_ENABLED=1']:
        if value.encode() not in data:
            raise ValueError('Missing native build evidence: ' + value)
    if data[:4] != b'\x7fELF' or int.from_bytes(data[18:20], 'little') != 183:
        raise ValueError('Expected arm64 ELF')
