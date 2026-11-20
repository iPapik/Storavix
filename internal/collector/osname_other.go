//go:build !linux && !windows

package collector

func OSName() string { return "" }
