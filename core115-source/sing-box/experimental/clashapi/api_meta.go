package clashapi

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/ws"
	"github.com/sagernet/ws/wsutil"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
)

// API created by Clash.Meta

func (s *Server) setupMetaAPI(r chi.Router) {
	if s.logDebug {
		r := chi.NewRouter()
		r.Put("/gc", func(w http.ResponseWriter, r *http.Request) {
			debug.FreeOSMemory()
		})
		r.Mount("/", middleware.Profiler())
	}
	r.Get("/memory", memory(s.ctx))
	r.Mount("/group", groupRouter(s))
	r.Mount("/upgrade", upgradeRouter(s))
}

type Memory struct {
	Inuse   uint64 `json:"inuse"`
	OSLimit uint64 `json:"oslimit"` // maybe we need it in the future
}

func inuseMemory() uint64 {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	return memStats.StackInuse + memStats.HeapInuse + memStats.HeapIdle - memStats.HeapReleased
}

func memory(owner context.Context) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, release := requestContext(r.Context(), owner)
		defer release()
		ctx, peerCancel := context.WithCancel(ctx)
		defer peerCancel()
		var conn net.Conn
		if r.Header.Get("Upgrade") == "websocket" {
			var err error
			var buffered *bufio.ReadWriter
			conn, buffered, _, err = ws.UpgradeHTTP(r, w)
			if err != nil {
				return
			}
			// Hijacking detaches net/http's disconnect detection. A read-only
			// pump observes EOF/close without racing the single frame writer.
			// Close unblocks it on every handler return, and always join it.
			peerDone := make(chan struct{})
			go func() {
				defer close(peerDone)
				defer peerCancel()
				for {
					header, readErr := ws.ReadHeader(buffered.Reader)
					if readErr != nil || header.OpCode == ws.OpClose {
						return
					}
					if _, readErr = io.CopyN(io.Discard, buffered.Reader, header.Length); readErr != nil {
						return
					}
				}
			}()
			defer func() { conn.Close(); <-peerDone }()
		}

		if conn == nil {
			w.Header().Set("Content-Type", "application/json")
			render.Status(r, http.StatusOK)
		}

		// Cancellation must also wake an in-progress socket/HTTP write. Join
		// the callback before returning so it cannot touch a reused writer.
		controller := http.NewResponseController(w)
		writeRetired := make(chan struct{})
		stopWrite := context.AfterFunc(ctx, func() {
			defer close(writeRetired)
			if conn != nil {
				conn.Close()
			} else {
				_ = controller.SetWriteDeadline(time.Now())
			}
		})
		defer func() {
			if !stopWrite() {
				<-writeRetired
			}
		}()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		buf := &bytes.Buffer{}
		var err error
		first := true
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			if ctx.Err() != nil {
				return
			}
			buf.Reset()

			inuse := inuseMemory()

			// make chat.js begin with zero
			// this is shit var,but we need output 0 for first time
			if first {
				first = false
				inuse = 0
			}
			if err := json.NewEncoder(buf).Encode(Memory{
				Inuse:   inuse,
				OSLimit: 0,
			}); err != nil {
				break
			}
			if ctx.Err() != nil {
				return
			}
			if conn == nil {
				_, err = w.Write(buf.Bytes())
				if err == nil {
					err = controller.Flush()
				}
			} else {
				err = wsutil.WriteServerText(conn, buf.Bytes())
			}
			if err != nil {
				break
			}
		}
	}
}
