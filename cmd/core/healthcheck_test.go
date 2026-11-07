package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	tests := []struct {
		name    string
		addr    string
		status  int
		wantErr bool
	}{
		{"ok on any bind host", "0.0.0.0:" + port, http.StatusOK, false},
		{"port only", ":" + port, http.StatusOK, false},
		{"unhealthy", ":" + port, http.StatusServiceUnavailable, true},
		{"nothing listens", "127.0.0.1:1", http.StatusOK, true},
		{"bad addr", "nope", http.StatusOK, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status = tt.status
			if err := healthcheck(tt.addr); (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
