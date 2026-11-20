package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iPapik/Storavix/internal/telemetry"
)

func TestNormalizeServerURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		raw       string
		allowHTTP bool
		want      string
		wantErr   bool
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

func TestSendRetriesWithoutOSNameForOlderServers(t *testing.T) {
	names := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report telemetry.Report
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Errorf("could not decode report: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		names <- report.OSName
		if report.OSName != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("{\"error\":\"invalid report\"}\n"))
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	if err := send(server.Client(), server.URL, "test", telemetry.Report{OS: "windows", OSName: "Windows Server 2012 R2 Standard"}); err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || <-names != "Windows Server 2012 R2 Standard" || <-names != "" {
		t.Fatal("expected full OS name on first request and legacy report on retry")
	}
}
