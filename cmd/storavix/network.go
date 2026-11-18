package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
)

const networkInterval = 45 * time.Second

type NetworkConfig struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Method     string `json:"method"`
	Port       uint16 `json:"port"`
	Secret     string `json:"secret,omitempty"`
	Generation string `json:"generation"`
}

type NetworkPort struct {
	Index  int     `json:"index"`
	Name   string  `json:"name"`
	Status string  `json:"status"`
	RxRate *uint64 `json:"rxRate,omitempty"`
	TxRate *uint64 `json:"txRate,omitempty"`
	rx     *uint64
	tx     *uint64
}

type NetworkDevice struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Host      string        `json:"host"`
	Method    string        `json:"method"`
	Port      uint16        `json:"port"`
	Status    string        `json:"status"`
	CheckedAt *time.Time    `json:"checkedAt,omitempty"`
	System    string        `json:"system,omitempty"`
	Message   string        `json:"message,omitempty"`
	Ports     []NetworkPort `json:"ports,omitempty"`
	PortCount int           `json:"portCount"`
	UpPorts   int           `json:"upPorts"`
	TotalRate *uint64       `json:"totalRate,omitempty"`
}

type networkSample struct {
	generation string
	address    string
	status     string
	at         time.Time
	system     string
	message    string
	ports      []NetworkPort
	uptime     *uint64
}

var dnsLabel = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func validNetworkHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsUnspecified() && !ip.IsMulticast()
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if !dnsLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func (s *server) getNetwork(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	devices := s.networkDevicesLocked(true)
	s.mu.Unlock()
	sendJSON(w, http.StatusOK, devices)
}

func (s *server) networkDevicesLocked(includePorts bool) []NetworkDevice {
	devices := make([]NetworkDevice, 0, len(s.state.Network))
	now := time.Now()
	for _, n := range s.state.Network {
		d := NetworkDevice{ID: n.ID, Name: n.Name, Host: n.Host, Method: n.Method, Port: n.Port, Status: "pending"}
		if sample, ok := s.networkSamples[n.ID]; ok && sample.generation == n.Generation {
			at := sample.at
			d.CheckedAt = &at
			d.Status, d.System, d.Message = sample.status, sample.system, sample.message
			if includePorts {
				d.Ports = sample.ports
			}
			d.PortCount = len(sample.ports)
			var rate uint64
			var measured bool
			for _, port := range sample.ports {
				if port.Status == "up" {
					d.UpPorts++
				}
				for _, value := range []*uint64{port.RxRate, port.TxRate} {
					if value != nil {
						rate += *value
						measured = true
					}
				}
			}
			if measured {
				d.TotalRate = &rate
			}
			if now.Sub(at) > 3*networkInterval {
				d.Status, d.Message = "stale", "Ожидание нового опроса"
			}
		}
		devices = append(devices, d)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name+devices[i].ID < devices[j].Name+devices[j].ID })
	return devices
}

func (s *server) putNetworkDevice(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name      string `json:"name"`
		Host      string `json:"host"`
		Method    string `json:"method"`
		Port      uint16 `json:"port"`
		Community string `json:"community"`
	}
	if decodeJSON(r, &input) != nil {
		problem(w, http.StatusBadRequest, "Некорректные настройки коммутатора")
		return
	}
	input.Name, input.Host = strings.TrimSpace(input.Name), strings.TrimSpace(input.Host)
	if input.Name == "" || len([]rune(input.Name)) > 64 || !validNetworkHost(input.Host) || input.Port == 0 ||
		(input.Method != "tcp" && input.Method != "snmp") || len(input.Community) > 128 {
		problem(w, http.StatusBadRequest, "Проверьте имя, адрес, способ опроса и порт")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := r.PathValue("id")
	devices := append([]NetworkConfig{}, s.state.Network...)
	index := -1
	for i, existing := range devices {
		if existing.ID == id {
			index = i
		}
	}
	newDevice := id == "new"
	if !newDevice && index < 0 {
		problem(w, http.StatusNotFound, "Коммутатор не найден")
		return
	}
	if newDevice {
		if len(devices) >= 128 {
			problem(w, http.StatusConflict, "Допустимо до 128 коммутаторов")
			return
		}
		id = hex.EncodeToString(randomBytes(12))
	}
	n := NetworkConfig{ID: id, Name: input.Name, Host: input.Host, Method: input.Method, Port: input.Port, Generation: hex.EncodeToString(randomBytes(12))}
	if n.Method == "snmp" {
		switch {
		case input.Community != "":
			secret, err := s.encrypt(input.Community)
			if err != nil {
				problem(w, http.StatusInternalServerError, "Не удалось сохранить SNMP community")
				return
			}
			n.Secret = secret
		case index >= 0:
			n.Secret = devices[index].Secret
		}
		if n.Secret == "" {
			problem(w, http.StatusBadRequest, "Введите read-only SNMP community")
			return
		}
	}
	if newDevice {
		devices = append(devices, n)
	} else {
		devices[index] = n
	}
	previous := s.state.Network
	s.state.Network = devices
	if err := s.saveLocked(); err != nil {
		s.state.Network = previous
		problem(w, http.StatusInternalServerError, "Не удалось сохранить коммутатор")
		return
	}
	delete(s.networkSamples, n.ID)
	s.wakeNetwork()
	sendJSON(w, http.StatusOK, map[string]string{"id": n.ID})
}

