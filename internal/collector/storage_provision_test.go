package collector

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iPapik/Storavix/internal/telemetry"
)

func TestNativeRAIDDetectionUsesUnknownHealthAndDeduplicates(t *testing.T) {
	devices := []raidDevice{
		{ID: "mega", Model: "MegaRAID 9560-16i", Driver: "megaraid_sas", Vendor: "1000", Product: "10e2", RAID: true},
		{ID: "hpe", Model: "HPE Smart Array", Driver: "hpsa", Vendor: "103c", RAID: true},
		{ID: "hba", Model: "HBA", Driver: "mpt3sas", Vendor: "1000"},
		{ID: "perc", Model: "PERC H730", Driver: "megasas2", Subsystem: "1028:1f37", RAID: true},
	}
	if !installRAIDToolForHardware("storcli", devices) || !installRAIDToolForHardware("ssacli", devices) ||
		installRAIDToolForHardware("storcli", devices[2:]) || !installRAIDToolForHardware("perccli", devices[3:]) ||
		installRAIDToolForHardware("perccli", devices[:3]) {
		t.Fatal("tool installer did not distinguish RAID and HBA devices")
	}
	mega := Inventory{Storage: telemetry.Storage{Controllers: []telemetry.Controller{{ID: "storcli/c0"}}}}
	result := nativeRAIDInventory(devices, mega, Inventory{})
	if len(result.Controllers) != 1 || result.Controllers[0].Model != "HPE Smart Array" ||
		result.Controllers[0].Status != "unknown" || len(result.Arrays) != 0 ||
		len(result.Sources) != 1 || result.Sources[0].Status != "partial" {
		t.Fatalf("unexpected OS RAID inventory: %+v", result)
	}
}

func TestRAIDToolStagingIsAtomicAndChecksFormat(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "vendor")
	target := filepath.Join(dir, "storcli")
	data := []byte("test executable bytes")
	checksum := fmt.Sprintf("%x", sha256.Sum256(data))
	for path, content := range map[string][]byte{src: data, target: []byte("original")} {
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	verify := func(path string) error {
		bytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(bytes) != string(data) {
			return fmt.Errorf("unexpected staged bytes")
		}
		return nil
	}
	assertTarget := func(want string) {
		t.Helper()
		bytes, err := os.ReadFile(target)
		if err != nil || string(bytes) != want {
			t.Fatalf("target = %q, want %q, error %v", bytes, want, err)
		}
	}
	if err := stageRAIDTool(target, src, strings.Repeat("0", 64), verify); err == nil {
		t.Fatal("accepted a binary with a mismatched SHA-256")
	}
	assertTarget("original")
	if err := stageRAIDTool(target, src, checksum, verifyNativeRAIDFormat); err == nil {
		t.Fatal("accepted a file that is not a native executable")
	}
	assertTarget("original")
	if err := stageRAIDTool(target, src, checksum, verify); err != nil {
		t.Fatal(err)
	}
	assertTarget(string(data))
	temps, err := filepath.Glob(filepath.Join(dir, ".raid-download-*"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("left partial downloads: %v, error %v", temps, err)
	}
}
