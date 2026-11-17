package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	bolt "go.etcd.io/bbolt"
)

const syslogFrameLimit = 8192

var syslogBucket = []byte("messages")

type SyslogConfig struct {
	Enabled bool   `json:"enabled"`
	Bind    string `json:"bind"`
	Port    uint16 `json:"port"`
	UDP     bool   `json:"udp"`
	TCP     bool   `json:"tcp"`
	Limit   int    `json:"limit"`
}

func (c SyslogConfig) defaults() SyslogConfig {
	if c.Port == 0 {
		return SyslogConfig{Bind: "0.0.0.0", Port: 5514, UDP: true, TCP: true, Limit: 10000}
	}
	return c
}

func (c SyslogConfig) address() string {
	return net.JoinHostPort(c.Bind, strconv.Itoa(int(c.Port)))
}

type SyslogEntry struct {
	ID         uint64    `json:"id"`
	ReceivedAt time.Time `json:"receivedAt"`
	Timestamp  string    `json:"timestamp,omitempty"`
	Source     string    `json:"source"`
	Transport  string    `json:"transport"`
	Host       string    `json:"host,omitempty"`
	App        string    `json:"app,omitempty"`
	Severity   int       `json:"severity"`
	Facility   int       `json:"facility"`
	Message    string    `json:"message"`
}

type syslogBinding struct {
	address string
	start   func()
	close   func()
}

type syslogReceiver struct {
	db           *bolt.DB
	mu           sync.Mutex
	bindings     map[string]syslogBinding
	queue        chan SyslogEntry
	connections  chan struct{}
	queueClosed  chan struct{}
	workers      sync.WaitGroup
	listenError  string
	writeError   atomic.Bool
	pendingDrops atomic.Uint64
	limit        atomic.Int32
}

func openSyslog(dir string) (*syslogReceiver, error) {
	db, err := bolt.Open(filepath.Join(dir, "syslog.db"), 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(syslogBucket)
		return err
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	r := &syslogReceiver{db: db, bindings: map[string]syslogBinding{}, queue: make(chan SyslogEntry, 2048),
		connections: make(chan struct{}, 64), queueClosed: make(chan struct{})}
	r.limit.Store(10000)
	go r.record()
	return r, nil
}

func (r *syslogReceiver) close() {
	r.mu.Lock()
	for _, listener := range r.bindings {
		listener.close()
	}
	r.mu.Unlock()
	r.workers.Wait()
	close(r.queue)
	<-r.queueClosed
	_ = r.db.Close()
}

func (r *syslogReceiver) configure(c SyslogConfig, save func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	requested := map[string]bool{"udp": c.Enabled && c.UDP, "tcp": c.Enabled && c.TCP}
	next := make(map[string]syslogBinding)
	var newBindings []syslogBinding
	defer func() {
		for _, b := range newBindings {
			b.close()
		}
	}()
	for _, transport := range []string{"udp", "tcp"} {
		if !requested[transport] {
			continue
		}
		if old, ok := r.bindings[transport]; ok && old.address == c.address() {
			next[transport] = old
			continue
		}
		b, err := r.bind(transport, c.address())
		if err != nil {
			return fmt.Errorf("%s %s: %w", strings.ToUpper(transport), c.address(), err)
		}
		next[transport] = b
		newBindings = append(newBindings, b)
	}
	if save != nil {
		if err := save(); err != nil {
			return err
		}
	}
	for transport, b := range r.bindings {
		if replacement, ok := next[transport]; !ok || replacement.address != b.address {
			b.close()
		}
	}
	r.bindings = next
	r.limit.Store(int32(c.Limit))
	r.listenError = ""
	for _, b := range newBindings {
		r.workers.Add(1)
		go func(start func()) {
			defer r.workers.Done()
			start()
		}(b.start)
	}
	newBindings = nil
	return nil
}

func (r *syslogReceiver) bind(transport, address string) (syslogBinding, error) {
	if transport == "udp" {
		p, err := net.ListenPacket("udp", address)
		if err != nil {
			return syslogBinding{}, err
		}
		return syslogBinding{address: address, close: func() { _ = p.Close() }, start: func() {
			buffer := make([]byte, syslogFrameLimit+1)
			for {
				n, source, err := p.ReadFrom(buffer)
				if err != nil {
					return
				}
				if n > syslogFrameLimit {
					r.pendingDrops.Add(1)
					continue
				}
				r.enqueue(source, "udp", buffer[:n])
			}
		}}, nil
	}
	l, err := net.Listen("tcp", address)
	if err != nil {
		return syslogBinding{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return syslogBinding{address: address, close: func() { cancel(); _ = l.Close() }, start: func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			select {
			case r.connections <- struct{}{}:
				r.workers.Add(1)
				go r.readTCP(ctx, conn)
			default:
				_ = conn.Close()
				r.pendingDrops.Add(1)
			}
		}
	}}, nil
}

func (r *syslogReceiver) readTCP(ctx context.Context, c net.Conn) {
	defer r.workers.Done()
	defer func() { <-r.connections }()
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	reader := bufio.NewReaderSize(c, syslogFrameLimit+1)
	for {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Minute))
		frame, err := readSyslogFrame(reader)
		if len(frame) != 0 && (err == nil || errors.Is(err, io.EOF)) {
			r.enqueue(c.RemoteAddr(), "tcp", frame)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				var timeout net.Error
				if !errors.As(err, &timeout) || !timeout.Timeout() {
					r.pendingDrops.Add(1)
				}
			}
			return
		}
	}
}

