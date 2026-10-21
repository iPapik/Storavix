package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/iPapik/Storavix/internal/telemetry"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func ensureAgentToken(dir string) (string, error) {
	p := filepath.Join(dir, "agent.token")
	if b, e := os.ReadFile(p); e == nil {
		return strings.TrimSpace(string(b)), nil
	} else if !errors.Is(e, os.ErrNotExist) {
		return "", e
	}
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	token := hex.EncodeToString(b)
	f, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		if errors.Is(e, os.ErrExist) {
			return ensureAgentToken(dir)
		}
		return "", e
	}
	defer f.Close()
	if _, e := f.WriteString(token + "\n"); e != nil {
		return "", e
	}
	return token, nil
}
func (s *server) loadAgents() error {
	b, e := os.ReadFile(filepath.Join(s.dataDir, "agents.json"))
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, &s.agents); e != nil {
		return e
	}
	if s.agents == nil {
		s.agents = map[string]telemetry.Report{}
	}
	return nil
}
func (s *server) saveAgentsLocked() error {
	b, e := json.Marshal(s.agents)
	if e != nil {
		return e
	}
	temp := filepath.Join(s.dataDir, "agents.tmp")
	if e = os.WriteFile(temp, b, 0600); e != nil {
		return e
	}
	return os.Rename(temp, filepath.Join(s.dataDir, "agents.json"))
}
func (s *server) arrayCount() int {
	n := 0
	for _, r := range s.agents {
		n += len(r.Arrays)
	}
	return n
}
func (s *server) onlineCount() int {
	now := time.Now()
	n := 0
	for _, r := range s.agents {
		if now.Sub(r.ReceivedAt) <= 120*time.Second {
			n++
		}
	}
	return n
}
func (s *server) getAgents(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	records := make([]telemetry.Report, 0, len(s.agents))
	for _, v := range s.agents {
		records = append(records, v)
	}
	s.mu.Unlock()
	sort.Slice(records, func(i, j int) bool {
		return strings.ToLower(records[i].Hostname) < strings.ToLower(records[j].Hostname)
	})
	if records == nil {
		records = []telemetry.Report{}
	}
	sendJSON(w, 200, records)
}
func (s *server) ingestAgent(w http.ResponseWriter, r *http.Request) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		problem(w, 401, "agent token required")
		return
	}
	provided := strings.TrimPrefix(h, "Bearer ")
	if len(provided) > 128 {
		problem(w, 401, "invalid token")
		return
	}
	expected, e := os.ReadFile(filepath.Join(s.dataDir, "agent.token"))
	if e != nil || subtle.ConstantTimeCompare([]byte(provided), []byte(strings.TrimSpace(string(expected)))) != 1 {
		problem(w, 401, "invalid agent token")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var report telemetry.Report
	if e = dec.Decode(&report); e != nil {
		problem(w, 400, "invalid report")
		return
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		problem(w, 400, "trailing JSON")
		return
	}
	if e = report.Validate(); e != nil {
		problem(w, 400, e.Error())
		return
	}
	report.ReceivedAt = time.Now().UTC()
	s.mu.Lock()
	if _, ok := s.agents[report.ID]; !ok && len(s.agents) >= telemetry.MaxReports {
		s.mu.Unlock()
		problem(w, 429, "host limit reached")
		return
	}
	s.agents[report.ID] = report
	e = s.saveAgentsLocked()
	s.mu.Unlock()
	if e != nil {
		problem(w, 500, "storage error")
		return
	}
	sendJSON(w, 202, map[string]string{"status": "accepted"})
}
