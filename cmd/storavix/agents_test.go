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
	arrays, drives := s.storageCounts()
	if len(s.agents) != 1 || s.agents["0123456789abcdef0123456789abcdef"].Hostname != "srv55" || arrays != 1 || drives != 0 {
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

func TestDeleteAgentRequiresAdminAndPersistsTombstone(t *testing.T) {
	t.Setenv("STORAVIX_ADMIN_PASSWORD", "abcdefgh")
	dir := t.TempDir()
	s, err := newServer(dir)
	if err != nil { t.Fatal(err) }
	token, err := ensureAgentToken(dir)
	if err != nil { t.Fatal(err) }
	ts := httptest.NewServer(s.routes())
	defer ts.Close()
	id := "0123456789abcdef0123456789abcdef"
	payload := `{"id":"` + id + `","hostname":"retired","os":"windows","version":"0.2.5"}`
	post := func() int {
		req, _ := http.NewRequest("POST", ts.URL+"/api/agent/report", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Storavix-Request", "1")
		resp, err := http.DefaultClient.Do(req)
		if err != nil { t.Fatal(err) }
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if got := post(); got != http.StatusAccepted { t.Fatalf("ingest: %d", got) }
	remove := func(cookie, id string) int {
		req, _ := http.NewRequest("DELETE", ts.URL+"/api/agents/"+id, nil)
		req.Header.Set("X-Storavix-Request", "1")
		if cookie != "" { req.AddCookie(&http.Cookie{Name:"storavix_session",Value:cookie}) }
		resp, err := http.DefaultClient.Do(req)
		if err != nil { t.Fatal(err) }
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if got := remove("",id); got != 401 { t.Fatalf("unauth delete: %d",got) }
	if len(s.agents)!=1 { t.Fatal("unauth delete changed data") }
	s.mu.Lock()
	s.sessions["testsession"]= userSession{AccountKey: accountKey(s.state.AdminUser), Expires: time.Now().Add(time.Hour)}
	s.mu.Unlock()
	if got:=remove("testsession","invalid");got!=400 {t.Fatalf("bad ID: %d",got)}
	if got:=remove("testsession",id);got!=200 {t.Fatalf("delete: %d",got)}
	if got:=remove("testsession",id);got!=404 {t.Fatalf("second delete: %d",got)}
	if got:=post();got!=410 {t.Fatalf("re-registration: %d",got)}
	if len(s.agents)!=0 {t.Fatal("deleted agent returned")}
	reloaded,err:=newServer(dir)
	if err!=nil {t.Fatal(err)}
	if len(reloaded.agents)!=0 || !reloaded.deletedAgents[id] {t.Fatal("tombstone did not survive restart")}
	if _,err:=os.Stat(filepath.Join(dir,"agents.json"));err!=nil {t.Fatal(err)}
}

func TestLegacyAgentDataMigratesWithoutLosingReports(t *testing.T) {
	t.Setenv("STORAVIX_ADMIN_PASSWORD", "abcdefgh")
	dir:=t.TempDir()
	id:="0123456789abcdef0123456789abcdef"
	legacy:=`{"`+id+`":{"id":"`+id+`","hostname":"legacy","os":"linux","version":"0.2.0"}}`
	if err:=os.WriteFile(filepath.Join(dir,"agents.json"),[]byte(legacy),0600);err!=nil {t.Fatal(err)}
	s,err:=newServer(dir)
	if err!=nil {t.Fatal(err)}
	if len(s.agents)!=1 {t.Fatal("legacy reports lost")}
	s.mu.Lock()
	err=s.saveAgentsLocked()
	s.mu.Unlock()
	if err!=nil {t.Fatal(err)}
	s2,err:=newServer(dir)
	if err!=nil || len(s2.agents)!=1 {t.Fatal("migration failed",err)}
}

func TestDetailedAgentReportPersistsAndCountsUniqueDrives(t *testing.T) {
	t.Setenv("STORAVIX_ADMIN_PASSWORD", "abcdefgh")
	dir := t.TempDir()
	s, err := newServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	token, err := ensureAgentToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	report := telemetry.Report{
		ID: "0123456789abcdef0123456789abcdef", Hostname: "raid-host", OS: "windows",
		MemoryBytes: 16 * 1024 * 1024 * 1024,
		Volumes:     []telemetry.Volume{{Name: "D:", Filesystem: "NTFS", SizeBytes: 10000, AvailableBytes: 5000}},
		Arrays: []telemetry.Array{
			{Name: "Data", Controller: "c0", Level: "RAID1", Status: "rebuilding", Members: []string{"d0", "d1"}},
			{Name: "Logs", Controller: "c0", Level: "RAID1", Status: "degraded", Members: []string{"d0", "d1"}},
		},
		Storage: &telemetry.Storage{
			CollectedAt: time.Now().UTC(),
			Sources:     []telemetry.Source{{Name: "storcli", Status: "ok"}},
			Controllers: []telemetry.Controller{{ID: "c0", Source: "storcli", Model: "MegaRAID 9560", Status: "active"}},
			Drives: []telemetry.Drive{
				{ID: "d0", Controller: "c0", Slot: "251:0", Serial: "SN01", Role: "member", Status: "active"},
				{ID: "d1", Controller: "c0", Slot: "251:1", Serial: "SN02", Role: "member", Status: "rebuilding"},
			},
		},
	}
	post := func() int {
		b, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(string(b)))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Storavix-Request", "1")
		res := httptest.NewRecorder()
		s.routes().ServeHTTP(res, request)
		return res.Code
	}
	if status := post(); status != http.StatusAccepted {
		t.Fatalf("could not ingest detailed report, HTTP %d", status)
	}
	report.Arrays[0].Members[0] = "missing"
	if status := post(); status != http.StatusBadRequest {
		t.Fatalf("invalid drive reference accepted, HTTP %d", status)
	}
	reopened, err := newServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	arrays, drives := reopened.storageCounts()
	persisted := reopened.agents[report.ID]
	if arrays != 2 || drives != 2 || len(persisted.Volumes) != 1 || persisted.Arrays[0].Members[0] != "d0" ||
		persisted.Storage.Drives[1].Serial != "SN02" || !persisted.Storage.CollectedAt.Equal(report.Storage.CollectedAt) {
		t.Fatalf("unexpected persisted inventory: %+v", persisted)
	}
}
