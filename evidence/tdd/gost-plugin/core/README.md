# gost-plugin Core TDD / wire evidence

## Scope

- Built-in SIP003 `gost-plugin`; no external process at runtime.
- Canonical `websocket` transport with `ws` alias; local-reference-compatible `wss`, `mws`, `mwss`, `tls`, `mtls` aliases.
- `h2`, `quic`, and `grpc` fail closed as unsupported.
- WebSocket compression and early data are disabled.
- Dial endpoint, HTTP Host, and TLS SNI are independent.
- Multiplexing uses `github.com/sagernet/smux`, protocol version 1, not sing-mux.
- UoT stays above the Shadowsocks/plugin dialer. Ordinary Shadowsocks and existing SIP003 plugins retain their prior path.
- Scope close, interface updates, and close-idle operations close gost smux sessions.

## References

- `maskedeken/gost-plugin` commit `870ad4e3e01fe642761b8c840a940051bb9887e0`.
- `MetaCubeX/mihomo` tag `v1.19.24`, commit `a84724665eb7f989809abe463c05f5723bd24975`.
- Reference binary was built locally from the pinned maskedeken commit and used only by tests on loopback.

## Verification

- `focused-tests.log`: SIP003 and Shadowsocks unit/integration tests.
- `reference-wire.log`: loopback wire interop with the pinned maskedeken server for WS binary stream and MWS/smux v1, two streams.
- `scoped-tests.log`: all transport packages plus Shadowsocks packages.
- `preexisting-snell-gate.log`: confirms the unrelated full-suite Snell uTLS tests pass with their required `with_utls` tag.

A plain untagged `go test ./...` reaches the pre-existing Snell OIX tests and fails because that build omits uTLS; the changed packages pass, and the failing Snell package passes with `-tags with_utls`.

Race testing was not available because the Windows Go environment reports `-race requires cgo`.
