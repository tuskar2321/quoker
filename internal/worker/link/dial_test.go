package link_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	workerlink "github.com/tuskar2321/quoker/internal/worker/link"
)

// TestDialContextKeepsServerName: the TCP connection goes to the dial target
// while the TLS server name stays the host of CoreAddr.
func TestDialContextKeepsServerName(t *testing.T) {
	// httptest's certificate is valid for example.com.
	ts := httptest.NewUnstartedServer(http.NotFoundHandler())
	ts.StartTLS()
	t.Cleanup(ts.Close)
	roots := x509.NewCertPool()
	roots.AddCert(ts.Certificate())

	var mu sync.Mutex
	var sni string
	seen := make(chan struct{}, 1)
	serverTLS := &tls.Config{
		Certificates: ts.TLS.Certificates,
		MinVersion:   tls.VersionTLS13,
		GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
			mu.Lock()
			sni = h.ServerName
			mu.Unlock()
			select {
			case seen <- struct{}{}:
			default:
			}
			return nil, nil
		},
	}
	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	var dialed string
	var dmu sync.Mutex
	client := workerlink.New(workerlink.Config{
		WorkerID: "w1",
		CoreAddr: "example.com:443", // unresolvable on purpose: only the dial target is real
		TLSConfig: &tls.Config{
			RootCAs:    roots,
			MinVersion: tls.VersionTLS13,
		},
		DialContext: func(ctx context.Context, addr string) (net.Conn, error) {
			dmu.Lock()
			dialed = addr
			dmu.Unlock()
			return (&net.Dialer{}).DialContext(ctx, "tcp", lis.Addr().String())
		},
	}, quiet, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { client.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	<-seen
	mu.Lock()
	defer mu.Unlock()
	if sni != "example.com" {
		t.Errorf("SNI = %q, want example.com", sni)
	}
	dmu.Lock()
	defer dmu.Unlock()
	if dialed != "example.com:443" {
		t.Errorf("dial addr = %q, want example.com:443", dialed)
	}
}
