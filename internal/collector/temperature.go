package collector

import (
	"strconv"
	"strings"
)

func maxTemperature(raw string, divisor, offset float64, highest *float64) *float64 {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
	if err != nil {
		return highest
	}
	celsius := float64(value)/divisor + offset
	if celsius < -40 || celsius > 150 || (highest != nil && *highest >= celsius) {
		return highest
	}
	return &celsius
}
