package originca

import (
	"context"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/stretchr/testify/require"
)

func TestWaitForDoneBlocksScopeCloseUntilCompletion(t *testing.T) {
	done := make(chan struct{})
	scope := adapter.NewScope(context.Background(), log.NewNOPFactory().NewLogger("test"))
	scope.Add(waitForDone(done))

	closed := make(chan error, 1)
	go func() {
		closed <- scope.Close()
	}()

	select {
	case err := <-closed:
		t.Fatalf("scope close returned before refresh loop completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(done)
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("scope close leaked after refresh loop completed")
	}
}
