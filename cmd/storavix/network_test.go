package main

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

func TestSNMPSwitchActivity(t *testing.T) {
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go serveSNMPFixture(listener)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	config := NetworkConfig{Host: "127.0.0.1", Port: uint16(listener.LocalAddr().(*net.UDPAddr).Port)}
	start := time.Now()
	sample := networkSample{at: start, status: "down"}
	pollSNMP(ctx, config, "test-read-only", &sample)
	if sample.status != "up" || sample.system != "switch-fixture" || sample.uptime == nil ||
		len(sample.ports) != 2 || sample.ports[0].Name != "Gi1" || sample.ports[0].Status != "up" ||
		sample.ports[1].Status != "disabled" || sample.ports[0].rx == nil || *sample.ports[0].rx != 40000 {
		t.Fatalf("SNMP sample incomplete: %+v, ports: %+v", sample, sample.ports)
	}
	uptime, rx, tx := *sample.uptime-4000, uint64(20000), uint64(30000)
	old := networkSample{at: start.Add(-40 * time.Second), uptime: &uptime,
		ports: []NetworkPort{{Index: 1, Name: "Gi1", rx: &rx, tx: &tx}}}
	addPortRates(&sample, old)
	if got := sample.ports[0].RxRate; got == nil || *got != 500 {
		t.Fatalf("unexpected counter rate: %v", got)
	}
	if got := sample.ports[0].TxRate; got == nil || *got != 250 {
		t.Fatalf("unexpected counter rate: %v", got)
	}
}

func serveSNMPFixture(conn *net.UDPConn) {
	client := &gosnmp.GoSNMP{Version: gosnmp.Version2c, Community: "test-read-only"}
	rows := map[string][]gosnmp.SnmpPDU{
		ifNameOID: {{Name: ifNameOID + ".1", Type: gosnmp.OctetString, Value: []byte("Gi1")}},
	}
	for i := 1; i <= 2; i++ {
		admin := 1
		if i == 2 {
			admin = 2
		}
		for oid, value := range map[string]int{ifOperOID: i, ifAdminOID: admin} {
			rows[oid] = append(rows[oid], gosnmp.SnmpPDU{Name: fmt.Sprintf("%s.%d", oid, i), Type: gosnmp.Integer, Value: value})
		}
		for _, oid := range []string{ifRxOID, ifTxOID} {
			rows[oid] = append(rows[oid], gosnmp.SnmpPDU{Name: fmt.Sprintf("%s.%d", oid, i), Type: gosnmp.Counter64, Value: uint64(40000)})
		}
	}
	data := make([]byte, 4096)
	for {
		n, peer, err := conn.ReadFromUDP(data)
		if err != nil {
			return
		}
		request, err := client.SnmpDecodePacket(data[:n])
		if err != nil || request.Community != client.Community || len(request.Variables) == 0 {
			continue
		}
		response := &gosnmp.SnmpPacket{Version: request.Version, Community: request.Community, RequestID: request.RequestID,
			PDUType: gosnmp.GetResponse}
		switch request.PDUType {
		case gosnmp.GetRequest:
			response.Variables = []gosnmp.SnmpPDU{
				{Name: sysNameOID, Type: gosnmp.OctetString, Value: []byte("switch-fixture")},
				{Name: sysUpTimeOID, Type: gosnmp.TimeTicks, Value: uint32(10000)},
			}
		case gosnmp.GetBulkRequest:
			oid := request.Variables[0].Name
			response.Variables = rows[oid]
			if len(response.Variables) == 0 {
				response.Variables = []gosnmp.SnmpPDU{{Name: oid, Type: gosnmp.EndOfMibView}}
			}
		}
		packet, err := response.MarshalMsg()
		if err == nil {
			_, _ = conn.WriteToUDP(packet, peer)
		}
	}
}
