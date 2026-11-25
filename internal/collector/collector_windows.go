//go:build windows

package collector

import (
	"context"
	"encoding/json"
	"time"

	"github.com/iPapik/Storavix/internal/telemetry"
)

type Snapshot struct {
	CPUIdle, CPUTotal, NetBytes uint64
	CPUValid, NetValid          bool
}

const hostMetricsScript = `
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding -ArgumentList $false
$os = Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue
$processors = @(Get-CimInstance Win32_Processor -ErrorAction SilentlyContinue)
$cpu = $processors | Measure-Object -Property LoadPercentage -Average
$cpuCores = ($processors | Measure-Object -Property NumberOfCores -Sum).Sum
$cpuThreads = ($processors | Measure-Object -Property NumberOfLogicalProcessors -Sum).Sum
$cpuModel = if ($processors.Count -gt 0) { [string]$processors[0].Name } else { '' }
$disks = @(Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3' -ErrorAction SilentlyContinue)
$network = $null
foreach ($adapter in Get-NetAdapterStatistics -ErrorAction SilentlyContinue) {
    if ($null -ne $adapter.ReceivedBytes -and $null -ne $adapter.SentBytes) {
        $network = [uint64]$network + [uint64]$adapter.ReceivedBytes + [uint64]$adapter.SentBytes
    }
}
$volumes = @(foreach ($disk in $disks) {
    if ($null -ne $disk.Size -and $null -ne $disk.FreeSpace) {
        @{ name = [string]$disk.DeviceID; filesystem = [string]$disk.FileSystem; sizeBytes = [uint64]$disk.Size; availableBytes = [uint64]$disk.FreeSpace }
    }
})
@{
    cpu = $cpu.Average; cpuModel = $cpuModel; cpuCores = $cpuCores; cpuThreads = $cpuThreads
    total = $os.TotalVisibleMemorySize; free = $os.FreePhysicalMemory
    systemDrive = [string]$os.SystemDrive; network = $network; volumes = $volumes
    uptime = if ($os.LastBootUpTime) { ([datetime]::UtcNow-$os.LastBootUpTime.ToUniversalTime()).TotalSeconds }
} | ConvertTo-Json -Depth 4 -Compress
`

func Read() (telemetry.Report, Snapshot) {
	r := telemetry.Report{OS: "windows"}
	s := Snapshot{}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	b, err := runStorageCLI(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", hostMetricsScript)
	if err != nil {
		return r, s
	}
	var v struct {
		CPU         *float64
		CPUModel    string
		CPUCores    int
		CPUThreads  int
		Total       uint64
		Free        *uint64
		Uptime      *float64
		Network     *uint64
		SystemDrive string
		Volumes     []telemetry.Volume
	}
	if json.Unmarshal(b, &v) != nil {
		return r, s
	}
	r.CPUPercent = v.CPU
	r.CPUModel = v.CPUModel
	r.CPUCores = v.CPUCores
	r.CPUThreads = v.CPUThreads
	r.MemoryBytes = v.Total * 1024
	if v.Total > 0 && v.Free != nil && *v.Free <= v.Total {
		free := *v.Free * 1024
		r.MemoryAvailable = &free
		x := 100 * (1 - float64(*v.Free)/float64(v.Total))
		r.MemoryPercent = &x
	}
	for _, volume := range v.Volumes {
		if volume.SizeBytes == 0 || volume.AvailableBytes > volume.SizeBytes {
			continue
		}
		if len(r.Volumes) < telemetry.MaxVolumes {
			r.Volumes = append(r.Volumes, volume)
		}
		if volume.Name == v.SystemDrive {
			x := 100 * (1 - float64(volume.AvailableBytes)/float64(volume.SizeBytes))
			r.DiskPercent = &x
		}
	}
	if v.Uptime != nil && *v.Uptime >= 0 {
		u := int64(*v.Uptime)
		r.UptimeSeconds = &u
	}
	if v.Network != nil {
		s.NetBytes, s.NetValid = *v.Network, true
	}
	return r, s
}
