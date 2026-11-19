package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iPapik/Storavix/internal/telemetry"
)

type Inventory struct {
	telemetry.Storage
	Arrays []telemetry.Array `json:"arrays,omitempty"`
}

func (i *Inventory) merge(other Inventory) {
	i.Sources = append(i.Sources, other.Sources...)
	i.Controllers = append(i.Controllers, other.Controllers...)
	i.Drives = append(i.Drives, other.Drives...)
	i.Arrays = append(i.Arrays, other.Arrays...)
}

func (i Inventory) AddTo(r *telemetry.Report) {
	r.Storage = &i.Storage
	r.Arrays = i.Arrays
}

func (i Inventory) bounded() Inventory {
	out := Inventory{Storage: telemetry.Storage{CollectedAt: i.CollectedAt, Sources: i.Sources}}
	controllers := map[string]bool{}
	drives := map[string]bool{}
	for _, c := range i.Controllers {
		if len(out.Controllers) == telemetry.MaxControllers {
			break
		}
		if c.ID != "" && !controllers[c.ID] {
			controllers[c.ID] = true
			out.Controllers = append(out.Controllers, c)
		}
	}
	for _, d := range i.Drives {
		if len(out.Drives) == telemetry.MaxDrives {
			break
		}
		if d.ID != "" && controllers[d.Controller] && !drives[d.ID] {
			drives[d.ID] = true
			out.Drives = append(out.Drives, d)
		}
	}
	for _, a := range i.Arrays {
		if len(out.Arrays) == telemetry.MaxArrays {
			break
		}
		if a.Controller != "" && !controllers[a.Controller] {
			continue
		}
		members := make([]string, 0, len(a.Members))
		for _, id := range a.Members {
			if drives[id] {
				members = append(members, id)
			}
		}
		a.Members = members
		sort.Strings(a.Members)
		out.Arrays = append(out.Arrays, a)
	}
	if len(out.Controllers) < len(i.Controllers) || len(out.Drives) < len(i.Drives) || len(out.Arrays) < len(i.Arrays) {
		out.markTruncated()
	}
	sort.Slice(out.Controllers, func(a, b int) bool { return out.Controllers[a].ID < out.Controllers[b].ID })
	sort.Slice(out.Drives, func(a, b int) bool { return out.Drives[a].ID < out.Drives[b].ID })
	sort.Slice(out.Sources, func(a, b int) bool { return out.Sources[a].Name < out.Sources[b].Name })
	// Reserve space for the host metrics in the same request.
	if b, _ := json.Marshal(out); len(b) > telemetry.MaxReportBytes-64*1024 {
		for index := range out.Arrays {
			out.Arrays[index].Members = nil
		}
		out.markTruncated()
	}
	return out
}

func (i *Inventory) markTruncated() {
	for _, source := range i.Sources {
		if source.Name == "inventory-limit" {
			return
		}
	}
	i.Sources = append(i.Sources, telemetry.Source{Name: "inventory-limit", Status: "partial"})
}

type probe func() (Inventory, string)

func ReadStorage() Inventory {
	probes := storageProbes()
	result := make(chan Inventory, len(probes))
	for name, collect := range probes {
		go func(name string, collect probe) {
			inventory, status := collect()
			if status != "" {
				inventory.Sources = append(inventory.Sources, telemetry.Source{Name: name, Status: status})
			}
			result <- inventory
		}(name, collect)
	}
	inventory := Inventory{}
	for range probes {
		inventory.merge(<-result)
	}
	inventory.CollectedAt = time.Now().UTC()
	return inventory.bounded()
}

type limitedOutput struct {
	bytes.Buffer
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 16*1024*1024 {
		return 0, fmt.Errorf("storage command returned too much data")
	}
	return b.Buffer.Write(p)
}

func runStorageCLI(ctx context.Context, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = time.Second
	if runtime.GOOS == "linux" {
		cmd.Env = append(os.Environ(), "LC_ALL=C")
	}
	var output limitedOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return output.Bytes(), err
}

func text(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.ReplaceAll(value, "\x00", "")), " ")
	if len(value) <= limit {
		return value
	}
	for offset := range value {
		if offset >= limit-3 {
			return value[:offset] + "..."
		}
	}
	return ""
}

func clean(value string) string { return text(value, 160) }

