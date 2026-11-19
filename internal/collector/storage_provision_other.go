//go:build !linux && !windows

package collector

import "errors"

func detectedRAIDDevices() []raidDevice { return nil }

func raidToolDirectory() string { return "" }

func prepareRAIDToolDirectory(string) error { return errors.New("unsupported platform") }
