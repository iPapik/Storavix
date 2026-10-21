package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iPapik/Storavix/internal/telemetry"
)

func TestAgentTokenReportsPersistAndRequireCredentials(t *testing.T) {
	t.Setenv("STORAVIX_ADMIN_PASSWORD", "abcdefgh")
	dir := t.TempDir()
	s, e := newServer(dir)
	if e != nil {
		t.Fatal(e)
	}
	tok, e := ensureAgentToken(dir)
	if e != nil || len(tok) != 64 {
		t.Fatalf("invalid token: %v", e)
	}
	if again, e := ensureAgentToken(dir); e != nil || again != tok {
		t.Fatal("token should be stable")
	}
	fi, e := os.Stat(filepath.Join(dir, "agent.token"))
	if e != nil || fi.Mode().Perm()&0077 != 0 {
		t.Fatal("token must be secret")
	}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()
	payload := `{"id":"0123456789abcdef0123456789abcdef","hostname":"srv55","os":"linux","version":"0.2.0","cpuPercent":3.25,"memoryPercent":48.1,"diskPercent":12.6,"arrays":[{"name":"md0","status":"degraded"}]}`
	post := func(token string) int {
		r, _ := http.NewRequest("POST", ts.URL+"/api/agent/report", strings.NewReader(payload))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Storavix-Request", "1")
		res, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	if code := post("wrong"); code != 401 {
		t.Fatalf("invalid token allowed: %d", code)
	}
	if code := post(tok); code != 202 {
		t.Fatalf("valid token rejected: %d", code)
	}
	if len(s.agents) != 1 || s.agents["0123456789abcdef0123456789abcdef"].Hostname != "srv55" || s.arrayCount() != 1 {
		t.Fatal("report not processed")
	}
	if len(s.agents["0123456789abcdef0123456789abcdef"].ReceivedAt.Format(time.RFC3339)) == 0 {
		t.Fatal("receivedAt missing")
	}
	s2, e := newServer(dir)
	if e != nil || len(s2.agents) != 1 {
		t.Fatal("agents lost on restart", e)
	}
	r, e := http.Get(ts.URL + "/api/agents")
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatalf("agents exposed without session: %d", r.StatusCode)
	}
}
func TestReportValidation(t *testing.T) {
	r := telemetry.Report{ID: "0123456789abcdef0123456789abcdef", Hostname: "server", OS: "linux"}
	if r.Validate() != nil {
		t.Fatal("valid report rejected")
	}
	v := 104.0
	r.CPUPercent = &v
	if r.Validate() == nil {
		t.Fatal("invalid CPU accepted")
	}
	r.CPUPercent = nil
	r.Arrays = []telemetry.Array{{Name: "md0", Status: "healthy"}}
	if r.Validate() == nil {
		t.Fatal("unsupported status accepted")
	}
	b, e := json.Marshal(r)
	if e != nil || len(b) == 0 {
		t.Fatal(e)
	}
}