func (s *server) deleteNetworkDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.state.Network
	devices := make([]NetworkConfig, 0, len(previous))
	for _, device := range previous {
		if device.ID != id {
			devices = append(devices, device)
		}
	}
	if len(devices) == len(previous) {
		problem(w, http.StatusNotFound, "Коммутатор не найден")
		return
	}
	s.state.Network = devices
	if err := s.saveLocked(); err != nil {
		s.state.Network = previous
		problem(w, http.StatusInternalServerError, "Не удалось удалить коммутатор")
		return
	}
	delete(s.networkSamples, id)
	sendJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *server) requestNetworkPoll(w http.ResponseWriter, _ *http.Request) {
	s.wakeNetwork()
	sendJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *server) wakeNetwork() {
	select {
	case s.networkWake <- struct{}{}:
	default:
	}
}

func (s *server) pollNetwork(ctx context.Context) {
	ticker := time.NewTicker(networkInterval)
	defer ticker.Stop()
	for {
		s.pollNetworkOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.networkWake:
		}
	}
}

func (s *server) pollNetworkOnce(ctx context.Context) {
	s.mu.Lock()
	devices := append([]NetworkConfig(nil), s.state.Network...)
	s.mu.Unlock()
	jobs := make(chan NetworkConfig, len(devices))
	for _, device := range devices {
		jobs <- device
	}
	close(jobs)
	var workers sync.WaitGroup
	for i := 0; i < min(16, len(devices)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for n := range jobs {
				if ctx.Err() != nil {
					return
				}
				s.pollDevice(ctx, n)
			}
		}()
	}
	workers.Wait()
}

func (s *server) pollDevice(ctx context.Context, n NetworkConfig) {
	s.mu.Lock()
	old := s.networkSamples[n.ID]
	s.mu.Unlock()
	if old.generation == n.Generation && time.Since(old.at) < 4*time.Second {
		return
	}
	start := time.Now().UTC()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sample := networkSample{generation: n.Generation, at: start, status: "down"}
	if n.Method == "tcp" {
		dialer := net.Dialer{}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(n.Host, strconv.Itoa(int(n.Port))))
		if err == nil {
			_ = conn.Close()
			sample.status = "up"
		} else {
			sample.message = "Подключение к настроенному TCP-порту не удалось"
		}
	} else {
		secret, err := s.decrypt(n.Secret)
		if err != nil {
			sample.status = "unknown"
			sample.message = "Повторно сохраните SNMP community"
		} else {
			pollSNMP(ctx, n, secret, &sample)
		}
	}
	if old.generation == n.Generation && old.status == "up" && sample.status == "up" {
		addPortRates(&sample, old)
	}
	s.mu.Lock()
	accepted := false
	for _, current := range s.state.Network {
		if current.ID == n.ID && current.Generation == n.Generation {
			s.networkSamples[n.ID] = sample
			accepted = true
			break
		}
	}
	s.mu.Unlock()
	if accepted && s.snmp != nil {
		_ = s.snmp.append(snmpPollEvents(n, old, sample))
	}
}

const (
	sysNameOID   = ".1.3.6.1.2.1.1.5.0"
	sysUpTimeOID = ".1.3.6.1.2.1.1.3.0"
	ifNameOID    = ".1.3.6.1.2.1.31.1.1.1.1"
	ifDescrOID   = ".1.3.6.1.2.1.2.2.1.2"
	ifAdminOID   = ".1.3.6.1.2.1.2.2.1.7"
	ifOperOID    = ".1.3.6.1.2.1.2.2.1.8"
	ifRxOID      = ".1.3.6.1.2.1.31.1.1.1.6"
	ifTxOID      = ".1.3.6.1.2.1.31.1.1.1.10"
)

