package main

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

func TestSNMPInformPersistsAndAcknowledges(t *testing.T) {
	dir := t.TempDir()
	s := &server{key: make([]byte, 32), state: State{
		Network: []NetworkConfig{{ID: "switch-id", Name: "Test switch", Host: "127.0.0.1", Method: "snmp"}},
	}}
	journal, err := openSNMPJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.snmp = journal
	closed := false
	defer func() {
		if !closed {
			journal.close()
		}
	}()
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.LocalAddr().(*net.UDPAddr)
	probe.Close()
	secret, err := s.encrypt("notifications-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.configureSNMPTraps(SNMPTrapConfig{Enabled: true, Bind: addr.IP.String(), Port: uint16(addr.Port), Secret: secret}, nil); err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	write := func(data []byte) {
		t.Helper()
		if _, err := conn.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	inform := gosnmp.SnmpPacket{
		Version: gosnmp.Version2c, Community: "incorrect", RequestID: 42, PDUType: gosnmp.InformRequest,
		Variables: []gosnmp.SnmpPDU{
			{Name: sysUpTimeOID, Type: gosnmp.TimeTicks, Value: uint32(1000)},
			{Name: ".1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.6.3.1.1.5.3"},
		},
	}
	sendInform := func() {
		t.Helper()
		data, err := inform.MarshalMsg()
		if err != nil {
			t.Fatal(err)
		}
		write(data)
	}
	write([]byte{0x30, 0x03, 0x02, 0x01, 0x03})
	sendInform()
	inform.Community = "notifications-test"
	sendInform()
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var buf [8192]byte
	n, err := conn.Read(buf[:])
	if err != nil {
		t.Fatal(err)
	}
	response := decodeSNMPDatagram(&gosnmp.GoSNMP{}, buf[:n])
	if response == nil || response.PDUType != gosnmp.GetResponse || response.RequestID != 42 || response.Community != inform.Community {
		t.Fatalf("unexpected acknowledgement: %+v", response)
	}
	journal.close()
	closed = true
	s.snmp, err = openSNMPJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.snmp.close()
	w := httptest.NewRecorder()
	s.getSNMPEvents(w, httptest.NewRequest("GET", "/api/network/events?device=switch-id&kind=trap&q=linkDown", nil))
	var result struct{ Items []SNMPEvent }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Items) != 1 ||
		result.Items[0].Device != "Test switch" || result.Items[0].Message != "linkDown" || len(result.Items[0].Variables) != 2 {
		t.Fatalf("unexpected stored events: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.getSNMPEvents(w, httptest.NewRequest("GET", "/api/network/events", nil))
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Items) != 1 {
		t.Fatalf("malformed or unauthenticated events were accepted: %s", w.Body.String())
	}
}

func TestSNMPPollEventsLogOnlyTransitions(t *testing.T) {
	device := NetworkConfig{ID: "switch-id", Method: "snmp"}
	up := networkSample{status: "up", generation: "test-generation", ports: []NetworkPort{{Index: 1, Name: "Gi1", Status: "up"}}}
	changed := up
	changed.ports = []NetworkPort{{Index: 1, Name: "Gi1", Status: "down"}}
	down := networkSample{status: "down", generation: "test-generation"}
	for _, tc := range []struct {
		old, current networkSample
		want         string
	}{
		{networkSample{}, up, "availability"},
		{up, up, ""},
		{up, changed, "interface"},
		{changed, down, "availability"},
	} {
		events := snmpPollEvents(device, tc.old, tc.current)
		if tc.want == "" && len(events) != 0 || tc.want != "" && (len(events) != 1 || events[0].Kind != tc.want) {
			t.Fatalf("got %+v, wanted %q", events, tc.want)
		}
	}
}
