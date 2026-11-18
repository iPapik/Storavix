package main

import (
	"crypto/subtle"
	"encoding/asn1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosnmp/gosnmp"
	bolt "go.etcd.io/bbolt"
)

const snmpEventLimit = 20000

var snmpEventBucket = []byte("events")

type SNMPEvent struct {
	ID        uint64    `json:"id"`
	At        time.Time `json:"at"`
	DeviceID  string    `json:"deviceId,omitempty"`
	Device    string    `json:"device"`
	Source    string    `json:"source"`
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	OID       string    `json:"oid,omitempty"`
	Variables []SNMPVar `json:"variables,omitempty"`
}

type SNMPVar struct {
	OID   string `json:"oid"`
	Value string `json:"value"`
}

type SNMPTrapConfig struct {
	Enabled bool   `json:"enabled"`
	Bind    string `json:"bind"`
	Port    uint16 `json:"port"`
	Secret  string `json:"secret,omitempty"`
}

func (c SNMPTrapConfig) defaults() SNMPTrapConfig {
	if c.Port == 0 {
		return SNMPTrapConfig{Bind: "0.0.0.0", Port: 1162}
	}
	return c
}

type snmpJournal struct {
	db            *bolt.DB
	mu            sync.Mutex
	conn          *net.UDPConn
	listenAddress string
	workers       sync.WaitGroup
	secret        atomic.Value
	writeFailed   atomic.Bool
	startError    string
}

