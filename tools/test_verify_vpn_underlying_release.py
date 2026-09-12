import importlib.util
import pathlib
import unittest

SCRIPT = pathlib.Path(__file__).with_name("verify_vpn_underlying_release.py")
spec = importlib.util.spec_from_file_location("verify_vpn_underlying_release", SCRIPT)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class VerifyVpnUnderlyingReleaseTest(unittest.TestCase):
    def test_rejects_release_emit_folded_to_return(self):
        folded = """
invoke-virtual {v0, v2, v6}, Lio/nekohasekai/sagernet/bg/VpnUnderlyingLiveDiagnostic;.recordConnectivityCallback:(ILandroid/net/Network;)V
invoke-virtual {v4, v1, v2}, Lio/nekohasekai/sagernet/bg/VpnUnderlyingLiveDiagnostic;.recordLiveSet:(Landroid/net/Network;Lkotlin/jvm/functions/Function1;)V
Class descriptor  : 'Lio/nekohasekai/sagernet/bg/VpnUnderlyingLiveDiagnostic;'
name          : 'emit'
const/16 v1, #int 512
const-string v0, "VPNUNDERTRACE"
0000: return-void
"""
        with self.assertRaisesRegex(AssertionError, "Log.i"):
            module.verify_dump(folded)

    def test_accepts_bounded_log_i_and_live_call_sites(self):
        retained = """
invoke-virtual {v0, v2, v6}, Lio/nekohasekai/sagernet/bg/VpnUnderlyingLiveDiagnostic;.recordConnectivityCallback:(ILandroid/net/Network;)V
invoke-virtual {v4, v1, v2}, Lio/nekohasekai/sagernet/bg/VpnUnderlyingLiveDiagnostic;.recordLiveSet:(Landroid/net/Network;Lkotlin/jvm/functions/Function1;)V
Class descriptor  : 'Lio/nekohasekai/sagernet/bg/VpnUnderlyingLiveDiagnostic;'
name          : 'emit'
const/16 v1, #int 512
const-string v0, "VPNUNDERTRACE"
invoke-static {v0, v3}, Landroid/util/Log;.i:(Ljava/lang/String;Ljava/lang/String;)I
return-void
"""
        module.verify_dump(retained)


if __name__ == "__main__":
    unittest.main()
