package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iPapik/Storavix/internal/telemetry"
)

type megaResponse map[string]json.RawMessage

func (r megaResponse) object(key string) megaResponse {
	var fields megaResponse
	_ = json.Unmarshal(r[key], &fields)
	return fields
}

func (r megaResponse) rows(key string) []megaResponse {
	var rows []megaResponse
	_ = json.Unmarshal(r[key], &rows)
	return rows
}

func (r megaResponse) value(keys ...string) string {
	for _, key := range keys {
		raw := r[key]
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if s = strings.TrimSpace(s); s != "" {
				return s
			}
		} else if len(raw) > 0 && (raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9') {
			return string(raw)
		}
	}
	return ""
}

type megaController struct {
	Command megaResponse `json:"Command Status"`
	Data    megaResponse `json:"Response Data"`
}

func megaPayload(output []byte) []megaController {
	var payload struct{ Controllers []megaController }
	start := bytes.IndexByte(output, '{')
	end := bytes.LastIndexByte(output, '}')
	if start < 0 || end <= start || json.Unmarshal(output[start:end+1], &payload) != nil {
		return nil
	}
	return payload.Controllers
}

func decodeMega(output []byte) ([]megaController, string) {
	payload := megaPayload(output)
	if payload == nil {
		return nil, "error"
	}
	var valid []megaController
	failed := false
	for _, c := range payload {
		status := c.Command.value("Status")
		description := strings.ToLower(c.Command.value("Description", "Detailed Status"))
		if strings.EqualFold(status, "success") {
			if _, err := strconv.Atoi(c.Command.value("Controller")); err == nil {
				valid = append(valid, c)
			} else if c.Data.value("Controller Count", "Number of Controllers") != "0" &&
				!strings.Contains(description, "no controller") {
				failed = true
			}
		} else if !strings.Contains(description, "no controller") &&
			!strings.Contains(description, "controller not found") {
			failed = true
		}
	}
	if failed && len(valid) == 0 {
		return nil, "error"
	}
	if failed {
		return valid, "partial"
	}
	return valid, "ok"
}

type megaKey struct{ controller, slot string }

var megaDrivePath = regexp.MustCompile(`^Drive (/c[0-9]+)(?:/e([0-9]+))?/s([0-9]+) - Detailed Information$`)

func megaDetails(responses []megaController) map[megaKey]megaResponse {
	out := make(map[megaKey]megaResponse)
	for _, c := range responses {
		for key := range c.Data {
			m := megaDrivePath.FindStringSubmatch(key)
			if m == nil {
				continue
			}
			drive := strings.TrimSuffix(key, " - Detailed Information")
			fields := c.Data.object(key)
			details := make(megaResponse)
			for _, name := range []string{" Device attributes", " State", " Policies/Settings"} {
				for k, value := range fields.object(drive + name) {
					details[k] = value
				}
			}
			out[megaKey{m[1], m[2] + ":" + m[3]}] = details
		}
	}
	return out
}

var megaRebuild = regexp.MustCompile(`^(/c[0-9]+)(?:/e([0-9]+))?/s([0-9]+)(?:\s+.*)?$`)

func megaProgress(responses []megaController) map[megaKey]*float64 {
	out := make(map[megaKey]*float64)
	for _, c := range responses {
		for key := range c.Data {
			for _, d := range c.Data.rows(key) {
				path := d.value("Drive-ID", "Drive")
				pct := number(d.value("Progress%", "Progress %", "Progress"), 0, 100)
				if m := megaRebuild.FindStringSubmatch(path); m != nil && pct != nil {
					out[megaKey{m[1], m[2] + ":" + m[3]}] = pct
				}
			}
		}
	}
	return out
}

