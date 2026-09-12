package shadowsocks

import (
	"context"
	"fmt"
	ss "github.com/sagernet/sing-shadowsocks2"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPrivateMethodsRemainReachable(t *testing.T) {
	for _, tc := range []struct {
		method, password string
		vt               bool
	}{
		{"aes-256-gcm", "plain", false},
		{"aes-256-gcm", "token#vt", true},
		{"aes-256-gcm", "token#VT", true},
		{"aes-256-gcm", "token#vt-extra", false},
		{"aes-128-cfb", "secret#BLACKSTONE", false},
		{"2022-blake3-aes-128-gcm", "AAAAAAAAAAAAAAAAAAAAAA==#BLACKSTONE", false},
	} {
		t.Run(tc.method+tc.password, func(t *testing.T) {
			m, err := ss.CreateMethod(context.Background(), tc.method, ss.MethodOptions{Password: tc.password})
			require.NoError(t, err)
			if tc.vt {
				require.Contains(t, fmt.Sprintf("%T", m), "viewTurboMethodWrapper")
			} else {
				require.NotContains(t, fmt.Sprintf("%T", m), "viewTurboMethodWrapper")
			}
		})
	}
}