func pollSNMP(ctx context.Context, n NetworkConfig, community string, sample *networkSample) {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, n.Host)
	if err != nil || len(addresses) == 0 {
		sample.message = "Адрес коммутатора не разрешается"
		return
	}
	sample.address = addresses[0].IP.String()
	client := &gosnmp.GoSNMP{
		Target: sample.address, Port: n.Port, Transport: "udp", Community: community,
		Version: gosnmp.Version2c, Timeout: 750 * time.Millisecond, MaxRepetitions: 24, Context: ctx,
	}
	if client.Connect() != nil {
		sample.message = "Не удалось начать SNMP-опрос"
		return
	}
	defer client.Conn.Close()
	packet, err := client.Get([]string{sysNameOID, sysUpTimeOID})
	if err != nil || packet == nil || packet.Error != gosnmp.NoError {
		sample.message = "Проверьте доступность UDP-порта и SNMP community"
		return
	}
	for _, item := range packet.Variables {
		switch item.Name {
		case sysNameOID:
			if item.Type == gosnmp.OctetString {
				sample.system = snmpText(item.Value)
				sample.status = "up"
			}
		case sysUpTimeOID:
			if item.Type == gosnmp.TimeTicks {
				value := gosnmp.ToBigInt(item.Value).Uint64()
				sample.uptime = &value
				sample.status = "up"
			}
		}
	}
	if sample.status != "up" {
		sample.message = "Коммутатор не открыл доступ к системным OID SNMP"
		return
	}
	ports, complete := pollSNMPPorts(client)
	sample.ports = ports
	if !complete || len(ports) == 0 {
		sample.message = "Таблица интерфейсов SNMP доступна частично или отсутствует"
	}
}

func snmpText(value any) string {
	b, _ := value.([]byte)
	runes := []rune(strings.TrimSpace(string(b)))
	return string(runes[:min(96, len(runes))])
}

func pollSNMPPorts(client *gosnmp.GoSNMP) ([]NetworkPort, bool) {
	ports := map[int]*NetworkPort{}
	complete := true
	admin := map[int]int64{}
	limited := errors.New("too many SNMP interfaces")
	for _, oid := range []string{ifOperOID, ifAdminOID, ifNameOID, ifDescrOID, ifRxOID, ifTxOID} {
		if err := client.BulkWalk(oid, func(p gosnmp.SnmpPDU) error {
			index, err := strconv.Atoi(strings.TrimPrefix(p.Name, oid+"."))
			if err != nil || index <= 0 {
				return nil
			}
			port := ports[index]
			if oid == ifOperOID && port == nil && p.Type == gosnmp.Integer {
				if len(ports) >= 256 {
					return limited
				}
				port = &NetworkPort{Index: index, Status: "unknown"}
				ports[index] = port
			}
			if port == nil {
				return nil
			}
			switch oid {
			case ifOperOID:
				switch gosnmp.ToBigInt(p.Value).Int64() {
				case 1:
					port.Status = "up"
				case 2, 6, 7:
					port.Status = "down"
				}
			case ifAdminOID:
				if p.Type == gosnmp.Integer {
					admin[index] = gosnmp.ToBigInt(p.Value).Int64()
				}
			case ifNameOID, ifDescrOID:
				if port.Name == "" && p.Type == gosnmp.OctetString {
					port.Name = snmpText(p.Value)
				}
			case ifRxOID, ifTxOID:
				if p.Type == gosnmp.Counter64 {
					value := gosnmp.ToBigInt(p.Value).Uint64()
					if oid == ifRxOID {
						port.rx = &value
					} else {
						port.tx = &value
					}
				}
			}
			return nil
		}); err != nil {
			complete = false
		}
	}
	out := make([]NetworkPort, 0, len(ports))
	for _, port := range ports {
		if admin[port.Index] == 2 {
			port.Status = "disabled"
		}
		if port.Name == "" {
			port.Name = fmt.Sprintf("ifIndex %d", port.Index)
		}
		out = append(out, *port)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, complete
}

func addPortRates(current *networkSample, previous networkSample) {
	elapsed := current.at.Sub(previous.at).Seconds()
	if elapsed <= 0 || elapsed > 3*networkInterval.Seconds() || current.uptime == nil || previous.uptime == nil ||
		*current.uptime < *previous.uptime || float64(*current.uptime-*previous.uptime)/100 < elapsed/2 {
		return
	}
	oldPorts := make(map[int]NetworkPort, len(previous.ports))
	for _, p := range previous.ports {
		oldPorts[p.Index] = p
	}
	rate := func(current, previous *uint64) *uint64 {
		if current == nil || previous == nil || *current < *previous {
			return nil
		}
		bytesPerSecond := uint64(float64(*current-*previous) / elapsed)
		return &bytesPerSecond
	}
	for i := range current.ports {
		p := &current.ports[i]
		old, ok := oldPorts[p.Index]
		if ok && old.Name == p.Name {
			p.RxRate, p.TxRate = rate(p.rx, old.rx), rate(p.tx, old.tx)
		}
	}
}