func megaDisk(controller, eidSlot string, row, info megaResponse, progress *float64) telemetry.Drive {
	raw := row.value("State", "Status")
	health := deviceHealth(raw)
	if other := deviceHealth(row.value("Status")); other == "inactive" || other == "rebuilding" || other == "degraded" && health == "active" {
		health = other
	}
	role := "member"
	switch strings.ToLower(raw) {
	case "ghs":
		role = "global-spare"
	case "dhs":
		role = "spare"
	case "jbod":
		role = "jbod"
	default:
		if group := row.value("DG"); group == "" || group == "-" || group == "F" {
			role = "unassigned"
		}
	}
	problem := ""
	if strings.EqualFold(info.value("S.M.A.R.T alert flagged by drive", "SMART alert flagged by drive"), "yes") {
		problem = "SMART alert"
	} else if count, _ := strconv.Atoi(info.value("Predictive Failure Count")); count > 0 {
		problem = "Predictive Failure Count: " + strconv.Itoa(count)
	} else if count, _ := strconv.Atoi(info.value("Media Error Count")); count > 0 {
		problem = "Media Error Count: " + strconv.Itoa(count)
	}
	if problem != "" && (health == "active" || health == "unknown") {
		health = "degraded"
	}
	model := info.value("Model Number", "Model")
	if model == "" {
		model = row.value("Model")
	}
	return telemetry.Drive{
		ID: controller + "/" + eidSlot, Controller: controller, Slot: eidSlot,
		Model: clean(model), Serial: clean(info.value("SN", "Serial Number")), Firmware: clean(info.value("Firmware Revision", "Firmware")),
		Interface: clean(row.value("Intf", "Interface")), Media: clean(row.value("Med", "Media")),
		Role: role, Status: health, Detail: text(strings.Trim(strings.Join([]string{raw, problem}, " · "), " ·"), 300),
		SizeBytes: sizeBytes(row.value("Size"), 1024), Temperature: number(info.value("Drive Temperature", "Temperature"), -80, 250),
		Progress: progress,
	}
}

func cliSource(path string) string {
	name := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasPrefix(name, "perccli"):
		if strings.Contains(name, "cli2") {
			return "perccli2"
		}
		return "perccli"
	case strings.HasPrefix(name, "storcli2"):
		return "storcli2"
	default:
		return "storcli"
	}
}

func readMega(path string) (Inventory, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 22*time.Second)
	defer cancel()
	run := func(args ...string) []byte {
		// A mixed result can contain useful JSON even with a nonzero exit code.
		b, _ := runStorageCLI(ctx, path, args...)
		return b
	}
	return readMegaWithRunner(cliSource(path), run)
}

func readMegaWithRunner(source string, run func(...string) []byte) (Inventory, string) {
	base, status := decodeMega(run("/call", "show", "all", "J"))
	var legacy []string
	if len(base) == 0 && status == "error" && source == "perccli" {
		legacy = megaControllerPaths(run("show", "J"))
		if len(legacy) == 0 {
			legacy = []string{"/c0"}
		}
		var missed bool
		for _, controller := range legacy {
			items, result := decodeMega(run(controller, "show", "all", "J"))
			missed = missed || len(items) == 0 || result != "ok"
			base = append(base, items...)
		}
		if len(base) > 0 {
			status = "ok"
			if missed {
				status = "partial"
			}
		}
	}
	if len(base) == 0 {
		return Inventory{}, status
	}
	detailRows := func(action string) []megaController {
		targets := legacy
		if len(targets) == 0 {
			targets = []string{"/call"}
		}
		var rows []megaController
		for _, controller := range targets {
			for _, suffix := range []string{"/eall/sall", "/sall"} {
				result, _ := decodeMega(run(controller+suffix, "show", action, "J"))
				rows = append(rows, result...)
			}
		}
		return rows
	}
	details := megaDetails(detailRows("all"))
	var progress map[megaKey]*float64
	if hasMegaRebuild(base) {
		progress = megaProgress(detailRows("rebuild"))
	}
	result := parseMega(source, base, details, progress)
	for _, d := range result.Drives {
		if d.Serial == "" && d.Status != "inactive" {
			status = "partial"
			break
		}
	}
	return result, status
}

