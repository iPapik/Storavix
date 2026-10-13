package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFirstRunAndPersistence(t *testing.T) {
	t.Setenv("STORAVIX_ADMIN_PASSWORD", "this-is-a-long-test-password")
	dir := t.TempDir()
	s, e := newServer(dir)
	if e != nil {
		t.Fatal(e)
	}
	if s.state.AdminUser != "admin" || s.state.PassHash == "this-is-a-long-test-password" {
		t.Fatal("administrator password must be hashed")
	}
	data, e := os.ReadFile(filepath.Join(dir, "state.json"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(data), "this-is-a-long-test-password") {
		t.Fatal("plaintext password on disk")
	}
	another, e := newServer(dir)
	if e != nil {
		t.Fatal(e)
	}
	if another.state.PassHash != s.state.PassHash {
		t.Fatal("password hash changed after restart")
	}
	secret, e := s.encrypt("mail-secret")
	if e != nil {
		t.Fatal(e)
	}
	recovered, e := another.decrypt(secret)
	if e != nil || recovered != "mail-secret" {
		t.Fatal("cannot decrypt persistent secret", e)
	}
}
func TestAPIAuthenticationAndEmailConfig(t *testing.T) {
	t.Setenv("STORAVIX_ADMIN_PASSWORD", "this-is-a-long-test-password")
	s, e := newServer(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	res, e := client.Get(ts.URL + "/api/summary")
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("unauthorized request status %d", res.StatusCode)
	}
	send := func(path, method, body string, cookie *http.Cookie) *http.Response {
		t.Helper()
		r, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Storavix-Request", "1")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		ans, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		return ans
	}
	res = send("/api/login", "POST", `{"username":"admin","password":"this-is-a-long-test-password"}`, nil)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("login HTTP %d", res.StatusCode)
	}
	cookies := res.Cookies()
	if len(cookies) == 0 || !cookies[0].HttpOnly {
		t.Fatal("session cookie missing or not HttpOnly")
	}
	mail := `{"host":"localhost","port":25,"security":"none","auth":"none","username":"","from":"alerts@example.com","recipients":"ops@example.com","caFile":"","password":"","hasSecret":false}`
	res = send("/api/settings/mail", "PUT", mail, cookies[0])
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("mail config save HTTP %d", res.StatusCode)
	}
	res = send("/api/settings/mail", "GET", "", cookies[0])
	defer res.Body.Close()
	var cfg MailConfig
	if e = json.NewDecoder(res.Body).Decode(&cfg); e != nil {
		t.Fatal(e)
	}
	if cfg.Host != "localhost" || cfg.Secret != "" {
		t.Fatal("bad redaction or persistence")
	}
	res = send("/api/settings/mail", "PUT", strings.Replace(mail, `"security":"none","auth":"none"`, `"security":"none","auth":"plain"`, 1), cookies[0])
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatal("must reject unencrypted password login")
	}
}
func TestSendMailLocalRelay(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	done := make(chan string, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			done <- "accept: " + e.Error()
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		respond := func(code string) { fmt.Fprint(w, code+"\r\n"); w.Flush() }
		respond("220 local test")
		inData := false
		message := ""
		for {
			line, e := r.ReadString('\n')
			if e != nil {
				done <- "read: " + e.Error()
				return
			}
			trimmed := strings.TrimRight(line, "\r\n")
			if inData {
				if trimmed == "." {
					inData = false
					respond("250 queued")
					continue
				}
				message += line
				continue
			}
			switch {
			case strings.HasPrefix(trimmed, "EHLO"), strings.HasPrefix(trimmed, "HELO"):
				respond("250-local\r\n250 SIZE 123456")
			case strings.HasPrefix(trimmed, "MAIL FROM:"):
				respond("250 ok")
			case strings.HasPrefix(trimmed, "RCPT TO:"):
				respond("250 ok")
			case trimmed == "DATA":
				inData = true
				respond("354 continue")
			case trimmed == "QUIT":
				respond("221 goodbye")
				done <- message
				return
			default:
				respond("500 unsupported " + trimmed)
			}
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	cfg := MailConfig{Host: "127.0.0.1", Port: port, Security: "none", Auth: "none", From: "alerts@example.com", Recipients: "ops@example.com"}
	if e = sendMail(cfg, "", "Test Storavix", "Some data"); e != nil {
		t.Fatal(e)
	}
	select {
	case body := <-done:
		if !strings.Contains(body, "Some data") || !strings.Contains(body, "Subject: Test Storavix") {
			t.Fatal("wrong mail contents", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("smtp server never received message")
	}
}
