package configcompat

import "testing"

// Regression of ConfigBuilder's mandatory legacy DNS output. No Internet queries.
func TestAndroidDNSFormerBlockerStart(t *testing.T) {
	startLegacy(t, `{"dns":{"servers":[{"tag":"dns-block","address":"rcode://success"},{"tag":"dns-local","address":"local","detour":"direct"},{"tag":"dns-direct","address":"223.5.5.5","address_resolver":"dns-local","strategy":"ipv4_only"}],"final":"dns-direct"},"outbounds":[{"type":"direct","tag":"direct"}]}`)
}
