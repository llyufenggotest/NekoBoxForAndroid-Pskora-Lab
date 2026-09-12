//go:build with_utls

package tls

import (
	"context"
	"encoding/base64"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
	"github.com/stretchr/testify/require"
	"net"
	"testing"
)

func TestShanlianDynamicSNIAndOrdinaryIsolation(t *testing.T) {
	for _, name := range []string{"MAGIC_SHANLIAN_TRIGGER", "ordinary.example"} {
		cfg, err := NewUTLSClient(context.Background(), logger.NOP(), "127.0.0.1", option.OutboundTLSOptions{ServerName: name, UTLS: &option.OutboundUTLSOptions{Enabled: true, Fingerprint: "chrome"}})
		require.NoError(t, err)
		left, right := net.Pipe()
		defer left.Close()
		defer right.Close()
		first, err := cfg.Client(left)
		require.NoError(t, err)
		second, err := cfg.Client(left)
		require.NoError(t, err)
		a := first.(*utlsALPNWrapper).UConn
		b := second.(*utlsALPNWrapper).UConn
		require.NoError(t, a.BuildHandshakeState())
		require.NoError(t, b.BuildHandshakeState())
		if name == "ordinary.example" {
			require.Equal(t, name, a.HandshakeState.Hello.ServerName)
		} else {
			require.NotEqual(t, a.HandshakeState.Hello.ServerName, b.HandshakeState.Hello.ServerName)
			data, err := base64.StdEncoding.DecodeString(a.HandshakeState.Hello.ServerName)
			require.NoError(t, err)
			require.Len(t, data, 36)
		}
		require.Equal(t, name, cfg.ServerName())
	}
}
