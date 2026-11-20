//go:build !windows && !linux

package collector

func Temperature() *float64 { return nil }
