package telemetry

import (
	"errors"
	"math"
	"regexp"
	"strings"
	"time"
)

const AgentVersion = "0.2.2"
const MaxReports = 500

// Report contains only measurements actually collected. Nil means unsupported/unavailable,
// NOT zero; in particular a null disk health value never implies a healthy RAID.
type Report struct {
	ID                 string    `json:"id"`
	Hostname           string    `json:"hostname"`
	OS                 string    `json:"os"`
	Version            string    `json:"version"`
	CPUPercent         *float64  `json:"cpuPercent"`
	MemoryPercent      *float64  `json:"memoryPercent"`
	DiskPercent        *float64  `json:"diskPercent"` // capacity usage of system filesystem, not RAID health
	NetworkBytesPerSec *float64  `json:"networkBytesPerSec"`
	LoadAverage        []float64 `json:"loadAverage,omitempty"`
	UptimeSeconds      *int64    `json:"uptimeSeconds"`
	Arrays             []Array   `json:"arrays,omitempty"`
	ReceivedAt         time.Time `json:"receivedAt"` // overwritten by server
}

type Array struct {
	Name   string `json:"name"`
	Status string `json:"status"` // active, degraded, rebuilding, inactive, unknown
	Detail string `json:"detail,omitempty"`
}

var validID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (r Report) Validate() error {
	if !validID.MatchString(r.ID) || len(r.Hostname) < 1 || len(r.Hostname) > 128 || strings.ContainsAny(r.Hostname, "\r\n<>\x00") || len(r.OS) > 48 || len(r.Version) > 32 {
		return errors.New("invalid agent identity")
	}
	for _, v := range []*float64{r.CPUPercent, r.MemoryPercent, r.DiskPercent} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 || *v > 100) {
			return errors.New("invalid utilization percentage")
		}
	}
	if r.NetworkBytesPerSec != nil && (math.IsNaN(*r.NetworkBytesPerSec) || math.IsInf(*r.NetworkBytesPerSec, 0) || *r.NetworkBytesPerSec < 0) {
		return errors.New("invalid network metric")
	}
	if r.UptimeSeconds != nil && (*r.UptimeSeconds < 0 || *r.UptimeSeconds > 100*365*24*3600) {
		return errors.New("invalid uptime")
	}
	if len(r.LoadAverage) > 3 || len(r.Arrays) > 64 {
		return errors.New("too many samples")
	}
	for _, v := range r.LoadAverage {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return errors.New("invalid load")
		}
	}
	for _, v := range r.Arrays {
		if len(v.Name) > 100 || len(v.Detail) > 300 || !map[string]bool{"active": true, "degraded": true, "rebuilding": true, "inactive": true, "unknown": true}[v.Status] {
			return errors.New("invalid array")
		}
	}
	return nil
}
