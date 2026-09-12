package tunnetcontrol

import "testing"

func TestGenerateXHTTPPathReturnsTransportBasePath(t *testing.T) {
	for iteration := 0; iteration < 10; iteration++ {
		path, err := GenerateXHTTPPath()
		if err != nil {
			t.Fatal(err)
		}
		if path != "/api/v1/sync/" {
			t.Fatalf("unexpected TunNet XHTTP base path: %q", path)
		}
	}
}
