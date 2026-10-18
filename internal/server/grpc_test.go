package server

import "testing"

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"https", "https://example.com", false},
		{"http with port and path", "http://127.0.0.1:2112/healthz", false},
		{"query", "https://example.com/path?q=1", false},
		{"no scheme", "example.com", true},
		{"wrong scheme", "ftp://example.com", true},
		{"empty host", "https://", true},
		{"malformed", "://bad", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateURL(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateURL(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
		})
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		v, lo, hi, want uint32
	}{
		{0, 5, 3600, 5},
		{5, 5, 3600, 5},
		{30, 5, 3600, 30},
		{3600, 5, 3600, 3600},
		{100000, 5, 3600, 3600},
	}

	for _, tt := range tests {
		if got := clamp(tt.v, tt.lo, tt.hi); got != tt.want {
			t.Fatalf("clamp(%d, %d, %d) = %d, want %d", tt.v, tt.lo, tt.hi, got, tt.want)
		}
	}
}
