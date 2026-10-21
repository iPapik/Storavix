//go:build windows

package collector

import (
	"context"
	"encoding/json"
	"github.com/iPapik/Storavix/internal/telemetry"
	"os/exec"
	"time"
)

// Windows collector uses built-in CIM cmdlets, not a third-party executable.
type Snapshot struct {
	CPUIdle, CPUTotal, NetBytes uint64
	CPUValid, NetValid          bool
}

func Read() (telemetry.Report, Snapshot) {
	r := telemetry.Report{OS: "windows"}
	s := Snapshot{}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$ErrorActionPreference='Stop'; $os=Get-CimInstance Win32_OperatingSystem; $cpu=Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average; $d=Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='C:'"; $n=Get-NetAdapterStatistics -ErrorAction SilentlyContinue | ForEach-Object { $_.ReceivedBytes + $_.SentBytes } | Measure-Object -Sum; [pscustomobject]@{cpu=$cpu.Average; total=$os.TotalVisibleMemorySize; free=$os.FreePhysicalMemory; disksize=$d.Size; diskfree=$d.FreeSpace; uptime=([datetime]::UtcNow-$os.LastBootUpTime.ToUniversalTime()).TotalSeconds; network=$n.Sum} | ConvertTo-Json -Compress`)
	b, e := cmd.Output()
	if e != nil {
		return r, s
	}
	var v struct {
		CPU      *float64 `json:"cpu"`
		Total    float64  `json:"total"`
		Free     float64  `json:"free"`
		DiskSize float64  `json:"disksize"`
		DiskFree float64  `json:"diskfree"`
		Uptime   float64  `json:"uptime"`
		Network  float64  `json:"network"`
	}
	if json.Unmarshal(b, &v) != nil {
		return r, s
	}
	if v.CPU != nil {
		r.CPUPercent = v.CPU
	}
	if v.Total > 0 {
		x := 100 * (1 - v.Free/v.Total)
		r.MemoryPercent = &x
	}
	if v.DiskSize > 0 {
		x := 100 * (1 - v.DiskFree/v.DiskSize)
		r.DiskPercent = &x
	}
	if v.Uptime > 0 {
		u := int64(v.Uptime)
		r.UptimeSeconds = &u
	}
	if v.Network >= 0 {
		s.NetBytes = uint64(v.Network)
		s.NetValid = true
	}
	return r, s
}
