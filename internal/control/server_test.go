package control

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUnixOwnerCredentialRequiresEnrolledPeer(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root is an explicit administrator")
	}
	socket := filepath.Join(t.TempDir(), "peer.sock")
	listener, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	server := UnixServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, map[string]bool{"allowed": true}) }), "owner-token", os.Geteuid()+1)
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	client := NewClient(socket, "owner-token")
	defer client.HTTP.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e = client.Call(ctx, "GET", "/", nil, nil); e == nil {
		t.Fatal("another account's owner token was accepted")
	}
}
