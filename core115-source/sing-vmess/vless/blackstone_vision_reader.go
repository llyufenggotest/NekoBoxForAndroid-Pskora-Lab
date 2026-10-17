package vless

import (
	"bytes"
	"errors"
	"io"
	"sync"
)

// Private stream state is bounded independently of record sizes: 21 header
// bytes and an 8192-byte discard scratch. Content is delivered into the caller.
// No chunk-reader prefetch: the inner Conn alone consumes response addons/H.
// A read lock never participates in Close, so a transport Close/deadline can
// interrupt a partial UUID/header/content/padding read.
type blackstoneVisionReader struct {
	mu               sync.Mutex
	first            bool
	header           [21]byte
	have             int
	content, padding int
	command          byte
	ended, direct    bool
	terminal         error
}

var errBlackstoneVisionUUID = errors.New("Blackstone: invalid Vision response UUID")
var errBlackstoneVisionCommand = errors.New("Blackstone: invalid Vision response command")

func (c *VisionConn) readBlackstoneVision(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s := c.blackstoneReader
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminal != nil {
		return 0, s.terminal
	}
	fail := func(err error) (int, error) { s.terminal = err; return 0, err }
	noProgress := 0
	emptyBlocks := 0
	for {
		if !s.first && s.have == 0 && s.content == 0 && s.padding == 0 {
			switch s.command {
			case commandPaddingEnd:
				s.ended = true
			case commandPaddingDirect:
				s.direct = true
			}
		}
		if s.content > 0 {
			dst := p
			if len(dst) > s.content {
				dst = dst[:s.content]
			}
			n, err := c.Conn.Read(dst)
			s.content -= n
			if err == io.EOF && (s.content > 0 || s.padding > 0) {
				err = io.ErrUnexpectedEOF
			}
			if err != nil {
				s.terminal = err
			}
			if n > 0 {
				if c.numberOfPacketToFilter > 0 {
					c.filterTLS([][]byte{dst[:n]})
				}
				return n, err
			}
			if err != nil {
				return 0, err
			}
		} else if s.padding > 0 {
			var discard [xrayChunkSize]byte
			take := s.padding
			if take > len(discard) {
				take = len(discard)
			}
			n, err := c.Conn.Read(discard[:take])
			s.padding -= n
			if err == io.EOF && s.padding > 0 {
				err = io.ErrUnexpectedEOF
			}
			if err != nil {
				return fail(err)
			}
			if n > 0 {
				noProgress = 0
				continue
			}
		} else if s.ended {
			return c.Conn.Read(p)
		} else if s.direct {
			// Drain decrypted TLS input before captured rawInput and finally the raw
			// connection. Do not advertise replaceability until BOTH buffers are empty.
			if c.input != nil && c.input.Len() > 0 {
				return c.input.Read(p)
			}
			if c.rawInput != nil && c.rawInput.Len() > 0 {
				return c.rawInput.Read(p)
			}
			c.directRead = true
			return c.directConn.Read(p)
		} else {
			need := 5
			if s.first {
				need = 21
			}
			n, err := c.Conn.Read(s.header[s.have:need])
			s.have += n
			if err != nil {
				// Process a complete header even when the transport returns data+EOF;
				// otherwise never return its accumulated bytes as application content.
				if s.have != need {
					if err == io.EOF && s.have > 0 {
						err = io.ErrUnexpectedEOF
					}
					return fail(err)
				}
				s.terminal = err
			}
			if s.have == need {
				h := s.header[:need]
				if s.first {
					if !bytes.Equal(h[:16], c.userUUID[:]) {
						return fail(errBlackstoneVisionUUID)
					}
					h = h[16:]
					s.first = false
				}
				if h[0] > commandPaddingDirect {
					return fail(errBlackstoneVisionCommand)
				}
				s.command = h[0]
				s.content = int(h[1])<<8 | int(h[2])
				s.padding = int(h[3])<<8 | int(h[4])
				s.have = 0
				// A complete header plus EOF cannot supply advertised content/padding.
				if s.terminal != nil {
					if s.content > 0 || s.padding > 0 {
						return fail(io.ErrUnexpectedEOF)
					}
					return fail(s.terminal)
				}
				noProgress = 0
				if s.content == 0 && s.command == commandPaddingContinue {
					// Includes padding-only controls; no application progress after 100
					// blocks is terminal even when the peer keeps supplying padding bytes.
					emptyBlocks++
					if emptyBlocks >= 100 {
						return fail(io.ErrNoProgress)
					}
				}
				if s.content > 0 || s.padding > 0 {
					continue
				}
			} else if n > 0 {
				noProgress = 0
				continue
			}
		}
		noProgress++
		if noProgress >= 100 {
			return fail(io.ErrNoProgress)
		}
	}
}
