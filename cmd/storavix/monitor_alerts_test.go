package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/iPapik/Storavix/internal/telemetry"
)

func eventKinds(s *server) []string {
	kinds := make([]string, 0, len(s.state.Alerts))
	for _, event := range s.state.Alerts {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

func TestMonitorAgentTransitionsPersist(t *testing.T) {
	s := &server{dataDir: t.TempDir(), agents: make(map[string]telemetry.Report)}
	now := time.Now().UTC().Truncate(time.Second)
	r := telemetry.Report{ID: "agent", Hostname: "SRV104", ReceivedAt: now,
		Arrays: []telemetry.Array{{Name: "VMs", Controller: "controller", Group: "2", Status: "active"}}}
	s.observeAgentLocked(r)
	if len(s.state.Alerts) != 0 {
		t.Fatal("initial healthy samples should establish a baseline")
	}
	r.ReceivedAt = now.Add(time.Second)
	hot := 85.0
	r.Temperature = &hot
	r.Arrays[0].Status = "rebuilding"
	s.state.Mail.Alerts = []string{"raid_rebuild_started"}
	s.observeAgentLocked(r)
	s.observeAgentLocked(r)
	if got := eventKinds(s); !slices.Equal(got, []string{"server_hot", "raid_rebuild_started"}) {
		t.Fatalf("initial transitions: %v", got)
	}
	if s.state.Alerts[0].Delivery != "disabled" || s.state.Alerts[1].Delivery != "queued" ||
		!s.state.Alerts[1].CreatedAt.Equal(r.ReceivedAt) {
		t.Fatalf("unexpected delivery status or observation time: %+v", s.state.Alerts)
	}

	raw, err := os.ReadFile(filepath.Join(s.dataDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &s.state); err != nil {
		t.Fatal(err)
	}
	r.ReceivedAt = now.Add(2 * time.Second)
	r.Temperature = nil
	r.Arrays[0].Status = "unknown"
	s.observeAgentLocked(r)
	warm := 77.0
	r.Temperature = &warm
	r.Arrays[0].Status = "rebuilding"
	s.observeAgentLocked(r)
	if len(s.state.Alerts) != 2 {
		t.Fatal("missing or hysteresis-band samples must preserve the baseline")
	}
	cool := 74.0
	r.Temperature = &cool
	r.ReceivedAt = now.Add(3 * time.Second)
	r.Arrays[0].Status = "active"
	s.observeAgentLocked(r)
	s.agents[r.ID] = r

	// Short gaps no longer generate back-to-back down/up emails.
	s.scanOffline(r.ReceivedAt.Add(agentStaleAfter + time.Second))
	if len(s.state.Alerts) != 4 {
		t.Fatalf("a brief telemetry gap generated email events: %v", eventKinds(s))
	}
	offlineAt := r.ReceivedAt.Add(agentOfflineAfter + time.Second)
	s.scanOffline(offlineAt)
	s.scanOffline(offlineAt.Add(5 * time.Second))
	r.ReceivedAt = offlineAt.Add(10 * time.Second)
	s.observeAgentLocked(r)
	want := []string{"server_up", "server_down", "server_cooled", "raid_rebuild_finished", "server_hot", "raid_rebuild_started"}
	if got := eventKinds(s); !slices.Equal(got, want) {
		t.Fatalf("transitions after restart: %v, want %v", got, want)
	}
	if !s.state.Alerts[1].CreatedAt.Equal(offlineAt) {
		t.Fatal("outage event lost the time of detection")
	}
}

func TestMonitorSwitchAndSelectedMail(t *testing.T) {
	cfg, cert := startSMTPFixture(t, "starttls")
	cfg.CertSHA256 = certSHA256(cert)
	cfg.Alerts = []string{"switch_down"}
	s := &server{dataDir: t.TempDir(), state: State{Mail: cfg}}
	n := NetworkConfig{ID: "sw", Name: "SW-01", Generation: "initial"}
	at := time.Now().UTC().Truncate(time.Second)
	high := 75.0
	s.observeNetworkLocked(n, networkSample{status: "up", at: at, temperature: &high})
	s.observeNetworkLocked(n, networkSample{status: "down", at: at.Add(time.Second)})
	s.observeNetworkLocked(n, networkSample{status: "down", at: at.Add(2 * time.Second)})
	low := 64.0
	s.observeNetworkLocked(n, networkSample{status: "up", at: at.Add(3 * time.Second), temperature: &low})
	want := []string{"switch_up", "switch_cooled", "switch_down", "switch_hot"}
	if got := eventKinds(s); !slices.Equal(got, want) {
		t.Fatalf("switch transitions: %v, want %v", got, want)
	}
	if !s.deliverMonitorMail(context.Background()) || s.deliverMonitorMail(context.Background()) {
		t.Fatal("exactly one selected event should be delivered")
	}
	for _, event := range s.state.Alerts {
		wantDelivery := "disabled"
		if event.Kind == "switch_down" {
			wantDelivery = "sent"
		}
		if event.Delivery != wantDelivery {
			t.Fatalf("%s status = %s, want %s (%s)", event.Kind, event.Delivery, wantDelivery, event.DeliveryError)
		}
	}
}

func TestEntitySensorCelsius(t *testing.T) {
	s := entitySensor{columns: [6]int64{0, 8, 9, 1, 745, 1}, present: [6]bool{false, true, true, true, true, true}}
	if c := s.celsius(); c == nil || *c < 74.49 || *c > 74.51 {
		t.Fatalf("could not parse 74.5 °C: %v", c)
	}
	s.columns[3], s.columns[4] = -1, 75
	if c := s.celsius(); c == nil || *c != 75 {
		t.Fatal("negative precision specifies accuracy, not decimal digits")
	}
	s.columns[5] = 2
	if s.celsius() != nil {
		t.Fatal("unavailable sensors must not produce a value")
	}
}
