"""Compile the production presenter without Gradle; check Android row wiring statically."""
from pathlib import Path
import os, subprocess, tempfile, xml.etree.ElementTree as ET
root = Path(__file__).resolve().parents[2]
home = Path.home()
java = home/'tools/jdk-17.0.20.1+1/bin/java.exe'
lib = next((home/'.gradle/wrapper/dists').glob('gradle-8.10.2-bin/*/gradle-8.10.2/lib'))
cp = os.pathsep.join(str(p) for p in lib.glob('*.jar') if p.name.startswith(('kotlin-', 'trove4j-', 'annotations-')))
stdlib = next(lib.glob('kotlin-stdlib-*.jar'))
with tempfile.TemporaryDirectory(prefix='preferred-ui-') as d:
    out = str(Path(d)/'classes')
    subprocess.run([str(java), '-cp', cp, 'org.jetbrains.kotlin.cli.jvm.K2JVMCompiler', '-no-stdlib', '-no-reflect', '-classpath', str(stdlib), '-d', out, str(root/'app/src/main/java/io/nekohasekai/sagernet/ui/PreferredMemberPresentation.kt'), str(Path(__file__).with_name('PreferredPresentationTest.kt'))], check=True)
    subprocess.run([str(java), '-cp', out+os.pathsep+str(stdlib), 'io.nekohasekai.sagernet.ui.PreferredPresentationTestKt'], check=True)
f = (root/'app/src/main/java/io/nekohasekai/sagernet/ui/PreferredGroupFragment.kt').read_text(encoding='utf8')
c = (root/'app/src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt').read_text(encoding='utf8')
s = (root/'app/src/main/java/io/nekohasekai/sagernet/database/PreferredGroupStore.kt').read_text(encoding='utf8')
assert 'inflate(R.layout.layout_profile, parent, false)' in f
assert 'displayAddress()' not in f and 'Button(' not in f and 'node.error' not in f
assert 'preferredMemberLatency(it.status, it.ping)' in f
assert 'PreferredGroupStore.rows(groupId)' in f
assert 'getById(id)' in s and 'rows[it.id] = it' in s
assert 'selectPreferredGroup(group.id)' in c and 'confirmDashboardGroupDeletion(group, position)' in c
assert 'fun selectPreferredGroup(groupId: Long)' in c
assert 'preferredRuntimeLabel(' in c
runtime = (root/'app/src/main/java/io/nekohasekai/sagernet/ui/PreferredRuntimeSelection.kt').read_text(encoding='utf8')
assert 'getPreferredSelection(profileId)' in runtime and '待选择' in runtime
assert 'recent_tcp_success' in runtime and 'UDP 独立选择' in runtime
import re
ids = {e.get('{http://schemas.android.com/apk/res/android}id', '').split('/')[-1] for e in ET.parse(root/'app/src/main/res/layout/layout_profile.xml').iter()}
assert set(re.findall(r'R.id.(\w+)', f)) <= ids
print('PASS: compact shared layout IDs, source-ID test reuse, no address/buttons, preserved menu/delete, honest selection fallback')