func sizeBytes(value string, legacyBase float64) uint64 {
	fields := strings.Fields(strings.ToUpper(value))
	if len(fields) == 1 {
		index := strings.IndexFunc(fields[0], func(r rune) bool { return r != '.' && (r < '0' || r > '9') })
		if index <= 0 {
			n, _ := strconv.ParseUint(fields[0], 10, 64)
			return n
		}
		fields = []string{fields[0][:index], fields[0][index:]}
	}
	if len(fields) < 2 {
		return 0
	}
	n, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || n < 0 {
		return 0
	}
	unit := fields[1]
	if unit == "B" || unit == "BYTES" {
		return uint64(n)
	}
	if len(unit) != 2 && len(unit) != 3 || (!strings.HasSuffix(unit, "B")) {
		return 0
	}
	exponent := strings.IndexByte("KMGTPE", unit[0]) + 1
	if exponent == 0 {
		return 0
	}
	if strings.HasSuffix(unit, "IB") {
		legacyBase = 1024
	}
	n *= math.Pow(legacyBase, float64(exponent))
	if !(n >= 0 && n < math.Exp2(64)) {
		return 0
	}
	return uint64(n)
}

func number(value string, min, max float64) *float64 {
	fields := strings.Fields(value)
	if len(fields) > 0 {
		n, err := strconv.ParseFloat(strings.TrimRight(fields[0], "%Cc°"), 64)
		if err == nil && n >= min && n <= max {
			return &n
		}
	}
	return nil
}

func deviceHealth(raw string) string {
	status := "unknown"
	for _, value := range strings.Split(strings.ToLower(raw), ",") {
		switch strings.Join(strings.Fields(value), " ") {
		case "failed", "failure", "faulty", "offline", "offln", "ofln", "msng", "missing", "lost communication", "no contact", "unhealthy", "ubad", "ubunsp", "detached", "failed redundancy", "stopped", "inaccessible", "error", "non-recoverable error", "dead", "removed", "no redundancy":
			return "inactive"
		case "degraded", "dgrd", "pdgd", "partially degraded", "warning", "predictive failure", "pred fail", "predictive failure detected", "predictive failure analysis", "pfa", "incomplete", "blocked", "resync required", "stale metadata", "retired":
			if status != "rebuilding" {
				status = "degraded"
			}
		case "rbld", "rebuild", "rebuilding", "recovering", "repairing", "regenerating", "resyncing", "reconstructing", "rebuilding drive":
			status = "rebuilding"
		case "optimal", "optl", "ok", "active", "online", "onln", "healthy", "good", "ugood", "ghs", "dhs", "hot spare", "jbod", "ready", "spare", "in_sync", "configured", "conf":
			if status == "unknown" {
				status = "active"
			}
		}
	}
	return status
}

func collectMegaTools(paths []string) Inventory {
	var result Inventory
	used := map[string]bool{}
	attempts := 0
	for _, path := range paths {
		if !isTool(path) {
			continue
		}
		tool := path
		if runtime.GOOS == "windows" {
			tool = strings.ToLower(tool)
		}
		if used[tool] {
			continue
		}
		used[tool] = true
		source := cliSource(path)
		inventory, status := readMega(path)
		if len(inventory.Controllers) > 0 {
			inventory.Sources = []telemetry.Source{{Name: source, Status: status}}
			return inventory
		}
		if len(result.Sources) == 0 || result.Sources[0].Name != source {
			result.Sources = append(result.Sources, telemetry.Source{Name: source, Status: status})
		}
		attempts++
		if attempts >= 2 {
			// Bound the wait on a host with several versions of the vendor tools.
			break
		}
	}
	if len(result.Sources) == 0 {
		result.Sources = []telemetry.Source{{Name: "storcli-perccli", Status: "unavailable"}}
	}
	return result
}

func collectHPETools(paths []string) Inventory {
	status := "unavailable"
	attempts := 0
	for _, path := range paths {
		if isTool(path) {
			inventory, current := readHPE(path)
			if len(inventory.Controllers) > 0 {
				inventory.Sources = []telemetry.Source{{Name: "ssacli", Status: current}}
				return inventory
			}
			status = current
			attempts++
			if attempts >= 2 {
				break
			}
		}
	}
	return Inventory{Storage: telemetry.Storage{Sources: []telemetry.Source{{Name: "ssacli", Status: status}}}}
}

func isTool(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && (runtime.GOOS == "windows" || info.Mode()&0111 != 0)
}
func contains(values []string, search string) bool {
	for _, value := range values {
		if value == search {
			return true
		}
	}
	return false
}
