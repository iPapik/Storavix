package main

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

const entitySensorEntry = ".1.3.6.1.2.1.99.1.1.1"

var validTemperatureOID = regexp.MustCompile(`^\.(?:[0-9]{1,10}\.){1,30}[0-9]{1,10}$`)

type entitySensor struct {
	columns [6]int64
	present [6]bool
}

func (s entitySensor) celsius() *float64 {
	// RFC 3433 columns 1–5: type, scale, precision, value, operational state.
	for _, present := range s.present[1:] {
		if !present {
			return nil
		}
	}
	kind, scale, precision, value, status := s.columns[1], s.columns[2], s.columns[3], s.columns[4], s.columns[5]
	if kind != 8 || status != 1 || scale < 1 || scale > 17 || precision < -8 || precision > 9 ||
		value <= -1000000000 || value >= 1000000000 {
		return nil
	}
	return validCelsius(float64(value) * math.Pow10(int(3*(scale-9)-max(precision, 0))))
}

func validCelsius(c float64) *float64 {
	if c >= -40 && c <= 150 {
		return &c
	}
	return nil
}

func temperatureInteger(item gosnmp.SnmpPDU) (int64, bool) {
	switch item.Type {
	case gosnmp.Integer, gosnmp.Gauge32, gosnmp.Uinteger32:
		return gosnmp.ToBigInt(item.Value).Int64(), true
	default:
		return 0, false
	}
}

func pollSNMPTemperature(ctx context.Context, client *gosnmp.GoSNMP, device NetworkConfig) *float64 {
	temperatureCtx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	client.Context = temperatureCtx
	defer func() { client.Context = ctx }()
	if device.TemperatureOID != "" {
		packet, err := client.Get([]string{device.TemperatureOID})
		if err != nil || packet == nil || packet.Error != gosnmp.NoError || len(packet.Variables) != 1 {
			return nil
		}
		if value, ok := temperatureInteger(packet.Variables[0]); ok {
			return validCelsius(float64(value) / float64(max(1, device.TemperatureDivisor)))
		}
		return nil
	}

	sensors := make(map[string]entitySensor)
	cells := 0
	_ = client.BulkWalk(entitySensorEntry, func(item gosnmp.SnmpPDU) error {
		cells++
		if cells > 512 {
			return errors.New("sensor walk limit")
		}
		suffix, ok := strings.CutPrefix(item.Name, entitySensorEntry+".")
		if !ok {
			return nil
		}
		column, index, _ := strings.Cut(suffix, ".")
		i, err := strconv.Atoi(column)
		value, numeric := temperatureInteger(item)
		if err == nil && numeric && index != "" && i >= 1 && i <= 5 {
			sensor := sensors[index]
			sensor.columns[i], sensor.present[i] = value, true
			sensors[index] = sensor
		}
		return nil
	})
	var highest *float64
	for _, sensor := range sensors {
		if c := sensor.celsius(); c != nil && (highest == nil || *c > *highest) {
			highest = c
		}
	}
	return highest
}
