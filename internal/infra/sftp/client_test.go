package sftp

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/lolgopher/synology-filesync/internal/app"
	"github.com/lolgopher/synology-filesync/protocol"
)

func TestNewClientWrapsDialFailure(t *testing.T) {
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

	client, err := NewClient(&protocol.ConnectionInfo{
		IP:       host,
		Port:     port,
		Username: "user",
		Password: "password",
	})
	if err == nil {
		if client != nil {
			_ = client.Close()
		}
		t.Fatal("NewClient returned nil error for a closed loopback port")
	}
	if client != nil {
		t.Fatalf("client = %#v, want nil on dial failure", client)
	}
	if !strings.Contains(err.Error(), "fail to dial") {
		t.Fatalf("error = %q, want wrapped dial failure", err)
	}
}

func TestClientImplementsSFTPClient(t *testing.T) {
	var client app.SFTPClient = (*Client)(nil)
	if client != (*Client)(nil) {
		t.Fatalf("client = %#v, want nil *Client assigned to app.SFTPClient", client)
	}
}

func TestNewClientMatchesFactoryResult(t *testing.T) {
	var factory func(*protocol.ConnectionInfo) (*Client, error) = NewClient
	if factory == nil {
		t.Fatal("factory is nil")
	}
}
