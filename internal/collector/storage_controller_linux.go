package collector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var pciModel = regexp.MustCompile(`^([^ ]+) "[^"]+" "([^"]+)" "([^"]+)"`)

func linuxRAIDDevices() []raidDevice {
	devices := readLinuxRAIDDevices("/sys/bus/pci/devices")
	if len(devices) == 0 {
		return devices
	}
	for _, tool := range []string{"/usr/bin/lspci", "/usr/sbin/lspci"} {
		if !isTool(tool) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		out, err := runStorageCLI(ctx, tool, "-Dmm")
		cancel()
		if err != nil {
			continue
		}
		models := map[string]string{}
		for _, line := range strings.Split(string(out), "\n") {
			if row := pciModel.FindStringSubmatch(line); row != nil {
				models[row[1]] = row[2] + " " + row[3]
			}
		}
		for n := range devices {
			if model := models[devices[n].ID]; model != "" {
				devices[n].Model = model
			}
		}
		break
	}
	return devices
}

func readLinuxRAIDDevices(root string) []raidDevice {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var devices []raidDevice
	for _, entry := range entries {
		base := filepath.Join(root, entry.Name())
		class := sysValue(base, "class")
		if !strings.HasPrefix(class, "0x01") {
			continue
		}
		driver := ""
		if link, err := os.Readlink(filepath.Join(base, "driver")); err == nil {
			driver = filepath.Base(link)
		}
		raid := strings.HasPrefix(class, "0x0104")
		if !raid && !isRAIDDriver(driver) {
			continue
		}
		id := func(field string) string { return strings.TrimPrefix(sysValue(base, field), "0x") }
		var subsystem string
		if vendor, device := id("subsystem_vendor"), id("subsystem_device"); vendor != "" && device != "" {
			subsystem = vendor + ":" + device
		}
		devices = append(devices, raidDevice{
			ID: entry.Name(), Model: sysValue(base, "label"), Driver: driver,
			Vendor: id("vendor"), Product: id("device"), Subsystem: subsystem, RAID: raid,
		})
	}
	return devices
}

func detectedRAIDDevices() []raidDevice { return readLinuxRAIDDevices("/sys/bus/pci/devices") }

func raidToolDirectory() string { return "/opt/storavix-agent/raid-tools" }

func prepareRAIDToolDirectory(dir string) error {
	if os.Geteuid() != 0 {
		return errors.New("RAID CLI installation requires root")
	}
	for _, path := range []string{"/opt", filepath.Dir(dir), dir} {
		if err := os.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode().Perm()&0022 != 0 || info.Sys().(*syscall.Stat_t).Uid != 0 {
			return fmt.Errorf("expected a root-owned RAID CLI directory: %s", path)
		}
	}
	return nil
}
