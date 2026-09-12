# Source provenance

Files in this directory (except private.go) are copied from SagerNet/sing-quic `v0.7.1-0.20260904135313-497364e8ee3e/congestion_meta2`, the same version required by this core. They retain their source headers. `private.go` adds an initial packet-window constructor for legacy XHTTP `cwnd` without changing the global upstream QUIC dependency or ignoring this option. Directory name denotes its private compatibility caller, not an old quic implementation.
