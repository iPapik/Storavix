package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestSyslogFrameParsing(t *testing.T) {
	raw := `<165>1 2026-11-17T17:50:00Z switch-core sshd 12 credentials [meta@24 name="a\]b"][second name="z"] Invalid login`
	reader := bufio.NewReader(strings.NewReader(fmt.Sprintf("%d %s<189>Nov 17 17:05:00 switch-edge port: Link up\n", len(raw), raw)))
	frame, err := readSyslogFrame(reader)
	if err != nil {
		t.Fatal(err)
	}
	entry := parseSyslog(string(frame), time.Now())
	if entry.Severity != 5 || entry.Facility != 20 || entry.Host != "switch-core" || entry.App != "sshd" || entry.Message != "Invalid login" {
		t.Fatalf("unexpected RFC 5424 entry: %+v", entry)
	}
	frame, err = readSyslogFrame(reader)
	if err != nil {
		t.Fatal(err)
	}
	entry = parseSyslog(strings.TrimSpace(string(frame)), time.Now())
	if entry.Severity != 5 || entry.Facility != 23 || entry.Host != "switch-edge" || entry.App != "port" || entry.Message != "Link up" {
		t.Fatalf("unexpected RFC 3164 entry: %+v", entry)
	}
	if _, err = readSyslogFrame(bufio.NewReader(strings.NewReader("9900 <1> oversized"))); err == nil {
		t.Fatal("accepted oversize frame")
	}
}

func TestSyslogReceivesAndPersistsBothTransports(t *testing.T) {
	dir := t.TempDir()
	r, err := openSyslog(dir)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		r.close()
		t.Fatal(err)
	}
	conf := SyslogConfig{Enabled: true, Bind: "127.0.0.1", Port: uint16(listener.Addr().(*net.TCPAddr).Port),
		UDP: true, TCP: true, Limit: 1000}
	attempt := r.configure(conf, nil)
	_ = listener.Close()
	if attempt == nil || len(r.bindings) != 0 {
		r.close()
		t.Fatal("partial listeners active after failed configuration")
	}
	if err = r.configure(conf, nil); err != nil {
		r.close()
		t.Fatal(err)
	}
	for _, proto := range []string{"udp", "tcp"} {
		c, err := net.Dial(proto, conf.address())
		if err != nil {
			r.close()
			t.Fatal(err)
		}
		_, err = fmt.Fprintln(c, "<11>Nov 17 17:12:00 switch port: test "+proto)
		_ = c.Close()
		if err != nil {
			r.close()
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var count int
		_ = r.db.View(func(tx *bolt.Tx) error {
			count = tx.Bucket(syslogBucket).Stats().KeyN
			return nil
		})
		if count == 2 {
			break
		}
		if time.Now().After(deadline) {
			r.close()
			t.Fatalf("expected two messages, got %d", count)
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.close()
	r, err = openSyslog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	s := server{syslog: r}
	httpResponse := httptest.NewRecorder()
	s.syslogMessages(httpResponse, httptest.NewRequest("GET", "/api/syslog?source=127.0.0.1&severity=3&q=tcp", nil))
	var output struct{ Items []SyslogEntry }
	if err := json.Unmarshal(httpResponse.Body.Bytes(), &output); err != nil ||
		httpResponse.Code != 200 || len(output.Items) != 1 || output.Items[0].Transport != "tcp" {
		t.Fatalf("unexpected journal: %d %s", httpResponse.Code, httpResponse.Body.String())
	}
}
