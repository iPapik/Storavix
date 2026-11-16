package collector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iPapik/Storavix/internal/telemetry"
)

const HardwareInventoryPath = "/run/storavix-agent-storage/inventory.json"

var (
	mdMember  = regexp.MustCompile(`^([^\[]+)\[([0-9]+)\](.*)$`)
	mdCounts  = regexp.MustCompile(`\[([0-9]+)/([0-9]+)\]`)
	mdPercent = regexp.MustCompile(`(?:recovery|resync|reshape|repair|check)\s*=\s*([0-9.]+)%`)
)

func storageProbes() map[string]probe {
	return map[string]probe{"linux-md": readMD, "linux-raid-hardware": readHardwareInventory}
}

func ExportHardwareInventory() error {
	i := collectHardware()
	data, err := json.Marshal(i)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(HardwareInventoryPath), "poll-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Chmod(0640)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), HardwareInventoryPath)
}

func readHardwareInventory() (Inventory, string) {
	b, err := os.ReadFile(HardwareInventoryPath)
	if os.IsNotExist(err) {
		return Inventory{}, "unavailable"
	}
	var i Inventory
	if err != nil || json.Unmarshal(b, &i) != nil || i.CollectedAt.IsZero() || time.Since(i.CollectedAt) > 4*time.Minute {
		return Inventory{}, "error"
	}
	// The individual sources describe the outcome. Do not attach a synthetic
	// successful hardware probe when all vendor tools are unavailable.
	return i, ""
}

func collectHardware() Inventory {
	results := make(chan Inventory, 2)
	go func() {
		results <- collectMegaOnLinux()
	}()
	go func() {
		results <- collectHPEOnLinux()
	}()
	var i Inventory
	i.merge(<-results)
	i.merge(<-results)
	i.CollectedAt = time.Now().UTC()
	return i.bounded()
}

func collectMegaOnLinux() Inventory {
	candidates := []string{
		"/opt/MegaRAID/storcli2/storcli2", "/opt/MegaRAID/storcli/storcli2",
		"/opt/MegaRAID/perccli2/perccli2", "/opt/MegaRAID/perccli/perccli64",
		"/opt/MegaRAID/storcli/storcli64", "/opt/lsi/storcli/storcli",
	}
	for _, name := range []string{"storcli2", "storcli2_64", "perccli2", "perccli64", "storcli64", "perccli", "storcli"} {
		for _, dir := range []string{"/usr/sbin", "/usr/bin", "/usr/local/sbin", "/usr/local/bin"} {
			candidates = append(candidates, filepath.Join(dir, name))
		}
	}
	if path := os.Getenv("STORAVIX_MEGA_CLI"); filepath.IsAbs(path) {
		candidates = append([]string{path}, candidates...)
	}
	return collectMegaTools(candidates)
}

func collectHPEOnLinux() Inventory {
	candidates := []string{
		"/opt/smartstorageadmin/ssacli/bin/ssacli",
		"/opt/hp/hpssacli/bld/hpssacli",
		"/usr/sbin/ssacli", "/usr/bin/ssacli", "/usr/sbin/hpssacli", "/usr/bin/hpssacli",
	}
	if path := os.Getenv("STORAVIX_HPE_CLI"); filepath.IsAbs(path) {
		candidates = append([]string{path}, candidates...)
	}
	return collectHPETools(candidates)
}

func readMD() (Inventory, string) {
	const root = "/sys/class/block"
	b, err := os.ReadFile("/proc/mdstat")
	if os.IsNotExist(err) {
		return Inventory{}, "ok"
	}
	if err != nil {
		return Inventory{}, "error"
	}
	return parseMD(string(b), root), "ok"
}

func ParseMDStat(body string) []telemetry.Array { return parseMD(body, "").Arrays }

