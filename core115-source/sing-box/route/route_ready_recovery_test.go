package route

import "testing"

func recoverySnapshot(id uint64, signature string, validated bool) tunRecoverySnapshot {
	return tunRecoverySnapshot{identity: id, signature: signature, validated: validated}
}

func TestValidatedOnlyFastQualification(t *testing.T) {
	tests := []struct {
		name          string
		fromOffline   bool
		first, second tunRecoverySnapshot
		wantFast      bool
		wantReason    uint8
	}{
		{"unvalidated route-ready stays fallback", true, recoverySnapshot(8022, "cell-a", false), recoverySnapshot(8022, "cell-a", false), false, recoveryReasonFallback},
		{"validated stable", true, recoverySnapshot(8022, "cell-a", true), recoverySnapshot(8022, "cell-a", true), true, recoveryReasonValidatedFast},
		{"two samples unstable", true, recoverySnapshot(8022, "cell-a", true), recoverySnapshot(8022, "cell-b", true), false, recoveryReasonFallback},
		{"offline second sample", true, recoverySnapshot(8022, "cell-a", true), tunRecoverySnapshot{}, false, recoveryReasonFallback},
		{"direct A to B", false, recoverySnapshot(8022, "cell-a", true), recoverySnapshot(8022, "cell-a", true), false, recoveryReasonFallback},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fast, reason := classifyTunRecoveryFast(tc.fromOffline, tc.first, tc.second)
			if fast != tc.wantFast || reason != tc.wantReason {
				t.Fatalf("fast=%t reason=%d, want fast=%t reason=%d", fast, reason, tc.wantFast, tc.wantReason)
			}
		})
	}
}
