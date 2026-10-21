//go:build !linux && !windows

package collector

import "github.com/iPapik/Storavix/internal/telemetry"

type Snapshot struct {
	CPUIdle, CPUTotal, NetBytes uint64
	CPUValid, NetValid          bool
}

func Read() (telemetry.Report, Snapshot) { return telemetry.Report{OS: "unsupported"}, Snapshot{} }
