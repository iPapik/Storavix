package collector

import (
	"os"
	"path/filepath"
	"strings"
)

func Temperature() *float64 {
	var highest *float64
	for _, sensor := range []struct{ glob, identifier string }{
		{"/sys/class/hwmon/hwmon*/temp*_input", "name"},
		{"/sys/class/thermal/thermal_zone*/temp", "type"},
	} {
		paths, _ := filepath.Glob(sensor.glob)
		for _, path := range paths {
			name, err := os.ReadFile(filepath.Join(filepath.Dir(path), sensor.identifier))
			if err != nil {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(string(name))) {
			case "coretemp", "k10temp", "zenpower", "acpitz", "peci_cputemp", "x86_pkg_temp",
				"cpu_thermal", "cpu-thermal", "soc_thermal", "soc-thermal", "armada_thermal",
				"bcm2835_thermal", "k8temp":
				if raw, err := os.ReadFile(path); err == nil {
					highest = maxTemperature(string(raw), 1000, 0, highest)
				}
			}
		}
	}
	return highest
}
