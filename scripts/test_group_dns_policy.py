"""Fallback wiring checks; not a replacement for real Android builder tests."""
import pathlib
import unittest
ROOT = pathlib.Path(__file__).resolve().parents[1]

class GroupDnsWiringTest(unittest.TestCase):
    def test_builder_uses_leaf_ownership_in_all_modes(self):
        src = (ROOT / 'app/src/main/java/io/nekohasekai/sagernet/fmt/ConfigBuilder.kt').read_text()
        self.assertIn('serverDnsResolverFor(bean, ownerGroup)', src)
        self.assertIn('val ownerGid = proxyEntity.groupId', src)
        self.assertNotIn('dns-airport', src)
    def test_tcp_uses_shared_precedence(self):
        src = (ROOT / 'app/src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt').read_text()
        self.assertIn('serverDnsResolverFor(bean, SagerDatabase.groupDao.getById(profile.groupId))', src)
    def test_oix_dns_is_user_configured_only(self):
        policy = (ROOT / 'app/src/main/java/io/nekohasekai/sagernet/fmt/GroupDnsPolicy.kt').read_text()
        builder = (ROOT / 'app/src/main/java/io/nekohasekai/sagernet/fmt/ConfigBuilder.kt').read_text()
        for source in (policy, builder):
            self.assertNotIn('124.221.68.73', source)
            self.assertNotIn('OIX_MANAGED_DNS_ADDRESS', source)
            self.assertNotIn('dns-oix-managed', source)
        self.assertNotIn('usesOixManagedDns(bean)', builder)
        self.assertIn('groupServerDnsResolver(group)', policy)
        self.assertIn('if (resolver != null)', builder)
    def test_import_does_not_pre_resolve_managed_oix(self):
        src = (ROOT / 'app/src/main/java/io/nekohasekai/sagernet/group/GroupUpdater.kt').read_text()
        self.assertIn('usesOixManagedDns(profile)', src)

if __name__ == '__main__':
    unittest.main()
