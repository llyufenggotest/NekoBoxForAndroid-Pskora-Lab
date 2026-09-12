"""Finish only after the real Gradle build; verify/copy APK and produce evidence."""
from pathlib import Path
import hashlib,json,shutil,sys,subprocess,xml.etree.ElementTree as ET,zipfile
R=Path(__file__).resolve().parents[1]; E=R/'evidence'; E.mkdir(exist_ok=True)
log=R.parent/'final-fallback-build.log'
assert 'BUILD SUCCESSFUL' in log.read_text(errors='replace')
sys.path.insert(0,str(R/'buildScript/lib/core'))
from verify_native import verify, MARKERS
apks=list((R/'app/build/outputs/apk/fdroid/release').glob('*.apk'))
assert len(apks)==1,apks
apk=apks[0]; result=verify(R/'app/libs/libcore.aar',apk)
target=R.parent/'NekoBoxYF-preferred-fallback-arm64-release-unsigned.apk'
shutil.copy2(apk,target)
assert hashlib.sha256(target.read_bytes()).digest()==hashlib.sha256(apk.read_bytes()).digest()
result['delivery']=str(target); result['size_bytes']=target.stat().st_size
result['apk_sha256']=hashlib.sha256(target.read_bytes()).hexdigest()
with zipfile.ZipFile(target) as z:
 so=z.read('lib/arm64-v8a/libgojni.so')
 result['so_sha256']=hashlib.sha256(so).hexdigest()
 result['protocol_markers']={p:all(m.encode() in so for m in ms) for p,ms in MARKERS.items()}
 assert len(result['protocol_markers'])==9 and all(result['protocol_markers'].values())
suites=[]
for p in sorted((R/'app/build/test-results/testFdroidReleaseUnitTest').glob('TEST-*.xml')):
 s=ET.parse(p).getroot(); suites.append({'name':s.attrib['name'],**{k:int(s.attrib.get(k,0)) for k in ['tests','failures','errors','skipped']}})
assert suites
result['unit_tests']={k:sum(s[k] for s in suites) for k in ['tests','failures','errors','skipped']}
assert result['unit_tests']['failures']==0 and result['unit_tests']['errors']==0
result['suites']=suites
p=subprocess.run([sys.executable,'-m','unittest','discover','-s','buildScript/lib/core','-p','test_verify_native.py','-v'],cwd=R,capture_output=True)
(E/'native-gate-tests.log').write_bytes(p.stdout+p.stderr); assert p.returncode==0
shutil.copy2(log,E/'final-fallback-build.log')
(E/'final-artifact-verification.json').write_text(json.dumps(result,indent=2)+'\n')
(target.with_suffix('.apk.sha256')).write_text(result['apk_sha256']+'  '+target.name+'\n')
report=f'''# Final fallback and Android integration acceptance

## Deliverable

- APK: `{target}`
- Signing: unsigned Release artifact; requires signing before Android installation.
- Size: {result['size_bytes']} bytes
- SHA-256: `{result['apk_sha256']}`
- ABI: arm64-v8a only.
- AAR SHA-256: `{result['aar_sha256']}`
- APK/AAR libgojni.so SHA-256: `{result['so_sha256']}`; exact byte identity verified.

## Executed acceptance

- `:app:testFdroidReleaseUnitTest :app:assembleFdroidRelease --no-daemon --console=plain`: BUILD SUCCESSFUL. Complete output: `evidence/final-fallback-build.log`.
- Release JVM tests: {result['unit_tests']}; per-suite counts: `evidence/final-artifact-verification.json`.
- Native gate tests: 7 passed; stale AAR, stale extra ABI, missing marker even after hash changes, and APK mismatch rejected. Output: `evidence/native-gate-tests.log`.
- Nine static protocol families all present in final APK: {', '.join(MARKERS)}. This does not prove real remote traffic for all protocols.
- Source fallback tests: `evidence/fallback/integrated-tests.log`, EXIT 0. Real SOCKS5 handshake/HTTP failure then backup success and established tunnel preservation; bounded attempts/budget, cancellation, isolation, health/ranking modes, stale probes, concurrency and established connections covered.
- Source wrapper regression tests: Oppa, TunNet control, VLESS and Trojan passed (`evidence/fallback/wrapper-tests.log`).
- Prior/new Java class descriptors and JNI exports equal in `evidence/fallback/artifact-verification.json`. AAR and SO hashes independently recalculated before integration.
- Source patch SHA-256: `31db9448ac25185439960e5807420113de8df83623c31a208995a441d1898bc9`; applied source file LF-normalized hashes matched the tested source.
- `git diff --check` passed. `adb devices` returned no attached devices.

## Integration and review fixes

- Original AAR retained at `evidence/fallback/libcore-before-fallback.aar`; original lock at `evidence/fallback/native-baseline-before-fallback.json`.
- `native-baseline.json` now records locally built source origin, dependency pins, patch provenance, hashes and JNI evidence. All native Gradle gates remain enabled.
- `VERIFIED_NATIVE_DIAL_FALLBACK=true` enables dial_fallback and the selected latency/stable mode. Tests retain explicit legacy false behavior and assert enabled Release defaults.
- Native fallback rejects nested outbound groups. Android now explicitly rejects nested preferred candidates at selection, save, restore and config generation; it never drops candidates silently.
- Independent review P2 selector mismatch fixed using candidate-context validation. P2 dynamic-reference breakage fixed by validating the proposed complete preferred reference snapshot before writes, using a noncolliding temporary ID for creation. Regression tests cover both plus unchanged source candidates and safe saves.
- User copy explains mode behavior, new-connection scope and reconnecting after source changes, without internal verification jargon.

## Limits and remaining device acceptance

- No Android device was attached: UI interaction, VPN lifecycle, traffic on device, actual switching timing and upgrade installation are not verified.
- Fallback acts on DialContext failures. UDP ListenPacket does not use the new dial fallback path. Lazy handshake/read/write failures are not replayed; existing established connections are not migrated. No instant-switch guarantee.
- Enhanced latency mode ranks each new dial by delay and does not honor tolerance as a dial-switch hysteresis threshold. The editor states that tolerance does not constrain dial ranking. Stable mode keeps a healthy selection. This iteration does not change/rebuild that native selection algorithm.
- TunNet, external plugins and custom overrides are rejected as auto-switch candidates; standalone use remains separate.
- Subscription/group membership changes take effect when configuration is regenerated on reconnect; no live replacement of active membership.
- The source report's statement that no APK was built/core replaced predates this integration; this report and final hash evidence describe the final artifact.
- Initial Gradle invocation used a JDK parent directory and failed before building; corrected to `F:/tools/jdk-21/jdk-21.0.12`, then executed successfully.
- No commit or remote push performed. Work confined to the authorized lab repository and evidence/delivery paths.
'''
(E/'final-integration-report.md').write_text(report,encoding='utf-8')
shutil.copy2(E/'final-integration-report.md',R.parent/'evidence/final-integration-report.md')
print(json.dumps({k:v for k,v in result.items() if k!='suites'},indent=2))
