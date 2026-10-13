package certificate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/fswatch"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/stretchr/testify/require"
)

func TestCertificateWatcherProductionFailureRollsBack(t *testing.T) {
	directory := t.TempDir()
	certificatePath := filepath.Join(directory, "certificate.pem")
	require.NoError(t, os.WriteFile(certificatePath, []byte("initial"), 0o600))
	var callbacks atomic.Int32
	watcher, err := fswatch.NewWatcher(fswatch.Options{
		Path: []string{certificatePath},
		Callback: func(string) {
			callbacks.Add(1)
		},
	})
	require.NoError(t, err)
	injected := errors.New("injected after certificate watcher start")
	hookReached := false
	store := &Store{
		watcher: watcher,
		startFailure: func(stage adapter.StartStage) error {
			require.Equal(t, adapter.StartStateStart, stage)
			hookReached = true
			return injected
		},
	}
	scope := adapter.NewScope(context.Background(), log.NewNOPFactory().NewLogger("certificate-rollback"))
	require.ErrorIs(t, scope.Start("certificate", store, adapter.StartStateStart), injected)
	require.True(t, hookReached, "failure must occur after the production watcher Start and cleanup registration")
	require.NoError(t, scope.Close())
	require.NoError(t, scope.Close(), "rollback is idempotent")

	require.NoError(t, os.WriteFile(certificatePath, []byte("after rollback"), 0o600))
	time.Sleep(2 * fswatch.DefaultWaitTimeout)
	require.Zero(t, callbacks.Load(), "closed watcher must not observe writes after rollback")
}
