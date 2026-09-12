"""Audit preserved source evidence before installing the fallback AAR locally."""
from pathlib import Path
import hashlib, json, shutil, zipfile, sys
R=Path(__file__).resolve().parents[1]
N=R.parent/'native-build-lab'
E=R/'evidence/fallback'
E.mkdir(parents=True,exist_ok=True)
sha=lambda b:hashlib.sha256(b).hexdigest()
report=json.loads((N/'evidence/fallback/artifact-verification.json').read_text())
new=N/'libcore-fallback-arm64.aar'
old=R/'app/libs/libcore.aar'
assert sha(new.read_bytes())==report['built']['sha256']
assert report['class_descriptors_equal'] and report['jni_exports_equal'] and report['arm64_only']
assert report['built']['classes']==report['restored']['classes']
assert report['built']['libs']['jni/arm64-v8a/libgojni.so']['jni_exports']==report['restored']['libs']['jni/arm64-v8a/libgojni.so']['jni_exports']
sys.path.insert(0,str(R/'buildScript/lib/core'))
from verify_native import MARKERS, verify
with zipfile.ZipFile(new) as z:
    so=z.read('jni/arm64-v8a/libgojni.so'); bridge=z.read('classes.jar')
    assert sha(so)==report['built']['libs']['jni/arm64-v8a/libgojni.so']['sha256']
    assert sha(bridge)==report['built']['classes_jar_sha256']
    markers={p:all(m.encode() in so for m in ms) for p,ms in MARKERS.items()}
    assert all(markers.values()) and len(markers)==9
    assert all(m.encode() in so for m in report['fallback_binary_markers'])
identity=json.loads((N/'evidence/fallback/patch-identity.json').read_text())
assert sha((N/'evidence/fallback/fallback-native.patch').read_bytes())==identity['patch_sha256']
for f,v in identity['files'].items():
    assert sha((N/'sing-box'/f).read_bytes().replace(b'\r\n',b'\n'))==v['target_lf_sha256']
for name in ['integrated-tests.log','wrapper-tests.log']:
    log=(N/'evidence/fallback'/name).read_text()
    assert 'EXIT 0' in log and 'FAIL' not in log
lock=R/'buildScript/lib/core/native-baseline.json'
baseline=json.loads(lock.read_text())
for dep,pin in baseline['dependency_pins'].items(): assert report['sources'][dep]['commit']==pin
print(json.dumps({'audit':'PASS','protocols':markers,'patch':identity['patch_sha256']},indent=2))
if '--install' not in sys.argv: sys.exit(0)
assert 'BUILD SUCCESSFUL' in (R.parent/'final-preferred-build.log').read_text(errors='replace'), 'Wait for prior Gradle build'
assert sha(old.read_bytes())==report['restored']['sha256']
backup=E/'libcore-before-fallback.aar'
assert not backup.exists(), 'Backup already exists: inspect before repeating'
shutil.copy2(old,backup)
shutil.copy2(lock,E/'native-baseline-before-fallback.json')
for name in ['artifact-summary.json','artifact-verification.json','patch-identity.json','fallback-native.patch','integrated-tests.log','wrapper-tests.log','so-go-buildinfo.log']:
    shutil.copy2(N/'evidence/fallback'/name,E/name)
newlock={
    'schema':1,'source_kind':'locally source-built arm64 fallback core; isolated native-build-lab',
    'source_root':str(N),'aar_sha256':sha(new.read_bytes()),'classes_jar_sha256':sha(bridge),
    'native_sha256':{'arm64-v8a':sha(so)},
    'dependency_pins':baseline['dependency_pins'],
    'source_dependencies':{k:{'commit':v['commit'],'tree':v['tree'],'go_mod_sha256':v['go_mod_sha256']} for k,v in report['sources'].items()},
    'patch_provenance':{'path':'evidence/fallback/fallback-native.patch','sha256':identity['patch_sha256'],'files':identity['files']},
    'jni_evidence':{'class_descriptors_equal':True,'exports_equal':True,'export_count':len(report['built']['libs']['jni/arm64-v8a/libgojni.so']['jni_exports']),'previous_aar_sha256':report['restored']['sha256']},
    'protocol_markers':markers,
    'fallback_evidence':'evidence/fallback/integrated-tests.log',
    'limitations':['arm64-v8a only','JNI compatibility is static; Android device not tested','Protocol markers are presence evidence, not live traffic proof','Fallback covers DialContext failures; no lazy handshake/read/write replay; no guaranteed instant switch']}
lock.write_text(json.dumps(newlock,indent=2)+'\n')
shutil.copy2(new,old)
print(json.dumps(verify(old),indent=2))
(E/'integration-audit.json').write_text(json.dumps({'status':'PASS','protocols':markers,'aar_sha256':sha(old.read_bytes()),'backup_sha256':sha(backup.read_bytes())},indent=2))
