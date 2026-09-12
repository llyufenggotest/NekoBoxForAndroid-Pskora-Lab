package configcompat

import (
	"context"
	"github.com/sagernet/sing-box/common/dialer"
	"testing"
)

func TestConcurrentInstanceIsolation(t *testing.T) {
	a, _, err := ConvertContext(context.Background(), []byte(`{"route":{"concurrent_dial":true}}`), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := ConvertContext(context.Background(), []byte(`{"route":{"concurrent_dial":false}}`), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !dialer.ConcurrentDialEnabled(a) || dialer.ConcurrentDialEnabled(b) || dialer.ConcurrentDialEnabled(context.Background()) {
		t.Fatal("instance setting leaked")
	}
	if dialer.ConcurrentDialEnabled(context.WithValue(a, "nb4a_no_concurrent_dial", true)) {
		t.Fatal("disable key ignored")
	}
}
