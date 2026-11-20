package collector

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Temperature() *float64 {
	root := os.Getenv("SystemRoot")
	if root == "" {
		return nil
	}
	powershell := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	out, err := runStorageCLI(ctx, powershell, "-NoProfile", "-NonInteractive", "-Command",
		`Get-CimInstance -Namespace root/wmi -ClassName MSAcpi_ThermalZoneTemperature -ErrorAction Stop | Where-Object { $_.Active -ne $false } | ForEach-Object { [string]$_.CurrentTemperature }`)
	if err != nil {
		return nil
	}
	var highest *float64
	for _, line := range strings.Split(string(out), "\n") {
		highest = maxTemperature(line, 10, -273.15, highest)
	}
	return highest
}
