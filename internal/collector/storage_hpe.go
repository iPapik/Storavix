package collector

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/iPapik/Storavix/internal/telemetry"
)

var (
	hpeSlot   = regexp.MustCompile(`(?i)^(.+?)\s+in Slot\s+([^\s(]+)(?:\s|$)`)
	hpeArray  = regexp.MustCompile(`(?i)^array(?::\s*|\s+)([^\s(]+)`)
	hpeObject = regexp.MustCompile(`(?i)^(logical\s*drive|physical\s*drive)(?::\s*|\s+)([^\s(]+)`)
)

func readHPE(path string) (Inventory, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Second)
	defer cancel()
	b, err := runStorageCLI(ctx, path, "ctrl", "all", "show", "config", "detail")
	out := parseHPE(string(b))
	if err != nil || len(out.Controllers) == 0 {
		if strings.Contains(strings.ToLower(string(b)), "no controllers detected") {
			return Inventory{}, "ok"
		}
		return Inventory{}, "error"
	}
	return out, "ok"
}

func parseHPE(body string) Inventory {
	var out Inventory
	var controller, group string
	var objectIndent int
	driveIndices := map[string]int{}
	currentDrive, currentArray := -1, -1
	members := map[string][]string{}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		indent := len(line) - len(trimmed)
		line = strings.TrimSpace(line)
		if m := hpeSlot.FindStringSubmatch(line); m != nil {
			controller = "hpe:" + clean(m[2])
			group, currentDrive, currentArray = "", -1, -1
			out.Controllers = append(out.Controllers, telemetry.Controller{ID: controller, Source: "ssacli", Model: clean(m[1]), Status: "unknown"})
			continue
		}
		if controller == "" {
			continue
		}
		if m := hpeArray.FindStringSubmatch(line); m != nil {
			group, currentDrive, currentArray = clean(strings.TrimSuffix(m[1], ":")), -1, -1
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "unassigned") || strings.HasPrefix(lower, "hba drives") {
			group, currentDrive, currentArray = "", -1, -1
			continue
		}
		if m := hpeObject.FindStringSubmatch(line); m != nil {
			if currentArray >= 0 && indent > objectIndent && strings.HasPrefix(strings.ToLower(m[1]), "physical") {
				continue // Mirror group preview inside a detailed logical drive.
			}
			currentDrive, currentArray = -1, -1
			objectIndent = indent
			switch strings.ToLower(strings.ReplaceAll(m[1], " ", "")) {
			case "logicaldrive":
				currentArray = len(out.Arrays)
				out.Arrays = append(out.Arrays, telemetry.Array{
					Name: clean("Logical Drive " + m[2]), Controller: controller, Group: group, Status: "unknown",
				})
			case "physicaldrive":
				currentDrive = len(out.Drives)
				role := "member"
				if group == "" {
					role = "unassigned"
				}
				driveID := controller + "/" + clean(m[2])
				if idx, exists := driveIndices[driveID]; exists {
					currentDrive = idx
				} else {
					driveIndices[driveID] = currentDrive
					out.Drives = append(out.Drives, telemetry.Drive{
						ID: driveID, Controller: controller, Slot: clean(m[2]), Role: role, Status: "unknown",
					})
				}
				if group != "" {
					key := controller + "/" + group
					if !contains(members[key], driveID) {
						members[key] = append(members[key], driveID)
					}
				}
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "Controller Status":
			c := &out.Controllers[len(out.Controllers)-1]
			c.Status, c.Detail = deviceHealth(value), text(value, 300)
		case "Serial Number", "Firmware Version":
			if currentDrive == -1 && currentArray == -1 && group == "" {
				c := &out.Controllers[len(out.Controllers)-1]
				if key == "Serial Number" {
					c.Serial = clean(value)
				} else {
					c.Firmware = clean(value)
				}
			}
		}
		if currentDrive >= 0 {
			d := &out.Drives[currentDrive]
			switch key {
			case "Status":
				state, progress, found := strings.Cut(value, ",")
				d.Status, d.Detail = deviceHealth(state), text(value, 300)
				if found && d.Status == "rebuilding" {
					d.Progress = number(progress, 0, 100)
				}
			case "Drive Type":
				if strings.Contains(strings.ToLower(value), "spare") {
					d.Role = "spare"
				}
			case "Interface Type":
				d.Interface = clean(value)
			case "Media Type":
				d.Media = clean(value)
			case "Rotational Speed":
				if d.Media == "" && value != "0" {
					d.Media = "HDD"
				}
			case "Size":
				d.SizeBytes = sizeBytes(value, 1000)
			case "Model":
				d.Model = clean(value)
			case "Serial Number":
				d.Serial = clean(value)
			case "Firmware Revision":
				d.Firmware = clean(value)
			case "Current Temperature (C)":
				d.Temperature = number(value, -80, 250)
			}
		}
		if currentArray >= 0 {
			a := &out.Arrays[currentArray]
			switch key {
			case "Logical Drive Label":
				if value != "" {
					a.Name = clean(a.Name + " · " + value)
				}
			case "Fault Tolerance":
				a.Level = clean(value)
				if a.Level != "" && a.Level[0] >= '0' && a.Level[0] <= '9' {
					a.Level = "RAID " + a.Level
				}
			case "Size":
				a.SizeBytes = sizeBytes(value, 1024)
			case "Status":
				state, progress, found := strings.Cut(value, ",")
				a.Status, a.Detail = deviceHealth(state), text(value, 300)
				if found && a.Status == "rebuilding" {
					a.Progress = number(progress, 0, 100)
				}
			}
		}
	}
	for idx := range out.Arrays {
		a := &out.Arrays[idx]
		a.Members = members[a.Controller+"/"+a.Group]
	}
	return out
}
