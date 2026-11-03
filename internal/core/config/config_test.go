package config

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr string
	}{
		{
			name: "defaults",
			env:  map[string]string{"DB_PATH": "/data/quoker.db"},
			want: Config{DBPath: "/data/quoker.db", HTTPAddr: ":8080", LogLevel: "info"},
		},
		{
			name: "overrides",
			env:  map[string]string{"DB_PATH": "q.db", "HTTP_ADDR": "127.0.0.1:9000", "LOG_LEVEL": "debug"},
			want: Config{DBPath: "q.db", HTTPAddr: "127.0.0.1:9000", LogLevel: "debug"},
		},
		{name: "missing db path", env: map[string]string{}, wantErr: "DB_PATH is required"},
		{name: "bad addr", env: map[string]string{"DB_PATH": "q.db", "HTTP_ADDR": "8080"}, wantErr: "HTTP_ADDR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(func(k string) string { return tt.env[k] })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