func megaControllerPaths(data []byte) []string {
	var paths []string
	for _, response := range megaPayload(data) {
		for _, controller := range response.Data.rows("System Overview") {
			index, err := strconv.Atoi(controller.value("Ctl", "Controller"))
			if err != nil || index < 0 || index >= telemetry.MaxControllers {
				continue
			}
			path := "/c" + strconv.Itoa(index)
			if !contains(paths, path) {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func hasMegaRebuild(base []megaController) bool {
	for _, c := range base {
		for _, d := range c.Data.rows("PD LIST") {
			if deviceHealth(d.value("State", "Status")) == "rebuilding" {
				return true
			}
		}
	}
	return false
}

func parseMega(source string, base []megaController, details map[megaKey]megaResponse, progress map[megaKey]*float64) Inventory {
	var out Inventory
	for _, c := range base {
		controller := "/c" + c.Command.value("Controller")
		basics := c.Data.object("Basics")
		id := "mega:" + controller
		model := basics.value("Model", "Product Name")
		if model == "" {
			model = c.Data.value("Product Name", "Model")
		}
		if model == "" {
			model = controller
		}
		raw := c.Data.object("Status").value("Controller Status")
		if raw == "" {
			raw = c.Data.value("Controller Status")
		}
		out.Controllers = append(out.Controllers, telemetry.Controller{
			ID: id, Model: clean(model), Source: source, Serial: clean(basics.value("Serial Number")),
			Firmware: clean(c.Data.object("Version").value("Firmware Package Build", "Firmware Version")),
			Status:   deviceHealth(raw), Detail: text(raw, 300),
		})
		members := map[string][]telemetry.Drive{}
		for _, d := range c.Data.rows("PD LIST") {
			slot := megaSlot(d.value("EID:Slt", "EID:Slot"))
			if slot == "" {
				if did := d.value("DID", "PID"); did != "" {
					slot = "ID " + did
				} else {
					continue
				}
			}
			key := megaKey{controller, slot}
			drive := megaDisk(id, clean(slot), d, details[key], progress[key])
			out.Drives = append(out.Drives, drive)
			if drive.Role == "member" {
				group := d.value("DG")
				members[group] = append(members[group], drive)
			}
		}
		expected := map[string]int{}
		missing := map[string]int{}
		for _, row := range c.Data.rows("TOPOLOGY") {
			if _, err := strconv.Atoi(row.value("Row")); err != nil {
				continue
			}
			group := row.value("DG")
			if group == "" || group == "-" {
				continue
			}
			expected[group]++
			switch strings.ToLower(row.value("State")) {
			case "missing", "msng":
				missing[group]++
			}
		}
		for _, v := range c.Data.rows("VD LIST") {
			group, vd, ok := strings.Cut(v.value("DG/VD"), "/")
			if !ok {
				continue
			}
			name := v.value("Name")
			if name == "" {
				name = fmt.Sprintf("VD %s", vd)
			}
			state := v.value("State", "Status")
			a := telemetry.Array{
				Name: clean(name), Controller: id, Level: clean(v.value("TYPE", "Type")),
				Group: clean(fmt.Sprintf("DG %s / VD %s", group, vd)), Status: deviceHealth(state),
				Detail: text(state, 300), SizeBytes: sizeBytes(v.value("Size"), 1024),
				Expected: expected[group], Missing: missing[group],
			}
			for _, d := range members[group] {
				a.Members = append(a.Members, d.ID)
				if d.Status == "rebuilding" && a.Status != "inactive" {
					a.Status = "rebuilding"
					if d.Progress != nil && (a.Progress == nil || *d.Progress < *a.Progress) {
						a.Progress = d.Progress
					}
				}
			}
			out.Arrays = append(out.Arrays, a)
		}
	}
	return out
}

func megaSlot(value string) string {
	if enclosure, slot, ok := strings.Cut(value, ":"); ok {
		return strings.TrimSpace(enclosure) + ":" + strings.TrimSpace(slot)
	}
	return value
}
