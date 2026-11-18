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

func TestAccountPermissionsAndRevocation(t *testing.T) {
	t.Setenv("STORAVIX_ADMIN_USER", "admin")
	t.Setenv("STORAVIX_ADMIN_PASSWORD", "owner-password")
	dir := t.TempDir()
	s, err := newServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.state.Network = []NetworkConfig{{ID: "net", Name: "Restricted-Switch", Host: "192.168.111.14"}}
	s.state.Locations = map[string]DeviceLocation{"switch:net": {Lat: 61, Lon: 73, Address: "Restricted-Address"}}
	s.state.Alerts = []Alert{{Subject: "Restricted-Notification"}}
	s.agents["host"] = telemetry.Report{ID: "host", Hostname: "Test-Server", ReceivedAt: time.Now()}
	s.state.Dashboard = &DashboardConfig{
		Widgets: []Widget{{ID: "raid", Type: "raid"}, {ID: "switches", Type: "network"}, {ID: "events", Type: "events"}},
	}
	router := s.routes()
	call := func(method, path, data string, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(data))
		if cookie != nil {
			r.AddCookie(cookie)
		}
		r.Header.Set("X-Storavix-Request", "1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s returned %d, want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	login := func(name, password string) *http.Cookie {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": name, "password": password})
		return call("POST", "/api/login", string(body), nil, 200).Result().Cookies()[0]
	}
	owner := login("admin", "owner-password")
	users := "/api/settings/users"
	call("POST", users, `{"username":"admin","password":"test-password","role":"admin","scope":"all"}`, owner, 403)
	call("POST", users, `{"username":"read","password":"read-password","role":"viewer","scope":"servers"}`, owner, 201)
	call("POST", users, `{"username":"Read","password":"read-password","role":"viewer","scope":"servers"}`, owner, 409)
	call("POST", users, `{"username":"network","password":"network-password","role":"operator","scope":"infrastructure"}`, owner, 201)
	call("POST", users, `{"username":"audit","password":"audit-password","role":"viewer","scope":"all"}`, owner, 201)
	call("POST", users, `{"username":"switches","password":"switches-password","role":"operator","scope":"switches"}`, owner, 201)
	call("POST", users, `{"username":"limited-admin","password":"test-password","role":"admin","scope":"servers"}`, owner, 400)
	list := call("GET", users, "", owner, 200).Body.String()
	for _, secret := range []string{"passHash", "salt", "network-password"} {
		if strings.Contains(list, secret) {
			t.Fatalf("account listing exposes credentials: %s", list)
		}
	}

	viewer := login("READ", "read-password")
	network := login("network", "network-password")
	audit := login("audit", "audit-password")
	switches := login("switches", "switches-password")
	call("GET", "/api/agents", "", switches, 403)
	call("GET", "/api/agents/host", "", switches, 403)
	call("PUT", "/api/locations/server/host", `{"lat":0,"lon":0}`, switches, 403)
	call("GET", "/api/network", "", switches, 200)
	switchData := call("GET", "/api/dashboard", "", switches, 200).Body.String()
	switchConfig := call("GET", "/api/dashboard/config", "", switches, 200).Body.String()
	if strings.Contains(switchData, "Test-Server") || !strings.Contains(switchData, "Restricted-Switch") ||
		strings.Contains(switchConfig, `"type":"raid"`) {
		t.Fatalf("switch-only overview exposes servers: %s; %s", switchData, switchConfig)
	}
	for _, path := range []string{"/api/network", "/api/network/events", "/api/settings/snmp", "/api/syslog", "/api/alerts", "/api/settings/mail", "/api/settings/syslog", users} {
		call("GET", path, "", viewer, 403)
	}
	call("GET", "/api/agents/host", "", viewer, 200)
	call("DELETE", "/api/agents/host", "", viewer, 403)
	call("PUT", "/api/dashboard/config", "{}", viewer, 403)
	call("PUT", "/api/locations/server/host", `{"lat":0,"lon":0}`, viewer, 403)
	data := call("GET", "/api/dashboard", "", viewer, 200).Body.String()
	config := call("GET", "/api/dashboard/config", "", viewer, 200).Body.String()
	if !strings.Contains(data, "Test-Server") || strings.Contains(data, "Restricted-") || strings.Contains(data, "switch:net") ||
		strings.Contains(config, `"type":"network"`) || strings.Contains(config, `"type":"events"`) {
		t.Fatalf("scoped overview: %s; config: %s", data, config)
	}
	call("GET", "/api/network", "", network, 200)
	call("POST", "/api/network/check", "", network, 202)
	call("GET", "/api/alerts", "", network, 403)
	call("PUT", "/api/locations/switch/net", `{"lat":0,"lon":0}`, network, 200)
	call("GET", "/api/alerts", "", audit, 200)
	call("POST", "/api/network/check", "", audit, 403)
	call("GET", "/api/settings/syslog", "", audit, 403)

	// Role/scope edits end only the affected user's sessions.
	call("PUT", users+"/network", `{"role":"operator","scope":"servers","password":""}`, owner, 200)
	call("GET", "/api/network", "", network, 401)
	network = login("network", "network-password")
	call("GET", "/api/network", "", network, 403)
	call("PUT", "/api/locations/switch/net", `{"lat":0,"lon":0}`, network, 403)
	call("PUT", "/api/locations/server/host", `{"lat":0,"lon":0}`, network, 200)
	call("POST", "/api/account/password", `{"old":"read-password","new":"new-reader-password"}`, viewer, 200)
	call("GET", "/api/agents/host", "", viewer, 401)
	call("GET", users, "", owner, 200)
	call("DELETE", users+"/audit", "", owner, 200)
	call("GET", "/api/alerts", "", audit, 401)
	call("DELETE", users+"/admin", "", owner, 403)

	reopened, err := newServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	persisted, ok := reopened.accountLocked("read")
	if !ok || persisted.Scope != scopeServers || !verifyAccountPassword("new-reader-password", persisted) {
		t.Fatal("user/password state did not survive reload")
	}
	if _, ok := reopened.accountLocked("audit"); ok {
		t.Fatal("deleted user survived reload")
	}
}
