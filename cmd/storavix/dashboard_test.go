package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iPapik/Storavix/internal/telemetry"
)

func TestDashboardSettingsAndAgentDetail(t *testing.T) {
	t.Setenv("STORAVIX_ADMIN_PASSWORD", "test-password")
	dir := t.TempDir()
	s, err := newServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.sessions["test"] = userSession{AccountKey: accountKey(s.state.AdminUser), Expires: time.Now().Add(time.Hour)}
	s.agents["0123456789abcdef0123456789abcdef"] = telemetry.Report{
		ID: "0123456789abcdef0123456789abcdef", Hostname: "SRV107", OS: "windows",
		ReceivedAt: time.Now(), Arrays: []telemetry.Array{{Name: "DATA", Status: "degraded", Level: "RAID5"}},
	}
	handler := s.routes()
	call := func(method, path, body string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Storavix-Request", "1")
		if auth {
			r.AddCookie(&http.Cookie{Name: "storavix_session", Value: "test"})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	if response := call("GET", "/api/dashboard", "", false); response.Code != 401 {
		t.Fatalf("unauthenticated monitoring API returned %d", response.Code)
	}
	if response := call("GET", "/api/agents/0123456789abcdef0123456789abcdef", "", true); response.Code != 200 ||
		!strings.Contains(response.Body.String(), "SRV107") {
		t.Fatalf("unexpected detail response: %d %s", response.Code, response.Body.String())
	}
	if response := call("GET", "/api/dashboard", "", true); response.Code != 200 ||
		!strings.Contains(response.Body.String(), `"severity":"warning"`) {
		t.Fatalf("missing active RAID alarm: %d %s", response.Code, response.Body.String())
	}
	config := defaultDashboard()
	config.Maps[0].Nodes = []MapNode{
		{ID: "server", Kind: "server", Ref: "0123456789abcdef0123456789abcdef", X: 25, Y: 30},
		{ID: "switch", Kind: "switch", Ref: "sw01", X: 75, Y: 30},
	}
	config.Maps[0].Links = []MapLink{{From: "server", To: "switch", Label: "Gi24"}}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if response := call("PUT", "/api/dashboard/config", string(encoded), true); response.Code != 200 {
		t.Fatalf("could not save dashboard: %d %s", response.Code, response.Body.String())
	}
	if response := call("PUT", "/api/dashboard/config", string(encoded), true); response.Code != 409 {
		t.Fatalf("accepted a stale update: %d %s", response.Code, response.Body.String())
	}
	restart, err := newServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	stored := restart.dashboardConfig()
	if stored.Revision != 1 || len(stored.Maps[0].Links) != 1 || stored.Maps[0].Links[0].Label != "Gi24" {
		t.Fatalf("dashboard settings missing after restart: %+v", stored)
	}
	stored.Maps[0].Links[0].To = "missing"
	if stored.validate() == nil {
		t.Fatal("map accepted a link to an unknown node")
	}
}

func TestDashboardStaleAgentDoesNotReportHealthyRAID(t *testing.T) {
	t.Setenv("STORAVIX_ADMIN_PASSWORD", "test-password")
	s, err := newServer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.agents["old"] = telemetry.Report{ID: "old", Hostname: "Old", ReceivedAt: time.Now().Add(-5 * time.Minute),
		Arrays: []telemetry.Array{{Name: "Data", Status: "active"}}}
	w := httptest.NewRecorder()
	s.getDashboardData(w, httptest.NewRequest("GET", "/", nil))
	var data DashboardData
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Arrays) != 1 || data.Arrays[0].Status != "stale" || len(data.Problems) != 1 ||
		data.Problems[0].Severity != "critical" {
		t.Fatalf("stale data shown as current: %+v", data)
	}
}
