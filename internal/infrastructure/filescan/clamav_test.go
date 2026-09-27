package filescan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
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

func TestClamAVScanUsesINSTREAMAndMapsResponses(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), (64<<10)+(6<<10))
	tests := []struct {
		name     string
		response string
		wantErr  error
	}{
		{name: "clean", response: "stream: OK"},
		{name: "infected", response: "stream: Eicar-Test-Signature FOUND", wantErr: domain.ErrFileInfected},
		{name: "malformed response", response: "clamd returned an unknown result", wantErr: domain.ErrFileScanUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := scanWithINSTREAMContractServer(t, payload, test.response)
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("scan error=%v, want success", err)
				}
				return
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("scan error=%v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestClamAVScanUnavailableFailsClosed(t *testing.T) {
	err := (ClamAV{Address: "127.0.0.1:0", Timeout: time.Second}).Scan(context.Background(), []byte("payload"))
	if !errors.Is(err, domain.ErrFileScanUnavailable) {
		t.Fatalf("scan error=%v, want scanner unavailable", err)
	}
}

func scanWithINSTREAMContractServer(t *testing.T, expectedPayload []byte, response string) error {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	serverResult := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverResult <- acceptErr
			return
		}
		defer func() { _ = connection.Close() }()
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(connection)
		command, readErr := reader.ReadString(0)
		if readErr != nil {
			serverResult <- readErr
			return
		}
		if command != "zINSTREAM\x00" {
			serverResult <- errors.New("unexpected ClamAV command")
			return
		}
		var received bytes.Buffer
		chunks := 0
		for {
			var header [4]byte
			if _, readErr = io.ReadFull(reader, header[:]); readErr != nil {
				serverResult <- readErr
				return
			}
			size := binary.BigEndian.Uint32(header[:])
			if size == 0 {
				break
			}
			if size > 64<<10 {
				serverResult <- errors.New("INSTREAM chunk exceeds 64 KiB")
				return
			}
			chunk := make([]byte, size)
			if _, readErr = io.ReadFull(reader, chunk); readErr != nil {
				serverResult <- readErr
				return
			}
			chunks++
			_, _ = received.Write(chunk)
		}
		if chunks < 2 || !bytes.Equal(received.Bytes(), expectedPayload) {
			serverResult <- errors.New("INSTREAM payload framing mismatch")
			return
		}
		_, writeErr := io.WriteString(connection, response+"\x00")
		serverResult <- writeErr
	}()

	scanErr := (ClamAV{Address: listener.Addr().String(), Timeout: 2 * time.Second}).Scan(context.Background(), expectedPayload)
	select {
	case serverErr := <-serverResult:
		if serverErr != nil {
			t.Fatalf("INSTREAM contract server: %v", serverErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("INSTREAM contract server did not finish")
	}
	return scanErr
}
