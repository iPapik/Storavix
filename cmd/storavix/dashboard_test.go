package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	config.Widgets[0].Layout = &WidgetLayout{X: 1, Y: 2, W: 6, H: 4}
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
	if stored.Revision != 1 || stored.Widgets[0].Layout == nil || stored.Widgets[0].Layout.X != 1 {
		t.Fatalf("dashboard settings missing after restart: %+v", stored)
	}
	stored.Widgets[0].Type = "map"
	if stored.validate() == nil {
		t.Fatal("accepted removed topology widget")
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
	r := httptest.NewRequest("GET", "/", nil)
	s.getDashboardData(w, r.WithContext(context.WithValue(r.Context(), accountContextKey{}, Account{Scope: scopeAll})))
	var data DashboardData
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Arrays) != 1 || data.Arrays[0].Status != "stale" ||
		len(data.Hosts)!=1 || data.Hosts[0].Status!="stale" || len(data.Problems)!=0 {
		t.Fatalf("brief gap must be stale, not critical: %+v", data)
	}
	s.agents["old"]=telemetry.Report{ID:"old",Hostname:"Old",ReceivedAt:time.Now().Add(-11*time.Minute),
		Arrays:[]telemetry.Array{{Name:"Data",Status:"active"}}}
	w=httptest.NewRecorder()
	s.getDashboardData(w,r.WithContext(context.WithValue(r.Context(),accountContextKey{},Account{Scope:scopeAll})))
	if err:=json.Unmarshal(w.Body.Bytes(),&data);err!=nil{t.Fatal(err)}
	if data.Hosts[0].Status!="down" || len(data.Problems)!=1 || data.Problems[0].Severity!="critical" {
		t.Fatalf("confirmed offline not shown: %+v",data)
	}
}

func TestDashboardPropagatesDiskIssuesToArrayAndHost(t *testing.T) {
	s := &server{agents: map[string]telemetry.Report{}}
	for _, tc := range []struct{ disk, host, alarm string }{
		{"degraded", "warning", "warning"},
		{"inactive", "critical", "critical"},
	} {
		s.agents["srv"] = telemetry.Report{
			ID: "srv", Hostname: "SRV104", ReceivedAt: time.Now(),
			Arrays: []telemetry.Array{
				{Name: "VMs", Status: "active", Members: []string{"bad"}},
				{Name: "Backup", Status: "active", Members: []string{"good"}},
			},
			Storage: &telemetry.Storage{Drives: []telemetry.Drive{
				{ID: "bad", Slot: "252:10", Status: tc.disk}, {ID: "good", Slot: "252:9", Status: "active"},
			}},
		}
		req := httptest.NewRequest("GET", "/api/dashboard", nil)
		response := httptest.NewRecorder()
		s.getDashboardData(response, req.WithContext(context.WithValue(req.Context(), accountContextKey{}, Account{Scope: scopeServers})))
		var data DashboardData
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if len(data.Hosts) != 1 || data.Hosts[0].Status != tc.host || len(data.Arrays) != 2 ||
			data.Arrays[0].Status != "active" || data.Arrays[0].MemberIssue ||
			data.Arrays[1].Status != tc.disk || !data.Arrays[1].MemberIssue ||
			len(data.Problems) != 1 || data.Problems[0].Severity != tc.alarm {
			t.Fatalf("bad RAID status for %s: %+v", tc.disk, data)
		}
	}
}

func TestDashboardIncludesAllNineServersWithMetadata(t *testing.T) {
	s:=&server{agents:map[string]telemetry.Report{}}
	cpu,memory:=12.5,50.0
	for i:=0;i<9;i++{
		id:=fmt.Sprintf("id%02d",i)
		s.agents[id]=telemetry.Report{ID:id,Hostname:fmt.Sprintf("SRV%02d",i),
			OSName:"Microsoft Windows Server 2019 Datacenter",Version:"0.2.23",
			ReceivedAt:time.Now(),CPUPercent:&cpu,MemoryPercent:&memory}
	}
	r:=httptest.NewRequest("GET","/api/dashboard",nil)
	w:=httptest.NewRecorder()
	s.getDashboardData(w,r.WithContext(context.WithValue(r.Context(),accountContextKey{},Account{Scope:scopeAll})))
	var result DashboardData
	if err:=json.Unmarshal(w.Body.Bytes(),&result);err!=nil{t.Fatal(err)}
	if len(result.Hosts)!=9 ||result.Hosts[8].Name!="SRV08" ||
		result.Hosts[8].AgentVersion!="0.2.23" ||result.Hosts[8].CPUPercent==nil ||
		result.Hosts[8].ReceivedAt.IsZero() {
		t.Fatalf("missing host/metadata from dashboard: %+v",result.Hosts)
	}
}
