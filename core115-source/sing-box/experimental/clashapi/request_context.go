package clashapi

import (
	"context"
	"sync"
)

// requestContext retains request values/deadlines and retires with either owner.
// No watcher goroutine remains attached to a long-lived core after a request.
func requestContext(request, owner context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(request)
	done := make(chan struct{})
	stop := context.AfterFunc(owner, func() { defer close(done); cancel() })
	if owner.Err() != nil {
		cancel()
	}
	return ctx, sync.OnceFunc(func() {
		if !stop() {
			<-done
		}
		cancel()
	})
}
