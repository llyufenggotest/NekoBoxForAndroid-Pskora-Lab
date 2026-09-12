"""Source integration contracts, not an Android/runtime substitute."""
from pathlib import Path
import xml.etree.ElementTree as ET
r = Path(__file__).resolve().parents[2] / 'app/src/main'
b = (r/'java/io/nekohasekai/sagernet/fmt/ConfigBuilder.kt').read_text(encoding='utf-8')
assert b.index('route.final_ = mainProxyTag') < b.index('if (!forTest && DataStore.globalMode)')
assert 'trafficMap[tag] = listOf(entity)' in b
assert 'tagMap[it.id] = buildProfile(it, it.id)' in b
assert 'val mainTag = buildProfile(proxy, 0)' in b
assert 'tagMap[key] = buildProfile(p, key)' in b
activity=(r/'java/io/nekohasekai/sagernet/ui/PreferredGroupActivity.kt').read_text(encoding='utf-8')
assert 'setResult(RESULT_OK, Intent().putExtra(ProfileSelectActivity.EXTRA_PROFILE_ID, saved.id))' in activity
assert 'WindowInsetsCompat.Type.ime()' in activity and 'confirmExit()' in activity
manifest=ET.parse(r/'AndroidManifest.xml')
assert any(a.get('{http://schemas.android.com/apk/res/android}name')=='io.nekohasekai.sagernet.ui.PreferredGroupActivity' for a in manifest.findall('.//activity'))
backup=(r/'java/io/nekohasekai/sagernet/ui/BackupFragment.kt').read_text(encoding='utf-8')
assert backup.index('validatePreferredRestoreSnapshot(profiles, groups)') < backup.index('SagerDatabase.proxyDao.reset()')
restore = (r/'java/moe/matsuri/nb4a/proxy/config/PreferredRestoreValidation.kt').read_text(encoding='utf-8')
assert 'resolver.validate(it)' in restore
assert 'SagerDatabase.instance.runInTransaction' in backup
bean=(r/'java/moe/matsuri/nb4a/proxy/config/ConfigBean.java').read_text(encoding='utf-8')
assert 'output.writeInt(3)' in bean and all(f'if (version >= {v})' in bean for v in (1, 2, 3))
print('PASS integration source contracts: final route, profile paths, no duplicate leaf attribution, result Long, IME/back, Manifest, restore validation/transaction, serialization version guards')