func openSNMPJournal(dir string) (*snmpJournal, error) {
	db, err := bolt.Open(filepath.Join(dir, "snmp.db"), 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(snmpEventBucket)
		return err
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &snmpJournal{db: db}, nil
}

func (j *snmpJournal) close() {
	j.mu.Lock()
	if j.conn != nil {
		_ = j.conn.Close()
	}
	j.mu.Unlock()
	j.workers.Wait()
	_ = j.db.Close()
}

func (j *snmpJournal) append(events []SNMPEvent) error {
	if len(events) == 0 {
		return nil
	}
	err := j.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(snmpEventBucket)
		var key [8]byte
		for _, e := range events {
			id, err := b.NextSequence()
			if err != nil {
				return err
			}
			e.ID = id
			data, err := json.Marshal(e)
			if err != nil {
				return err
			}
			binary.BigEndian.PutUint64(key[:], id)
			if err := b.Put(key[:], data); err != nil {
				return err
			}
			if id > snmpEventLimit {
				binary.BigEndian.PutUint64(key[:], id-snmpEventLimit)
				if err := b.Delete(key[:]); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		if !j.writeFailed.Swap(true) {
			log.Printf("write SNMP events: %v", err)
		}
	} else {
		j.writeFailed.Store(false)
	}
	return err
}

func snmpPollEvents(n NetworkConfig, old, current networkSample) []SNMPEvent {
	if n.Method != "snmp" {
		return nil
	}
	makeEvent := func(kind, text string) SNMPEvent {
		return SNMPEvent{At: current.at, DeviceID: n.ID, Device: n.Name, Source: n.Host, Kind: kind, Message: text}
	}
	var events []SNMPEvent
	if old.generation != current.generation || old.status != current.status {
		label := map[string]string{"up": "SNMP отвечает", "down": "SNMP не отвечает", "unknown": "Ошибка опроса SNMP"}[current.status]
		if current.status != "up" && current.message != "" {
			label += " · " + current.message
		}
		events = append(events, makeEvent("availability", label))
	}
	if old.generation != current.generation || old.status != "up" || current.status != "up" {
		return events
	}
	priorPorts := make(map[int]string, len(old.ports))
	for _, port := range old.ports {
		priorPorts[port.Index] = port.Status
	}
	for _, port := range current.ports {
		if previous, ok := priorPorts[port.Index]; ok && previous != port.Status {
			message := fmt.Sprintf("%s (ifIndex %d): %s → %s", port.Name, port.Index, previous, port.Status)
			events = append(events, makeEvent("interface", message))
		}
	}
	return events
}

func (s *server) configureSNMPTraps(c SNMPTrapConfig, save func() error) error {
	j := s.snmp
	secret, err := s.decrypt(c.Secret)
	if err != nil || c.Enabled && secret == "" {
		return fmt.Errorf("укажите community для уведомлений SNMP")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	addr := net.UDPAddr{IP: net.ParseIP(c.Bind), Port: int(c.Port)}
	var next *net.UDPConn
	if c.Enabled && (j.conn == nil || j.listenAddress != addr.String()) {
		next, err = net.ListenUDP("udp", &addr)
		if err != nil {
			return fmt.Errorf("не удалось открыть UDP %s: %w", addr.String(), err)
		}
	}
	if save != nil {
		if err := save(); err != nil {
			if next != nil {
				_ = next.Close()
			}
			return err
		}
	}
	j.secret.Store(secret)
	if j.conn != nil && (!c.Enabled || next != nil) {
		_ = j.conn.Close()
		j.conn = nil
	}
	if next != nil {
		j.conn = next
		j.listenAddress = addr.String()
		j.workers.Add(1)
		go s.receiveSNMPTraps(next)
	}
	j.startError = ""
	return nil
}

func (s *server) receiveSNMPTraps(conn *net.UDPConn) {
	j := s.snmp
	defer j.workers.Done()
	decoder := &gosnmp.GoSNMP{}
	buf := make([]byte, 8193)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if n == len(buf) {
			continue
		}
		notification := decodeSNMPDatagram(decoder, buf[:n])
		if notification == nil {
			continue
		}
		secret, _ := j.secret.Load().(string)
		event, ok := decodeSNMPEvent(notification, secret, from.IP.String())
		if !ok {
			continue
		}
		s.mu.Lock()
		for _, device := range s.state.Network {
			if device.Method == "snmp" && (device.Host == event.Source || s.networkSamples[device.ID].address == event.Source) {
				event.Device, event.DeviceID = device.Name, device.ID
				break
			}
		}
		s.mu.Unlock()
		if err := j.append([]SNMPEvent{event}); err != nil {
			continue
		}
		if notification.PDUType == gosnmp.InformRequest {
			notification.PDUType = gosnmp.GetResponse
			if response, err := notification.MarshalMsg(); err == nil {
				_, _ = conn.WriteToUDP(response, from)
			}
		}
	}
}

func decodeSNMPDatagram(decoder *gosnmp.GoSNMP, data []byte) (packet *gosnmp.SnmpPacket) {
	// Network input can reach the library's BER decoder before community validation.
	defer func() {
		if recover() != nil {
			packet = nil
		}
	}()
	var envelope asn1.RawValue
	rest, err := asn1.Unmarshal(data, &envelope)
	if err != nil || len(rest) != 0 || envelope.Class != asn1.ClassUniversal || envelope.Tag != asn1.TagSequence || !envelope.IsCompound {
		return nil
	}
	var version int
	if _, err := asn1.Unmarshal(envelope.Bytes, &version); err != nil || version < 0 || version > 1 {
		return nil
	}
	packet, err = decoder.SnmpDecodePacket(data)
	if err != nil {
		return nil
	}
	return packet
}

func decodeSNMPEvent(p *gosnmp.SnmpPacket, secret, source string) (SNMPEvent, bool) {
	event := SNMPEvent{At: time.Now().UTC(), Source: source, Device: source, Kind: "trap"}
	if p == nil || secret == "" || subtle.ConstantTimeCompare([]byte(p.Community), []byte(secret)) != 1 {
		return event, false
	}
	if p.Version == gosnmp.Version1 && p.PDUType == gosnmp.Trap {
		event.OID = p.Enterprise + ".0." + strconv.Itoa(p.SpecificTrap)
		if p.GenericTrap >= 0 && p.GenericTrap <= 5 {
			event.OID = ".1.3.6.1.6.3.1.1.5." + strconv.Itoa(p.GenericTrap+1)
		}
	} else if p.Version == gosnmp.Version2c && (p.PDUType == gosnmp.SNMPv2Trap || p.PDUType == gosnmp.InformRequest) {
		for _, v := range p.Variables {
			if strings.TrimPrefix(v.Name, ".") == "1.3.6.1.6.3.1.1.4.1.0" && v.Type == gosnmp.ObjectIdentifier {
				event.OID, _ = v.Value.(string)
			}
		}
	} else {
		return event, false
	}
	names := map[string]string{"1": "coldStart", "2": "warmStart", "3": "linkDown", "4": "linkUp", "5": "authenticationFailure"}
	event.Message = "Уведомление SNMP"
	if suffix, ok := strings.CutPrefix(strings.TrimPrefix(event.OID, "."), "1.3.6.1.6.3.1.1.5."); ok && names[suffix] != "" {
		event.Message = names[suffix]
	}
	for _, v := range p.Variables[:min(len(p.Variables), 32)] {
		value := ""
		if b, ok := v.Value.([]byte); ok {
			value = strings.ToValidUTF8(string(b[:min(256, len(b))]), "\uFFFD")
		} else {
			value = fmt.Sprint(v.Value)
		}
		value = value[:min(256, len(value))]
		event.Variables = append(event.Variables, SNMPVar{OID: v.Name, Value: value})
	}
	return event, true
}

func (s *server) snmpTrapStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	c := s.state.SNMPTraps.defaults()
	s.mu.Unlock()
	hasSecret := c.Secret != ""
	c.Secret = ""
	out := map[string]any{"config": c, "hasCommunity": hasSecret}
	if s.snmp == nil {
		out["error"] = "Журнал SNMP недоступен"
	} else {
		s.snmp.mu.Lock()
		out["active"] = s.snmp.conn != nil
		out["error"] = s.snmp.startError
		s.snmp.mu.Unlock()
	}
	sendJSON(w, http.StatusOK, out)
}

func (s *server) saveSNMPTraps(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled   bool   `json:"enabled"`
		Bind      string `json:"bind"`
		Port      uint16 `json:"port"`
		Community string `json:"community"`
	}
	if decodeJSON(r, &in) != nil || net.ParseIP(in.Bind) == nil || in.Port == 0 || len(in.Community) > 128 {
		problem(w, http.StatusBadRequest, "Проверьте IP, порт и community")
		return
	}
	if s.snmp == nil {
		problem(w, http.StatusServiceUnavailable, "Журнал SNMP недоступен")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.state.SNMPTraps
	c := SNMPTrapConfig{Enabled: in.Enabled, Bind: net.ParseIP(in.Bind).String(), Port: in.Port, Secret: previous.Secret}
	if in.Community != "" {
		var err error
		c.Secret, err = s.encrypt(in.Community)
		if err != nil {
			problem(w, http.StatusInternalServerError, "Не удалось сохранить community")
			return
		}
	}
	err := s.configureSNMPTraps(c, func() error {
		s.state.SNMPTraps = c
		if err := s.saveLocked(); err != nil {
			s.state.SNMPTraps = previous
			return fmt.Errorf("не удалось сохранить настройки")
		}
		return nil
	})
	if err != nil {
		problem(w, http.StatusConflict, err.Error())
		return
	}
	sendJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) getSNMPEvents(w http.ResponseWriter, r *http.Request) {
	if s.snmp == nil {
		problem(w, http.StatusServiceUnavailable, "Журнал SNMP недоступен")
		return
	}
	q := r.URL.Query()
	before, _ := strconv.ParseUint(q.Get("before"), 10, 64)
	kind, device := q.Get("kind"), q.Get("device")
	search := strings.ToLower(strings.TrimSpace(q.Get("q")))
	out := struct {
		Items []SNMPEvent `json:"items"`
		Next  uint64      `json:"next,omitempty"`
	}{Items: []SNMPEvent{}}
	err := s.snmp.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(snmpEventBucket).Cursor()
		key, value := c.Last()
		if before != 0 {
			var b [8]byte
			binary.BigEndian.PutUint64(b[:], before)
			if key, value = c.Seek(b[:]); key == nil {
				key, value = c.Last()
			} else {
				key, value = c.Prev()
			}
		}
		for ; key != nil; key, value = c.Prev() {
			if err := r.Context().Err(); err != nil {
				return err
			}
			var e SNMPEvent
			if json.Unmarshal(value, &e) != nil || kind != "" && e.Kind != kind || device != "" && e.DeviceID != device ||
				!strings.Contains(strings.ToLower(e.Device+" "+e.Source+" "+e.Message+" "+e.OID), search) {
				continue
			}
			if len(out.Items) == 100 {
				out.Next = out.Items[len(out.Items)-1].ID
				break
			}
			out.Items = append(out.Items, e)
		}
		return nil
	})
	if err != nil {
		problem(w, http.StatusInternalServerError, "Не удалось прочитать журнал SNMP")
		return
	}
	sendJSON(w, http.StatusOK, out)
}
