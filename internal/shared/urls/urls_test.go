package urls

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

type vector struct {
	Name     string `json:"name"`
	Input    string `json:"input"`
	Error    string `json:"error"`
	Platform string `json:"platform"`
	VideoID  string `json:"video_id"`
	Short    bool   `json:"short"`
	Key      string `json:"key"`
	URL      string `json:"url"`
}

func loadVectors(t *testing.T) []vector {
	t.Helper()
	b, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Comment string   `json:"comment"`
		Vectors []vector `json:"vectors"`
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		t.Fatalf("decode vectors: %v", err)
	}
	if len(f.Vectors) == 0 {
		t.Fatal("no vectors")
	}
	return f.Vectors
}

func TestParseVectors(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range loadVectors(t) {
		if seen[v.Name] {
			t.Fatalf("duplicate vector name %q", v.Name)
		}
		seen[v.Name] = true
		t.Run(v.Name, func(t *testing.T) {
			got, err := Parse(v.Input)
			if v.Error != "" {
				if err == nil || err.Error() != v.Error {
					t.Fatalf("Parse(%q) = %+v, %v; want error %s", v.Input, got, err, v.Error)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): %v", v.Input, err)
			}
			if string(got.Platform) != v.Platform || got.VideoID() != v.VideoID ||
				got.Short() != v.Short || got.Key != v.Key || got.URL != v.URL {
				t.Fatalf("Parse(%q) =\n  platform=%s video_id=%q short=%v key=%s url=%s\nwant\n  platform=%s video_id=%q short=%v key=%s url=%s",
					v.Input, got.Platform, got.VideoID(), got.Short(), got.Key, got.URL,
					v.Platform, v.VideoID, v.Short, v.Key, v.URL)
			}
			// A normalized URL must normalize to itself (stable for aliases).
			again, err := Normalize(got.URL)
			if err != nil || again != got {
				t.Fatalf("Normalize(%q) = %+v, %v; want %+v", got.URL, again, err, got)
			}
		})
	}
}

func TestNormalizeTooLong(t *testing.T) {
	long := "https://www.tiktok.com/@" + strings.Repeat("a", MaxLen) + "/video/1"
	if _, err := Normalize(long); !errors.Is(err, ErrUnsupportedURL) {
		t.Fatalf("err = %v, want ErrUnsupportedURL", err)
	}
}
