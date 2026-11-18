package telemetry

import (
	"errors"
	"math"
	"regexp"
	"strings"
	"time"
)

const AgentVersion = "0.2.14"
const MaxReports = 500
const MaxReportBytes = 1024 * 1024

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
	MemoryBytes        uint64    `json:"memoryBytes,omitempty"`
	MemoryAvailable    *uint64   `json:"memoryAvailable,omitempty"`
	Volumes            []Volume  `json:"volumes,omitempty"`
	Arrays             []Array   `json:"arrays,omitempty"`
	Storage            *Storage  `json:"storage,omitempty"`
	ReceivedAt         time.Time `json:"receivedAt"` // overwritten by server
}

type Volume struct {
	Name           string `json:"name"`
	Filesystem     string `json:"filesystem,omitempty"`
	SizeBytes      uint64 `json:"sizeBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

// Array describes a logical RAID device. Member IDs refer to entries in Storage.Drives.
type Array struct {
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	Detail     string   `json:"detail,omitempty"`
	Level      string   `json:"level,omitempty"`
	Controller string   `json:"controller,omitempty"`
	Group      string   `json:"group,omitempty"`
	SizeBytes  uint64   `json:"sizeBytes,omitempty"`
	Expected   int      `json:"expected,omitempty"`
	Missing    int      `json:"missing,omitempty"`
	Progress   *float64 `json:"progress,omitempty"`
	Members    []string `json:"members,omitempty"`
}

type Storage struct {
	CollectedAt time.Time    `json:"collectedAt"`
	Sources     []Source     `json:"sources,omitempty"`
	Controllers []Controller `json:"controllers,omitempty"`
	Drives      []Drive      `json:"drives,omitempty"`
}

type Source struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok, partial, error, unavailable
}

type Controller struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	Model    string `json:"model"`
	Serial   string `json:"serial,omitempty"`
	Firmware string `json:"firmware,omitempty"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
}

type Drive struct {
	ID          string   `json:"id"`
	Controller  string   `json:"controller"`
	Slot        string   `json:"slot,omitempty"`
	Device      string   `json:"device,omitempty"`
	Model       string   `json:"model,omitempty"`
	Serial      string   `json:"serial,omitempty"`
	Firmware    string   `json:"firmware,omitempty"`
	Interface   string   `json:"interface,omitempty"`
	Media       string   `json:"media,omitempty"`
	Role        string   `json:"role,omitempty"`
	Status      string   `json:"status"`
	Detail      string   `json:"detail,omitempty"`
	SizeBytes   uint64   `json:"sizeBytes,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	Progress    *float64 `json:"progress,omitempty"`
}

const (
	MaxArrays      = 256
	MaxControllers = 64
	MaxDrives      = 512
	MaxVolumes     = 128
)

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
	if len(r.LoadAverage) > 3 || len(r.Arrays) > MaxArrays || len(r.Volumes) > MaxVolumes {
		return errors.New("too many samples")
	}
	for _, v := range r.LoadAverage {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return errors.New("invalid load")
		}
	}
	if r.MemoryAvailable != nil && *r.MemoryAvailable > r.MemoryBytes {
		return errors.New("invalid available memory")
	}
	for _, v := range r.Volumes {
		if !shortText(v.Name, v.Filesystem) || v.AvailableBytes > v.SizeBytes {
			return errors.New("invalid volume")
		}
	}
	for _, v := range r.Arrays {
		if !shortText(v.Name, v.Level, v.Controller, v.Group) || len(v.Detail) > 300 ||
			!validHealth(v.Status) || v.Expected < 0 || v.Missing < 0 || len(v.Members) > MaxDrives ||
			!validNumber(v.Progress, 0, 100) {
			return errors.New("invalid array")
		}
		if !shortText(v.Members...) {
			return errors.New("invalid array member")
		}
	}
	if storage := r.Storage; storage != nil {
		if len(storage.Sources) > 16 || len(storage.Controllers) > MaxControllers || len(storage.Drives) > MaxDrives {
			return errors.New("too many storage entries")
		}
		for _, s := range storage.Sources {
			if !shortText(s.Name) || (s.Status != "ok" && s.Status != "partial" && s.Status != "error" && s.Status != "unavailable") {
				return errors.New("invalid storage source")
			}
		}
		controllers := make(map[string]bool, len(storage.Controllers))
		for _, c := range storage.Controllers {
			if c.ID == "" || controllers[c.ID] || !shortText(c.ID, c.Source, c.Model, c.Serial, c.Firmware) ||
				!validHealth(c.Status) || len(c.Detail) > 300 {
				return errors.New("invalid storage controller")
			}
			controllers[c.ID] = true
		}
		drives := make(map[string]string, len(storage.Drives))
		for _, d := range storage.Drives {
			if d.ID == "" || drives[d.ID] != "" || !controllers[d.Controller] ||
				!shortText(d.ID, d.Slot, d.Device, d.Model, d.Serial, d.Firmware, d.Interface, d.Media, d.Role) ||
				!validHealth(d.Status) || len(d.Detail) > 300 || !validNumber(d.Temperature, -80, 250) ||
				!validNumber(d.Progress, 0, 100) {
				return errors.New("invalid physical drive")
			}
			drives[d.ID] = d.Controller
		}
		for _, a := range r.Arrays {
			if a.Controller != "" && !controllers[a.Controller] {
				return errors.New("invalid array controller")
			}
			for _, member := range a.Members {
				if a.Controller == "" || drives[member] != a.Controller {
					return errors.New("invalid array member")
				}
			}
		}
	}
	return nil
}

func shortText(fields ...string) bool {
	for _, field := range fields {
		if len(field) > 160 || strings.ContainsAny(field, "\x00\r\n") {
			return false
		}
	}
	return true
}

func validHealth(status string) bool {
	switch status {
	case "active", "degraded", "rebuilding", "inactive", "unknown":
		return true
	default:
		return false
	}
}

func validNumber(n *float64, min, max float64) bool {
	return n == nil || (!math.IsNaN(*n) && *n >= min && *n <= max)
}
