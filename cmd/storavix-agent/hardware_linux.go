package main

import (
	"flag"
	"log"
	"os"

	"github.com/iPapik/Storavix/internal/collector"
)

var hardwareOnly = flag.Bool("collect-hardware-storage", false, "write a local hardware RAID inventory for the Linux agent service")

func runHardwareInventory() bool {
	if !*hardwareOnly {
		return false
	}
	if os.Geteuid() != 0 {
		log.Fatal("hardware inventory service requires root")
	}
	if err := collector.ExportHardwareInventory(); err != nil {
		log.Fatalf("write hardware storage inventory: %v", err)
	}
	return true
}
