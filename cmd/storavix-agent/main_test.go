package main

import "testing"

func TestNormalizeServerURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw string
		allowHTTP bool
		want string
		wantErr bool
	}{
		{"https bare", "https://monitor.example.org:8080", false, "https://monitor.example.org:8080", false},
		{"https slash", "https://monitor.example.org:8080/", false, "https://monitor.example.org:8080", false},
		{"http trusted bare", "http://192.168.111.130:8080", true, "http://192.168.111.130:8080", false},
		{"http trusted slash Windows wizard", "http://192.168.111.130:8080/", true, "http://192.168.111.130:8080", false},
		{"spaces around input", " http://127.0.0.1:8080/ ", true, "http://127.0.0.1:8080", false},
		{"insecure denied", "http://192.168.111.130:8080/", false, "", true},
		{"no scheme", "192.168.111.130:8080", true, "", true},
		{"path denied", "https://monitor.example.org/api", false, "", true},
		{"double slash denied", "https://monitor.example.org//", false, "", true},
		{"query denied", "https://monitor.example.org/?secret=x", false, "", true},
		{"empty query denied", "https://monitor.example.org/?", false, "", true},
		{"fragment denied", "https://monitor.example.org/#abc", false, "", true},
		{"credentials denied", "https://user:secret@monitor.example.org/", false, "", true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeServerURL(test.raw, test.allowHTTP)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeServerURL(%q) err=%v, wantErr=%t", test.raw, err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("normalizeServerURL(%q)=%q, want %q", test.raw, got, test.want)
			}
		})
	}
}
