package collector

import (
	"strings"

	"github.com/iPapik/Storavix/internal/telemetry"
)

const nativeRAIDSource = "raid-os"

type raidDevice struct {
	ID, Model, Driver, Vendor, Product, Subsystem string
	RAID                                          bool
}

func (d raidDevice) family() string {
	driver := strings.ToLower(d.Driver)
	model := strings.ToLower(d.Model)
	switch {
	case strings.HasPrefix(driver, "megaraid"), strings.HasPrefix(driver, "megasas"),
		d.Vendor == "1000", strings.Contains(model, "perc h"), strings.Contains(model, "megaraid"):
		return "mega"
	case driver == "hpsa", driver == "cciss", strings.HasPrefix(driver, "hpciss"),
		d.Vendor == "103c" || d.Vendor == "1590" || d.Vendor == "0e11",
		strings.HasPrefix(d.Subsystem, "103c:"), strings.HasPrefix(d.Subsystem, "1590:"):
		return "hpe"
	default:
		return ""
	}
}

func nativeRAIDInventory(devices []raidDevice, mega, hpe Inventory) Inventory {
	var result Inventory
	for _, d := range devices {
		family := d.family()
		if !d.RAID && family == "" {
			continue
		}
		// Some HBA drivers and PCI vendors also handle non-RAID adapters.
		if !d.RAID && !isRAIDDriver(d.Driver) {
			continue
		}
		if family == "mega" && len(mega.Controllers) != 0 || family == "hpe" && len(hpe.Controllers) != 0 {
			continue
		}
		model := d.Model
		if model == "" {
			switch family {
			case "mega":
				model = "Broadcom/LSI MegaRAID"
			case "hpe":
				model = "HPE Smart Array"
			default:
				model = "PCI RAID controller"
			}
		}
		detail := "PCI " + d.Vendor + ":" + d.Product
		if d.Subsystem != "" {
			detail += " · subsystem " + d.Subsystem
		}
		if d.Driver != "" {
			detail += " · " + d.Driver
		}
		switch family {
		case "mega":
			detail += " · StorCLI / PercCLI required for array details"
		case "hpe":
			detail += " · HPE SSA CLI required for array details"
		default:
			detail += " · Vendor tool required for array details"
		}
		result.Controllers = append(result.Controllers, telemetry.Controller{
			ID: clean(nativeRAIDSource + "/" + d.ID), Source: nativeRAIDSource,
			Model: clean(model), Status: "unknown", Detail: text(detail, 300),
		})
	}
	if len(result.Controllers) > 0 {
		result.Sources = []telemetry.Source{{Name: nativeRAIDSource, Status: "partial"}}
	}
	return result
}

func isRAIDDriver(driver string) bool {
	driver = strings.ToLower(driver)
	return strings.HasPrefix(driver, "megaraid") || strings.HasPrefix(driver, "megasas") ||
		driver == "hpsa" || driver == "cciss" || strings.HasPrefix(driver, "hpciss") || driver == "aacraid"
}
