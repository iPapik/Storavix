package collector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const pciRAIDScript = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding -ArgumentList $false
$items = @(Get-CimInstance -ClassName Win32_PnPEntity -Filter "PNPDeviceID LIKE 'PCI%'" |
    Where-Object { $_.Present -ne $false -and $_.ConfigManagerErrorCode -ne 24 -and $_.ConfigManagerErrorCode -ne 45 } |
    ForEach-Object {
        $raid = $false
        foreach ($key in ($_.HardwareID + $_.CompatibleID)) {
            if ($key -match 'CC_0104') { $raid = $true; break }
        }
        @{ ID = [string]$_.PNPDeviceID; Model = [string]$_.Name; Driver = [string]$_.Service; RAID = $raid }
    })
@{ Devices = $items } | ConvertTo-Json -Depth 4 -Compress
`

var (
	pciIdentity  = regexp.MustCompile(`(?i)^PCI\\VEN_([a-f0-9]{4})&DEV_([a-f0-9]{4})`)
	pciSubsystem = regexp.MustCompile(`(?i)&SUBSYS_([a-f0-9]{4})([a-f0-9]{4})(?:&|\\|$)`)
)

func windowsRAIDDevices() []raidDevice {
	root := os.Getenv("SystemRoot")
	if root == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	powershell := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	data, err := runStorageCLI(ctx, powershell, "-NoProfile", "-NonInteractive", "-Command", pciRAIDScript)
	if err != nil {
		return nil
	}
	var result struct{ Devices []raidDevice }
	if json.Unmarshal(data, &result) != nil {
		return nil
	}
	var devices []raidDevice
	for _, d := range result.Devices {
		if !d.RAID && !isRAIDDriver(d.Driver) {
			continue
		}
		id := pciIdentity.FindStringSubmatch(d.ID)
		if id == nil {
			continue
		}
		d.Vendor, d.Product = strings.ToLower(id[1]), strings.ToLower(id[2])
		if sub := pciSubsystem.FindStringSubmatch(d.ID); sub != nil {
			d.Subsystem = strings.ToLower(sub[2] + ":" + sub[1])
		}
		devices = append(devices, d)
	}
	return devices
}

func detectedRAIDDevices() []raidDevice { return windowsRAIDDevices() }

func raidToolDirectory() string {
	root := os.Getenv("ProgramW6432")
	if root == "" {
		root = os.Getenv("ProgramFiles")
	}
	if !filepath.IsAbs(root) {
		return ""
	}
	return filepath.Join(root, "Storavix Agent", "raid-tools")
}

func prepareRAIDToolDirectory(dir string) error {
	return os.MkdirAll(dir, 0755)
}
