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
			want: Config{DBPath: "/data/quoker.db", HTTPAddr: ":8080", FilesAddr: ":8081", GRPCAddr: ":9090",
				CacheDir: "./cache", CacheMaxBytes: 3 << 30, ReleasesDir: "./releases", LogLevel: "info"},
		},
		{
			name: "overrides",
			env: map[string]string{"DB_PATH": "q.db", "HTTP_ADDR": "127.0.0.1:9000", "FILES_ADDR": "127.0.0.1:9001",
				"GRPC_ADDR": "127.0.0.1:9091", "CACHE_DIR": "/cache", "CACHE_MAX_BYTES": "1000",
				"UPLOAD_BASE": "http://127.0.0.1:9001/internal/v1", "DOMAIN": "ignored.example", "LOG_LEVEL": "debug",
				"RELEASES_DIR": "/releases", "PUBLIC_URL": "http://127.0.0.1:9000/"},
			want: Config{DBPath: "q.db", HTTPAddr: "127.0.0.1:9000", FilesAddr: "127.0.0.1:9001", GRPCAddr: "127.0.0.1:9091",
				CacheDir: "/cache", CacheMaxBytes: 1000, UploadBase: "http://127.0.0.1:9001/internal/v1", LogLevel: "debug",
				ReleasesDir: "/releases", PublicURL: "http://127.0.0.1:9000"},
		},
		{
			name: "upload base from domain",
			env:  map[string]string{"DB_PATH": "q.db", "DOMAIN": "quoker.example"},
			want: Config{DBPath: "q.db", HTTPAddr: ":8080", FilesAddr: ":8081", GRPCAddr: ":9090", CacheDir: "./cache",
				CacheMaxBytes: 3 << 30, UploadBase: "https://w.quoker.example/internal/v1", LogLevel: "info",
				ReleasesDir: "./releases", PublicURL: "https://api.quoker.example"},
		},
		{name: "bad cache size", env: map[string]string{"DB_PATH": "q.db", "CACHE_MAX_BYTES": "lots"}, wantErr: "CACHE_MAX_BYTES"},
		{name: "missing db path", env: map[string]string{}, wantErr: "DB_PATH is required"},
		{name: "bad http addr", env: map[string]string{"DB_PATH": "q.db", "HTTP_ADDR": "8080"}, wantErr: "HTTP_ADDR"},
		{name: "bad grpc addr", env: map[string]string{"DB_PATH": "q.db", "GRPC_ADDR": "9090"}, wantErr: "GRPC_ADDR"},
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
