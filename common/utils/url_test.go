package utils

import (
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"https://EXAMPLE.COM/path/", "https://example.com/path", false},
		{"http://example.com#fragment", "http://example.com", false},
		{"https://example.com/path?query=1", "https://example.com/path?query=1", false},
		{"https://example.com/foo%2Fbar/", "https://example.com/foo%2Fbar", false},
		// Bug 11: scheme validation
		{"javascript:alert(1)", "", true},
		{"mailto:foo@bar.com", "", true},
		{"ftp://files.example.com", "", true},
		{"data:text/html,<h1>hi</h1>", "", true},
		// Missing host
		{"https://", "", true},
	}

	for _, test := range tests {
		actual, err := NormalizeURL(test.input)
		if test.wantErr {
			if err == nil {
				t.Errorf("NormalizeURL(%q): expected error but got %q", test.input, actual)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeURL(%q): unexpected error: %v", test.input, err)
			continue
		}
		if actual != test.expected {
			t.Errorf("NormalizeURL(%q): expected %q, got %q", test.input, test.expected, actual)
		}
	}
}

func TestGetDomain(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"https://example.com/path", "example.com", false},
		{"http://EXAMPLE.COM:8080/path", "example.com", false},
		{"https://sub.domain.org:443", "sub.domain.org", false},
		{"invalid-url", "", true},
	}

	for _, test := range tests {
		actual, err := GetDomain(test.input)
		if test.wantErr {
			if err == nil {
				t.Errorf("GetDomain(%q): expected error but got %q", test.input, actual)
			}
			continue
		}
		if err != nil {
			t.Errorf("GetDomain(%q): unexpected error: %v", test.input, err)
			continue
		}
		if actual != test.expected {
			t.Errorf("GetDomain(%q): expected %q, got %q", test.input, test.expected, actual)
		}
	}
}