func readSyslogFrame(r *bufio.Reader) ([]byte, error) {
	first, err := r.Peek(1)
	if err != nil {
		return nil, err
	}
	if first[0] >= '1' && first[0] <= '9' {
		prefix, err := r.Peek(1)
		if err != nil {
			return nil, err
		}
		for length := 2; length <= 6 && prefix[len(prefix)-1] >= '0' && prefix[len(prefix)-1] <= '9'; length++ {
			prefix, err = r.Peek(length)
			if err != nil {
				return nil, err
			}
		}
		if prefix[len(prefix)-1] == ' ' {
			n, err := strconv.Atoi(string(prefix[:len(prefix)-1]))
			if err != nil || n < 1 || n > syslogFrameLimit {
				return nil, errors.New("invalid syslog octet length")
			}
			_, _ = r.Discard(len(prefix))
			msg := make([]byte, n)
			_, err = io.ReadFull(r, msg)
			if err != nil {
				return nil, err
			}
			return msg, nil
		}
	}
	frame, err := r.ReadSlice('\n')
	if len(frame) > syslogFrameLimit && (len(frame) > syslogFrameLimit+1 || frame[len(frame)-1] != '\n') {
		return nil, errors.New("syslog frame too long")
	}
	return frame, err
}

func (r *syslogReceiver) enqueue(addr net.Addr, transport string, data []byte) {
	text := strings.ToValidUTF8(string(bytes.TrimSpace(data)), "\uFFFD")
	if text == "" {
		return
	}
	ip, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		ip = addr.String()
	}
	entry := parseSyslog(text, time.Now().UTC())
	entry.Source, entry.Transport = ip, transport
	select {
	case r.queue <- entry:
	default:
		r.pendingDrops.Add(1)
	}
}

func parseSyslog(raw string, now time.Time) SyslogEntry {
	entry := SyslogEntry{ReceivedAt: now, Severity: -1, Facility: -1, Message: raw}
	eof := strings.IndexByte(raw, '>')
	if !strings.HasPrefix(raw, "<") || eof < 2 || eof > 4 {
		return entry
	}
	pri, err := strconv.Atoi(raw[1:eof])
	if err != nil || pri < 0 || pri > 191 {
		return entry
	}
	entry.Severity, entry.Facility = pri%8, pri/8
	body := strings.TrimLeft(raw[eof+1:], " ")
	entry.Message = body
	fields := strings.SplitN(body, " ", 7)
	if len(fields) == 7 && fields[0] == "1" {
		if fields[1] != "-" {
			entry.Timestamp = fields[1]
		}
		if fields[2] != "-" {
			entry.Host = fields[2]
		}
		if fields[3] != "-" {
			entry.App = fields[3]
		}
		entry.Message = stripSyslogData(fields[6])
		return entry
	}
	if len(body) > 16 && body[15] == ' ' {
		date, err := time.Parse("Jan _2 15:04:05", body[:15])
		if err == nil {
			entry.Timestamp = date.Format("Jan _2 15:04:05")
			host, rest, _ := strings.Cut(strings.TrimSpace(body[16:]), " ")
			entry.Host = host
			entry.Message = strings.TrimSpace(rest)
		}
	}
	if app, message, found := strings.Cut(entry.Message, ": "); found && len(app) <= 64 && !strings.ContainsAny(app, " \t") {
		entry.App, entry.Message = app, message
	}
	return entry
}

