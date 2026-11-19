package collector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxDetectsUnprobedRAIDThroughPCI(t *testing.T) {
	root := t.TempDir()
	for _, device := range []struct{ slot, class, driver string }{
		{"0000:21:00.0", "0x010400", "megaraid_sas"},
		{"0000:22:00.0", "0x010700", "mpt3sas"},
		{"0000:23:00.0", "0x020000", "igb"},
	} {
		dir := filepath.Join(root, device.slot)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/sys/bus/pci/drivers/"+device.driver, filepath.Join(dir, "driver")); err != nil {
			t.Fatal(err)
		}
		for field, value := range map[string]string{"class": device.class, "vendor": "0x1000", "device": "0x10e2", "label": "MegaRAID 9560", "subsystem_vendor": "0x1028", "subsystem_device": "0x1f37"} {
			if err := os.WriteFile(filepath.Join(dir, field), []byte(value+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	devices := readLinuxRAIDDevices(root)
	if len(devices) != 1 || devices[0].Model != "MegaRAID 9560" || devices[0].family() != "mega" || devices[0].Subsystem != "1028:1f37" {
		t.Fatalf("unexpected RAID device list: %+v", devices)
	}
}
