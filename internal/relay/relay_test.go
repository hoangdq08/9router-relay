package relay

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestConfigValidation(t *testing.T) {
	valid := Config{ListenAddr: "127.0.0.1:0", UpstreamAddr: "127.0.0.1:1", DialTimeout: time.Second}
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing listen", Config{UpstreamAddr: valid.UpstreamAddr, DialTimeout: valid.DialTimeout}},
		{"missing upstream", Config{ListenAddr: valid.ListenAddr, DialTimeout: valid.DialTimeout}},
		{"non-positive timeout", Config{ListenAddr: valid.ListenAddr, UpstreamAddr: valid.UpstreamAddr}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewServer(tc.cfg, testLogger()); err == nil {
				t.Fatal("NewServer succeeded for invalid config")
			}
		})
	}
	if _, err := NewServer(valid, testLogger()); err != nil {
		t.Fatalf("NewServer(valid) error: %v", err)
	}
}

func TestProxyCopiesBothDirections(t *testing.T) {
	client, clientPeer := net.Pipe()
	upstream, upstreamPeer := net.Pipe()
	defer client.Close()
	defer clientPeer.Close()
	defer upstream.Close()
	defer upstreamPeer.Close()

	done := make(chan struct{})
	go func() {
		proxy(context.Background(), client, upstream)
		close(done)
	}()

	const clientMessage = "client-to-upstream"
	go func() {
		_, _ = clientPeer.Write([]byte(clientMessage))
	}()
	buf := make([]byte, len(clientMessage))
	if _, err := io.ReadFull(upstreamPeer, buf); err != nil {
		t.Fatalf("read client-to-upstream: %v", err)
	}
	if string(buf) != clientMessage {
		t.Fatalf("client-to-upstream = %q, want %q", buf, clientMessage)
	}

	const upstreamMessage = "upstream-to-client"
	go func() {
		_, _ = upstreamPeer.Write([]byte(upstreamMessage))
	}()
	buf = make([]byte, len(upstreamMessage))
	if _, err := io.ReadFull(clientPeer, buf); err != nil {
		t.Fatalf("read upstream-to-client: %v", err)
	}
	if string(buf) != upstreamMessage {
		t.Fatalf("upstream-to-client = %q, want %q", buf, upstreamMessage)
	}

	_ = clientPeer.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not stop after client close")
	}
}

func TestServerForwardsTCP(t *testing.T) {
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstreamListener.Close()

	upstreamDone := make(chan error, 1)
	go func() {
		conn, err := upstreamListener.Accept()
		if err != nil {
			upstreamDone <- err
			return
		}
		defer conn.Close()
		_, err = io.Copy(conn, conn)
		upstreamDone <- err
	}()

	server, err := NewServer(Config{
		ListenAddr:   "127.0.0.1:0",
		UpstreamAddr: upstreamListener.Addr().String(),
		DialTimeout:  time.Second,
	}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for server.Addr() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if server.Addr() == nil {
		t.Fatal("server did not start listening")
	}

	client, err := net.Dial("tcp", server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	const message = "relay-e2e"
	if _, err := client.Write([]byte(message)); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len(message))
	if _, err := io.ReadFull(client, response); err != nil {
		t.Fatal(err)
	}
	if string(response) != message {
		t.Fatalf("response = %q, want %q", response, message)
	}
	_ = client.Close()

	cancel()
	select {
	case err := <-serveDone:
		if !errors.Is(err, ErrServerClosed) {
			t.Fatalf("Serve error = %v, want ErrServerClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
	select {
	case err := <-upstreamDone:
		if err != nil && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("upstream error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream handler did not stop")
	}
}
