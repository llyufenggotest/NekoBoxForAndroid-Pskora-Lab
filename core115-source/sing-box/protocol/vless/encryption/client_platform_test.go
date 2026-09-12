package encryption

import "testing"

func TestClientUseAESDisablesHardwarePathOnAndroid(t *testing.T) {
	if clientUseAES("android", true) {
		t.Fatal("Android client must avoid the architecture-dependent AES-GCM handshake path")
	}
}

func TestClientUseAESPreservesHardwarePathElsewhere(t *testing.T) {
	if !clientUseAES("windows", true) {
		t.Fatal("non-Android client should preserve hardware AES-GCM")
	}
	if clientUseAES("linux", false) {
		t.Fatal("client must not enable AES-GCM without hardware support")
	}
}
