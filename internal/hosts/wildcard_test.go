package hosts

import (
	"testing"
)

func TestWildcard_Match(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"*.example.com.", "foo.example.com.", true},
		{"*.example.com.", "bar.example.com.", true},
		{"*.example.com.", "example.com.", false},
		{"*.example.com.", "foo.bar.example.com.", false},
		{"*.sub.example.com.", "foo.sub.example.com.", true},
		{"*.sub.example.com.", "foo.example.com.", false},
		{"example.com.", "example.com.", true},
		{"example.com.", "foo.example.com.", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"->"+tt.name, func(t *testing.T) {
			got := wildcardMatch(tt.pattern, tt.name)
			if got != tt.want {
				t.Errorf("wildcardMatch(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
			}
		})
	}
}

func TestWildcard_Priority(t *testing.T) {
	tests := []struct {
		patterns []string
		name     string
		want     string
	}{
		{
			patterns: []string{"foo.example.com.", "*.example.com."},
			name:     "foo.example.com.",
			want:     "foo.example.com.",
		},
		{
			patterns: []string{"*.sub.example.com.", "*.example.com."},
			name:     "foo.sub.example.com.",
			want:     "*.sub.example.com.",
		},
		{
			patterns: []string{"*.example.com."},
			name:     "foo.example.com.",
			want:     "*.example.com.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectBestMatch(tt.patterns, tt.name)
			if got != tt.want {
				t.Errorf("selectBestMatch(%v, %q) = %q, want %q", tt.patterns, tt.name, got, tt.want)
			}
		})
	}
}
