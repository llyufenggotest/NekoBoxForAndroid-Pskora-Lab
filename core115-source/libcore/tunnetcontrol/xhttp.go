package tunnetcontrol

// TunNet's XHTTP transport appends its own per-session token to this base
// path. Persisting a pre-generated token causes the server to reject the
// resulting nested path with HTTP 400.
func GenerateXHTTPPath() (string, error) {
	return "/api/v1/sync/", nil
}
