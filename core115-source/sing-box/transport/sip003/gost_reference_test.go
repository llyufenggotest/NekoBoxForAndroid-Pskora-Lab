package sip003

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

func TestGostReferencePluginWebsocketInterop(t *testing.T) {
	for _, opts := range []string{"mode=ws;host=front.invalid;path=/gost", "mode=ws;host=front.invalid;path=/gost;mux=false;tls=false"} {
		t.Run(opts, func(t *testing.T) { testGostReferenceWebsocket(t, opts) })
	}
}

func testGostReferenceWebsocket(t *testing.T, clientOpts string) {
	binary := os.Getenv("GOST_PLUGIN_REFERENCE")
	if binary == "" {
		t.Skip("set GOST_PLUGIN_REFERENCE to maskedeken/gost-plugin@870ad4e binary")
	}
	target := startTCPEcho(t)
	pluginPort := reserveTCPPort(t)
	command := exec.Command(binary)
	command.Env = append(os.Environ(),
		"SS_REMOTE_HOST=127.0.0.1",
		"SS_REMOTE_PORT="+strconv.Itoa(pluginPort),
		"SS_LOCAL_HOST=127.0.0.1",
		"SS_LOCAL_PORT="+strconv.Itoa(int(target.Port)),
		"SS_PLUGIN_OPTIONS=server;mode=ws;path=/gost;nocomp",
	)
	output, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	logOutput := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(output)
		logOutput <- string(data)
	}()
	waitTCP(t, M.ParseSocksaddr(fmt.Sprintf("127.0.0.1:%d", pluginPort)), logOutput)

	plugin, err := newGostPlugin(context.Background(), mustArgs(t, clientOpts), nil, stdDialer{}, M.ParseSocksaddr(fmt.Sprintf("127.0.0.1:%d", pluginPort)))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := plugin.DialContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	assertEcho(t, conn, []byte("maskedeken-reference-ws"))
}

func TestGostReferencePluginMuxWebsocketInterop(t *testing.T) {
	for _, opts := range []string{"mode=mws;host=front.invalid;path=/gost;mux=2", "mode=ws;host=front.invalid;path=/gost;mux=true;tls=false", "mode=ws;host=front.invalid;path=/gost;mux=0;tls=false"} {
		t.Run(opts, func(t *testing.T) { testGostReferenceMuxWebsocket(t, opts) })
	}
}

func testGostReferenceMuxWebsocket(t *testing.T, clientOpts string) {
	binary := os.Getenv("GOST_PLUGIN_REFERENCE")
	if binary == "" {
		t.Skip("set GOST_PLUGIN_REFERENCE to maskedeken/gost-plugin@870ad4e binary")
	}
	target := startTCPEcho(t)
	pluginPort := reserveTCPPort(t)
	command := exec.Command(binary)
	command.Env = append(os.Environ(),
		"SS_REMOTE_HOST=127.0.0.1",
		"SS_REMOTE_PORT="+strconv.Itoa(pluginPort),
		"SS_LOCAL_HOST=127.0.0.1",
		"SS_LOCAL_PORT="+strconv.Itoa(int(target.Port)),
		"SS_PLUGIN_OPTIONS=server;mode=mws;path=/gost;nocomp",
	)
	output, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	logOutput := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(output)
		logOutput <- string(data)
	}()
	waitTCP(t, M.ParseSocksaddr(fmt.Sprintf("127.0.0.1:%d", pluginPort)), logOutput)

	plugin, err := newGostPlugin(context.Background(), mustArgs(t, clientOpts), nil, stdDialer{}, M.ParseSocksaddr(fmt.Sprintf("127.0.0.1:%d", pluginPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer plugin.(*gostPlugin).Close()
	for _, payload := range [][]byte{[]byte("maskedeken-reference-mux-one"), []byte("maskedeken-reference-mux-two")} {
		conn, err := plugin.DialContext(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		assertEcho(t, conn, payload)
		_ = conn.Close()
	}
}

func startTCPEcho(t *testing.T) M.Socksaddr {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go acceptEcho(listener, false)
	return M.ParseSocksaddr(listener.Addr().String())
}

func reserveTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func waitTCP(t *testing.T, destination M.Socksaddr, logOutput <-chan string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", destination.String(), 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		select {
		case output := <-logOutput:
			t.Fatalf("reference exited: %s", output)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case output := <-logOutput:
		t.Fatalf("reference did not listen: %s", output)
	default:
		t.Fatal("reference did not listen")
	}
}
