package protocol

import (
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestNewSFTPClientWrapsDialFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		listener.Close()
		t.Fatalf("parse listener address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		listener.Close()
		t.Fatalf("parse listener port: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	client, err := NewSFTPClient(&ConnectionInfo{
		IP:       host,
		Port:     port,
		Username: "user",
		Password: "password",
	})
	if err == nil {
		if client != nil {
			_ = client.Close()
		}
		t.Fatal("NewSFTPClient returned nil error for a closed loopback port")
	}
	if client != nil {
		t.Fatalf("client = %#v, want nil on dial failure", client)
	}
	if !strings.Contains(err.Error(), "fail to dial") {
		t.Fatalf("error = %q, want wrapped dial failure", err)
	}
}
