package main

import (
	"testing"
)

func TestParseServe(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		args    []string
		wantErr bool
		wantTok string
	}{
		{"token from env", "1000001:DEV", nil, false, "1000001:DEV"},
		{"token from flag wins", "", []string{"-token", "2000002:FLAG"}, false, "2000002:FLAG"},
		{"token missing", "", nil, true, ""},
		{"stray argument", "1000001:DEV", []string{"extra"}, true, ""},
		{"unknown flag", "1000001:DEV", []string{"-nope"}, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("FAKE_TELEGRAM_TOKEN", tt.env)
			o, err := parseServe(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error=%v", err, tt.wantErr)
			}
			if err == nil && o.token != tt.wantTok {
				t.Fatalf("token = %q, want %q", o.token, tt.wantTok)
			}
		})
	}
}