func parseMD(body, root string) Inventory {
	var i Inventory
	disks := map[string]int{}
	lines := strings.Split(body, "\n")
	for idx := 0; idx < len(lines); idx++ {
		line := lines[idx]
		f := strings.Fields(line)
		if len(f) < 3 || f[1] != ":" || !strings.HasPrefix(f[0], "md") || filepath.Base(f[0]) != f[0] {
			continue
		}
		name := f[0]
		var details []string
		for idx+1 < len(lines) && strings.TrimSpace(lines[idx+1]) != "" && (strings.HasPrefix(lines[idx+1], " ") || strings.HasPrefix(lines[idx+1], "\t")) {
			idx++
			details = append(details, strings.TrimSpace(lines[idx]))
		}
		info := strings.Join(details, " ")
		if strings.Contains(info, "super external:imsm") {
			// The IMSM container is metadata; its child md devices are the logical arrays.
			continue
		}
		if strings.Contains(info, "super external:ddf") {
			continue
		}
		a := telemetry.Array{Name: name, Controller: "linux-md", Status: deviceHealth(f[2]), Detail: text(strings.TrimSpace(line)+" "+info, 300)}
		for _, entry := range f[3:] {
			if strings.HasPrefix(entry, "raid") || entry == "linear" || entry == "multipath" {
				a.Level = strings.ToUpper(entry)
			}
			if m := mdMember.FindStringSubmatch(entry); m != nil && filepath.Base(m[1]) == m[1] {
				device, state := m[1], m[3]
				role, health, raw := "member", "unknown", state
				if root != "" {
					if value := sysValue(root, name, "md", "dev-"+device, "state"); value != "" {
						raw = value
						health = deviceHealth(value)
					}
				}
				switch {
				case strings.Contains(state, "(F)"):
					health, raw = "inactive", "faulty"
				case strings.Contains(state, "(S)") || strings.Contains(raw, "spare"):
					role = "spare"
					if health == "unknown" {
						health = "active"
					}
				case health == "unknown" && a.Status == "active" && raw == "":
					health = "active"
				}
				d := mdDrive(root, device, role, health, raw)
				if old, ok := disks[d.ID]; ok {
					previous := &i.Drives[old]
					if d.Status == "inactive" || d.Status == "degraded" && previous.Status == "active" {
						previous.Status, previous.Detail = d.Status, d.Detail
					}
					if d.Role == "member" {
						previous.Role = d.Role
					}
				} else {
					disks[d.ID] = len(i.Drives)
					i.Drives = append(i.Drives, d)
				}
				if !contains(a.Members, d.ID) {
					a.Members = append(a.Members, d.ID)
				}
			}
		}
		if match := mdCounts.FindStringSubmatch(info); match != nil {
			a.Expected, _ = strconv.Atoi(match[1])
			present, _ := strconv.Atoi(match[2])
			if present < a.Expected {
				a.Missing = a.Expected - present
			}
		}
		if root != "" {
			if v := sysValue(root, name, "md", "level"); v != "" {
				a.Level = clean(strings.ToUpper(v))
			}
			if v, err := strconv.Atoi(sysValue(root, name, "md", "raid_disks")); err == nil {
				a.Expected = v
			}
			if v, err := strconv.Atoi(sysValue(root, name, "md", "degraded")); err == nil {
				a.Missing = v
			}
			a.SizeBytes = sectorsBytes(sysValue(root, name, "size"))
		}
		if a.Missing > 0 && a.Status != "inactive" {
			a.Status = "degraded"
		}
		if match := mdPercent.FindStringSubmatch(info); match != nil && a.Status != "inactive" {
			a.Progress = number(match[1], 0, 100)
			if !strings.Contains(match[0], "check") {
				a.Status = "rebuilding"
			}
		}
		i.Arrays = append(i.Arrays, a)
	}
	if len(i.Arrays) > 0 {
		i.Controllers = []telemetry.Controller{{ID: "linux-md", Model: "Linux MD", Source: "linux-md", Status: "unknown"}}
	}
	return i
}

func mdDrive(root, device, role, health, detail string) telemetry.Drive {
	base := device
	d := telemetry.Drive{Controller: "linux-md", Device: clean("/dev/" + device), Role: role, Status: health, Detail: text(detail, 300)}
	if root != "" {
		if sysValue(root, device, "partition") != "" {
			if resolved, err := filepath.EvalSymlinks(filepath.Join(root, device)); err == nil {
				base = filepath.Base(filepath.Dir(resolved))
			}
		}
		d.Model = clean(sysValue(root, base, "device", "model"))
		d.Serial = clean(sysValue(root, base, "device", "serial"))
		d.Firmware = clean(sysValue(root, base, "device", "rev"))
		if strings.HasPrefix(base, "nvme") {
			d.Interface = "NVMe"
			if d.Firmware == "" {
				d.Firmware = clean(sysValue(root, base, "device", "firmware_rev"))
			}
		}
		switch sysValue(root, base, "queue", "rotational") {
		case "1":
			d.Media = "HDD"
		case "0":
			d.Media = "SSD"
		}
		d.SizeBytes = sectorsBytes(sysValue(root, base, "size"))
	}
	d.ID = clean("linux-md/" + base)
	return d
}

func sysValue(parts ...string) string {
	b, _ := os.ReadFile(filepath.Join(parts...))
	return strings.TrimSpace(string(b))
}

func sectorsBytes(input string) uint64 {
	n, err := strconv.ParseUint(input, 10, 55)
	if err != nil {
		return 0
	}
	return n * 512
}