func stripSyslogData(input string) string {
	if input == "-" || strings.HasPrefix(input, "- ") {
		return strings.TrimPrefix(input[1:], " ")
	}
	original := input
	for strings.HasPrefix(input, "[") {
		input = input[1:]
		quoted, escaped := false, false
		end := -1
		for i := 0; i < len(input); i++ {
			c := input[i]
			switch {
			case escaped:
				escaped = false
			case c == '\\' && quoted:
				escaped = true
			case c == '"':
				quoted = !quoted
			case c == ']' && !quoted:
				end = i
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			return original
		}
		input = input[end+1:]
	}
	return strings.TrimPrefix(strings.TrimPrefix(input, " "), "\uFEFF")
}

func (r *syslogReceiver) record() {
	defer close(r.queueClosed)
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]SyslogEntry, 0, 128)
	var lastLimit uint64
	flush := func() {
		limit := uint64(r.limit.Load())
		if len(batch) == 0 && limit == lastLimit {
			return
		}
		err := r.db.Update(func(tx *bolt.Tx) error {
			bucket := tx.Bucket(syslogBucket)
			for _, item := range batch {
				id, err := bucket.NextSequence()
				if err != nil {
					return err
				}
				item.ID = id
				value, err := json.Marshal(item)
				if err != nil {
					return err
				}
				var key [8]byte
				binary.BigEndian.PutUint64(key[:], id)
				if err := bucket.Put(key[:], value); err != nil {
					return err
				}
			}
			if id := bucket.Sequence(); id > limit {
				cutoff := id - limit
				cursor := bucket.Cursor()
				var oldKeys [][8]byte
				for key, _ := cursor.First(); key != nil && binary.BigEndian.Uint64(key) <= cutoff; key, _ = cursor.Next() {
					oldKeys = append(oldKeys, [8]byte(key))
				}
				for _, key := range oldKeys {
					if err := bucket.Delete(key[:]); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err != nil {
			r.pendingDrops.Add(uint64(len(batch)))
			if !r.writeError.Swap(true) {
				log.Printf("write syslog database: %v", err)
			}
		} else {
			lastLimit = limit
			r.writeError.Store(false)
		}
		batch = batch[:0]
	}
	for {
		select {
		case entry, ok := <-r.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, entry)
			if len(batch) == cap(batch) {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

type syslogStatus struct {
	Config SyslogConfig `json:"config"`
	Active []string     `json:"active"`
	Error  string       `json:"error,omitempty"`
	Drops  uint64       `json:"drops"`
}

func (s *server) getSyslogSettings(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	conf := s.state.Syslog.defaults()
	status := s.syslogState(conf)
	s.mu.Unlock()
	sendJSON(w, http.StatusOK, status)
}

func (s *server) syslogState(c SyslogConfig) syslogStatus {
	out := syslogStatus{Config: c, Active: []string{}}
	if s.syslog == nil {
		out.Error = "Хранилище syslog недоступно"
		return out
	}
	r := s.syslog
	r.mu.Lock()
	for _, name := range []string{"udp", "tcp"} {
		if _, ok := r.bindings[name]; ok {
			out.Active = append(out.Active, name)
		}
	}
	out.Error = r.listenError
	r.mu.Unlock()
	if r.writeError.Load() {
		out.Error = "Ошибка записи журнала"
	}
	out.Drops = r.pendingDrops.Load()
	return out
}

func (s *server) putSyslogSettings(w http.ResponseWriter, request *http.Request) {
	var c SyslogConfig
	if decodeJSON(request, &c) != nil || net.ParseIP(c.Bind) == nil || c.Port == 0 ||
		c.Limit < 1000 || c.Limit > 50000 || c.Enabled && !c.UDP && !c.TCP {
		problem(w, http.StatusBadRequest, "Проверьте IP, порт, протоколы и глубину журнала (1 000–50 000)")
		return
	}
	c.Bind = net.ParseIP(c.Bind).String()
	if s.syslog == nil {
		problem(w, http.StatusServiceUnavailable, "Хранилище Syslog недоступно")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.state.Syslog
	err := s.syslog.configure(c, func() error {
		s.state.Syslog = c
		if err := s.saveLocked(); err != nil {
			s.state.Syslog = previous
			log.Printf("save syslog settings: %v", err)
			return errors.New("не удалось сохранить настройки")
		}
		return nil
	})
	if err != nil {
		problem(w, http.StatusConflict, err.Error())
		return
	}
	sendJSON(w, http.StatusOK, s.syslogState(c))
}

func (s *server) syslogMessages(w http.ResponseWriter, request *http.Request) {
	if s.syslog == nil {
		problem(w, http.StatusServiceUnavailable, "Хранилище Syslog недоступно")
		return
	}
	q := request.URL.Query()
	before, _ := strconv.ParseUint(q.Get("before"), 10, 64)
	severity, err := strconv.Atoi(q.Get("severity"))
	if err != nil {
		severity = -2
	}
	source, text := strings.ToLower(q.Get("source")), strings.ToLower(q.Get("q"))
	out := struct {
		Items []SyslogEntry `json:"items"`
		Next  uint64        `json:"next,omitempty"`
	}{Items: []SyslogEntry{}}
	err = s.syslog.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(syslogBucket).Cursor()
		key, value := cursor.Last()
		if before > 0 {
			var bound [8]byte
			binary.BigEndian.PutUint64(bound[:], before)
			if key, value = cursor.Seek(bound[:]); key != nil {
				key, value = cursor.Prev()
			} else {
				key, value = cursor.Last()
			}
		}
		for ; key != nil; key, value = cursor.Prev() {
			if request.Context().Err() != nil {
				return request.Context().Err()
			}
			var entry SyslogEntry
			if json.Unmarshal(value, &entry) != nil || (severity != -2 && entry.Severity != severity) ||
				!strings.Contains(strings.ToLower(entry.Source+" "+entry.Host), source) ||
				!strings.Contains(strings.ToLower(entry.App+" "+entry.Message), text) {
				continue
			}
			if len(out.Items) == 100 {
				out.Next = out.Items[len(out.Items)-1].ID
				break
			}
			out.Items = append(out.Items, entry)
		}
		return nil
	})
	if err != nil {
		problem(w, http.StatusInternalServerError, "Не удалось прочитать журнал")
		return
	}
	sendJSON(w, http.StatusOK, out)
}
