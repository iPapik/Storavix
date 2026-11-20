package collector

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func storageProbes() map[string]probe {
	return map[string]probe{
		"windows-spaces": readStorageSpaces,
		"windows-raid-hardware": func() (Inventory, string) {
			megaCh, hpeCh := make(chan Inventory, 1), make(chan Inventory, 1)
			go func() { megaCh <- collectMegaTools(windowsMegaTools()) }()
			go func() { hpeCh <- collectHPETools(windowsHPETools()) }()
			mega, hpe := <-megaCh, <-hpeCh
			var i Inventory
			i.merge(mega)
			i.merge(hpe)
			if len(mega.Controllers) == 0 || len(hpe.Controllers) == 0 {
				i.merge(nativeRAIDInventory(windowsRAIDDevices(), mega, hpe))
			}
			return i, ""
		},
	}
}

func windowsMegaTools() []string {
	var paths []string
	if configured := windowsToolSetting("STORAVIX_MEGA_CLI"); filepath.IsAbs(configured) {
		paths = append(paths, configured)
	}
	paths = append(paths, managedRAIDTools(megaToolNames)...)
	names := []string{"storcli2.exe", "storcli2_64.exe", "perccli2.exe", "perccli64.exe", "storcli64.exe", "perccli.exe", "storcli.exe"}
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramW6432"), os.Getenv("ProgramFiles(x86)")} {
		if root == "" {
			continue
		}
		for _, dir := range []string{`StorCLI`, `StorCLI2`, `MegaRAID\StorCLI`, `MegaRAID\StorCLI2`, `Broadcom\StorCLI`, `Broadcom\StorCLI2`, `Dell\PercCLI`, `Dell\PercCLI2`, `Dell\SysMgt\perccli`} {
			for _, name := range names {
				paths = append(paths, filepath.Join(root, dir, name))
			}
		}
	}
	return paths
}

func windowsHPETools() []string {
	var paths []string
	if configured := windowsToolSetting("STORAVIX_HPE_CLI"); filepath.IsAbs(configured) {
		paths = append(paths, configured)
	}
	paths = append(paths, managedRAIDTools(hpeToolNames)...)
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramW6432"), os.Getenv("ProgramFiles(x86)")} {
		if root == "" {
			continue
		}
		for _, dir := range []string{`HPE\Smart Storage Administrator\ssacli\bin`, `HP\hpssacli\bin`, `HP\ssacli\bin`, `HPE\ssacli\bin`, `Smart Storage Administrator\ssacli\bin`} {
			for _, name := range []string{"ssacli.exe", "hpssacli.exe"} {
				paths = append(paths, filepath.Join(root, dir, name))
			}
		}
	}
	return paths
}

const spacesScript = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding -ArgumentList $false
$id = { param($d) if ($d.UniqueId) { return [string]$d.UniqueId }; return [string]$d.ObjectId }
$partial = $false
if (-not (Get-Command Get-StoragePool -ErrorAction SilentlyContinue)) {
    @{ unavailable = $true } | ConvertTo-Json -Compress
    return
}
# Filtering in PowerShell permits hosts with no concrete pools. The cmdlet's
# CIM filter can raise ObjectNotFound when only the primordial pool exists.
$pools = @(Get-StoragePool | Where-Object { -not $_.IsPrimordial })
$poolData = @(foreach ($p in $pools) {
    $drives = @(try {
        foreach ($d in Get-PhysicalDisk -StoragePool $p) {
            @{
                id = (& $id $d); model = [string]$d.Model; serial = [string]$d.SerialNumber
                firmware = [string]$d.FirmwareVersion; slot = [string]$d.PhysicalLocation
                interface = [string]$d.BusType; media = [string]$d.MediaType; usage = [string]$d.Usage
                health = [string]$d.HealthStatus; operational = ($d.OperationalStatus -join ', ')
                size = [uint64]$d.Size
            }
        }
    } catch { $partial = $true })
    $arrays = @(try {
        foreach ($v in Get-VirtualDisk -StoragePool $p) {
            $members = @(try {
                foreach ($d in Get-PhysicalDisk -VirtualDisk $v) { & $id $d }
            } catch { $partial = $true })
            $layout = [string]$v.ResiliencySettingName
            if ($v.NumberOfDataCopies -gt 1 -and $layout -eq 'Mirror') { $layout += ' (' + $v.NumberOfDataCopies + ')' }
            if ($v.PhysicalDiskRedundancy -gt 1 -and $layout -eq 'Parity') { $layout = 'Dual Parity' }
            @{
                name = [string]$v.FriendlyName; health = [string]$v.HealthStatus; layout = $layout
                operational = ($v.OperationalStatus -join ', '); size = [uint64]$v.Size
                members = $members
            }
        }
    } catch { $partial = $true })
    @{
        id = (& $id $p); name = [string]$p.FriendlyName; health = [string]$p.HealthStatus
        operational = ($p.OperationalStatus -join ', '); drives = $drives; arrays = $arrays
    }
})
@{ pools = $poolData; partial = $partial } | ConvertTo-Json -Depth 7 -Compress
`

func readStorageSpaces() (Inventory, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, err := runStorageCLI(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", spacesScript)
	if err != nil {
		return Inventory{}, "error"
	}
	return parseStorageSpaces(b)
}

// Task Scheduler can cache an old process environment. Read machine-level
// configuration directly so a newly installed CLI can be picked up on the next poll.
func windowsToolSetting(name string) string {
	path, _ := syscall.UTF16PtrFromString(`SYSTEM\CurrentControlSet\Control\Session Manager\Environment`)
	value, _ := syscall.UTF16PtrFromString(name)
	var key syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, path, 0, syscall.KEY_READ, &key) == nil {
		defer syscall.RegCloseKey(key)
		var kind uint32
		var buf [65536]byte
		length := uint32(len(buf))
		if syscall.RegQueryValueEx(key, value, nil, &kind, &buf[0], &length) == nil && kind == syscall.REG_SZ && length >= 2 {
			decoded := make([]uint16, length/2)
			for j := range decoded {
				decoded[j] = binary.LittleEndian.Uint16(buf[j*2:])
			}
			return strings.TrimSpace(syscall.UTF16ToString(decoded))
		}
	}
	return os.Getenv(name)
}
