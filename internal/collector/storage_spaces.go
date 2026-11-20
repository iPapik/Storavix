package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/iPapik/Storavix/internal/telemetry"
)

type spacesDrive struct {
	ID, Model, Serial, Firmware, Slot, Interface, Media, Usage, Health, Operational string
	Size                                                                            uint64
}

type spacesArray struct {
	Name, Health, Operational, Layout string
	Size                              uint64
	Members                           []string
}

type spacesPool struct {
	ID, Name, Health, Operational string
	Drives                        []spacesDrive
	Arrays                        []spacesArray
}

func parseStorageSpaces(b []byte) (Inventory, string) {
	var data struct {
		Pools       []spacesPool
		Partial     bool
		Unavailable bool
	}
	if json.Unmarshal(b, &data) != nil {
		return Inventory{}, "error"
	}
	if data.Unavailable {
		return Inventory{}, "unavailable"
	}
	if data.Pools == nil {
		return Inventory{}, "error"
	}
	var out Inventory
	for _, pool := range data.Pools {
		id := "spaces:" + stableID(pool.ID, pool.Name)
		raw := strings.Trim(pool.Operational+", "+pool.Health, ", ")
		out.Controllers = append(out.Controllers, telemetry.Controller{
			ID: id, Source: "windows-spaces", Model: clean(pool.Name), Status: deviceHealth(raw), Detail: text(raw, 300),
		})
		drives := map[string]string{}
		for _, disk := range pool.Drives {
			driveID := id + "/" + stableID(disk.ID, disk.Slot+":"+disk.Serial)
			drives[disk.ID] = driveID
			raw := strings.Trim(disk.Operational+", "+disk.Health, ", ")
			role := "pool"
			switch strings.ToLower(disk.Usage) {
			case "hot spare", "hotspare":
				role = "spare"
			case "journal":
				role = "journal"
			case "retired":
				role = "retired"
			}
			out.Drives = append(out.Drives, telemetry.Drive{
				ID: driveID, Controller: id, Model: clean(disk.Model), Serial: clean(disk.Serial),
				Firmware: clean(disk.Firmware), Slot: clean(disk.Slot), Interface: clean(disk.Interface),
				Media: clean(disk.Media), Role: role, Status: deviceHealth(raw), Detail: text(raw, 300), SizeBytes: disk.Size,
			})
		}
		for _, array := range pool.Arrays {
			raw := strings.Trim(array.Operational+", "+array.Health, ", ")
			a := telemetry.Array{
				Name: clean(array.Name), Controller: id, Group: clean(pool.Name),
				Level: clean(array.Layout), Status: deviceHealth(raw), Detail: text(raw, 300), SizeBytes: array.Size,
			}
			for _, disk := range array.Members {
				if member := drives[disk]; member != "" {
					a.Members = append(a.Members, member)
				} else {
					data.Partial = true
				}
			}
			out.Arrays = append(out.Arrays, a)
		}
	}
	if data.Partial {
		return out, "partial"
	}
	return out, "ok"
}

func stableID(id, fallback string) string {
	if id == "" {
		id = fallback
	}
	hash := sha256.Sum256([]byte(id))
	return hex.EncodeToString(hash[:16])
}
