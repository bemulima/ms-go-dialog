package filescan

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestClamAVScan_ContextCancellationInterruptsBlockedConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	accepted := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = connection.Close() }()
		close(accepted)
		_, _ = io.Copy(io.Discard, connection)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- (ClamAV{Address: listener.Addr().String(), Timeout: 5 * time.Second}).Scan(ctx, []byte("payload"))
	}()
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("scanner did not connect")
	}
	started := time.Now()
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled scan succeeded")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("context cancellation did not interrupt scan")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("scan cancellation took %s", elapsed)
	}
	_ = listener.Close()
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("test server did not stop")
	}
}
