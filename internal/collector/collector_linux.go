//go:build linux

package collector

import (
	"bufio"
	"github.com/iPapik/Storavix/internal/telemetry"
	"os"
	"strconv"
	"strings"
	"syscall"
)

type Snapshot struct {
	CPUIdle, CPUTotal, NetBytes uint64
	CPUValid, NetValid          bool
}

func readCPU() (idle, total uint64, valid bool) {
	b, e := os.ReadFile("/proc/stat")
	if e != nil {
		return
	}
	line := strings.SplitN(string(b), "\n", 2)[0]
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return
	}
	for i, field := range f[1:] {
		v, er := strconv.ParseUint(field, 10, 64)
		if er != nil {
			return 0, 0, false
		}
		total += v
		if i == 3 || i == 4 {
			idle += v
		}
	}
	return idle, total, true
}
func readNetwork() (uint64, bool) {
	f, e := os.Open("/proc/net/dev")
	if e != nil {
		return 0, false
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	var total uint64
	for s.Scan() {
		line := s.Text()
		i := strings.Index(line, ":")
		if i < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:i])
		if iface == "lo" {
			continue
		}
		nums := strings.Fields(line[i+1:])
		if len(nums) < 16 {
			continue
		}
		rx, e1 := strconv.ParseUint(nums[0], 10, 64)
		tx, e2 := strconv.ParseUint(nums[8], 10, 64)
		if e1 == nil && e2 == nil {
			total += rx + tx
		}
	}
	return total, s.Err() == nil
}
func Read() (telemetry.Report, Snapshot) {
	r := telemetry.Report{OS: "linux"}
	ss := Snapshot{}
	ss.CPUIdle, ss.CPUTotal, ss.CPUValid = readCPU()
	ss.NetBytes, ss.NetValid = readNetwork()
	if b, e := os.ReadFile("/proc/meminfo"); e == nil {
		var total, available uint64
		for _, ln := range strings.Split(string(b), "\n") {
			f := strings.Fields(ln)
			if len(f) < 2 {
				continue
			}
			v, _ := strconv.ParseUint(f[1], 10, 64)
			switch f[0] {
			case "MemTotal:":
				total = v
			case "MemAvailable:":
				available = v
			}
		}
		if total > 0 {
			x := 100 * (1 - float64(available)/float64(total))
			r.MemoryPercent = &x
		}
	}
	var stat syscall.Statfs_t
	if syscall.Statfs("/", &stat) == nil && stat.Blocks > 0 {
		x := 100 * (1 - float64(stat.Bavail)/float64(stat.Blocks))
		r.DiskPercent = &x
	}
	if b, e := os.ReadFile("/proc/uptime"); e == nil {
		f := strings.Fields(string(b))
		if len(f) > 0 {
			if v, e := strconv.ParseFloat(f[0], 64); e == nil {
				u := int64(v)
				r.UptimeSeconds = &u
			}
		}
	}
	if b, e := os.ReadFile("/proc/loadavg"); e == nil {
		f := strings.Fields(string(b))
		for i := 0; i < 3 && i < len(f); i++ {
			if v, e := strconv.ParseFloat(f[i], 64); e == nil {
				r.LoadAverage = append(r.LoadAverage, v)
			}
		}
	}
	if b, e := os.ReadFile("/proc/mdstat"); e == nil {
		r.Arrays = ParseMDStat(string(b))
	}
	return r, ss
}
func ParseMDStat(body string) []telemetry.Array {
	lines := strings.Split(body, "\n")
	out := []telemetry.Array{}
	for i, ln := range lines {
		f := strings.Fields(ln)
		if len(f) < 4 || !strings.HasPrefix(f[0], "md") || f[1] != ":" {
			continue
		}
		status := "unknown"
		for _, v := range f {
			if v == "active" {
				status = "active"
			}
			if v == "inactive" {
				status = "inactive"
			}
		}
		detail := strings.TrimSpace(ln)
		if i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			detail += " | " + next
			if strings.Contains(next, "recovery") || strings.Contains(next, "resync") || strings.Contains(next, "reshape") {
				status = "rebuilding"
			}
			if strings.Contains(next, "_") && strings.Contains(next, "[") {
				status = "degraded"
			}
		}
		if len(detail) > 300 {
			detail = detail[:300]
		}
		out = append(out, telemetry.Array{Name: f[0], Status: status, Detail: detail})
	}
	return out
}
