package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func startSMTPFixture(t *testing.T, security string) (MailConfig, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "SMTP fixture"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				if security == "tls" {
					conn = tls.Server(conn, cfg)
				}
				fixtureSMTP(conn, cfg, security == "starttls")
			}()
		}
	}()
	return MailConfig{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Security: security,
		Auth: "none", From: "test@example.org", Recipients: "dest@example.org"}, leaf
}

func fixtureSMTP(conn net.Conn, cfg *tls.Config, advertise bool) {
	input := bufio.NewReader(conn)
	if _, err := fmt.Fprint(conn, "220 fixture SMTP\r\n"); err != nil {
		return
	}
	for {
		line, err := input.ReadString('\n')
		if err != nil {
			return
		}
		command, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		var reply string
		switch command {
		case "EHLO":
			reply = "250 fixture\r\n"
			if advertise {
				reply = "250-fixture\r\n250 STARTTLS\r\n"
			}
		case "STARTTLS":
			if _, err := fmt.Fprint(conn, "220 ready\r\n"); err != nil {
				return
			}
			conn = tls.Server(conn, cfg)
			input = bufio.NewReader(conn)
			advertise = false
			continue
		case "MAIL", "RCPT":
			reply = "250 accepted\r\n"
		case "DATA":
			if _, err := fmt.Fprint(conn, "354 body\r\n"); err != nil {
				return
			}
			for {
				line, err := input.ReadString('\n')
				if err != nil {
					return
				}
				if line == ".\r\n" {
					break
				}
			}
			reply = "250 queued\r\n"
		case "QUIT":
			fmt.Fprint(conn, "221 bye\r\n")
			return
		default:
			reply = "500 unexpected command\r\n"
		}
		if _, err := fmt.Fprint(conn, reply); err != nil {
			return
		}
	}
}

func TestSMTPCertificatePreviewAndPinnedSend(t *testing.T) {
	for _, mode := range []string{"starttls", "tls"} {
		t.Run(mode, func(t *testing.T) {
			cfg, leaf := startSMTPFixture(t, mode)
			body := fmt.Sprintf(`{"host":"127.0.0.1","port":%d,"security":"%s"}`, cfg.Port, mode)
			r := httptest.NewRequest("POST", "/api/settings/mail/certificate", strings.NewReader(body))
			w := httptest.NewRecorder()
			(&server{}).inspectSMTPCertificate(w, r)
			var got struct {
				SHA256          string
				Trusted, CanPin bool
			}
			if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil ||
				got.SHA256 != certSHA256(leaf) || got.Trusted || !got.CanPin {
				t.Fatalf("certificate preview = %d %s", w.Code, w.Body.String())
			}
			if err := sendMail(cfg, "", "test", "test"); err == nil || !strings.Contains(err.Error(), "unknown authority") {
				t.Fatalf("untrusted SMTP accepted: %v", err)
			}
			cfg.CertSHA256 = got.SHA256
			if err := sendMail(cfg, "", "test", "test"); err != nil {
				t.Fatalf("confirmed certificate did not allow SMTP: %v", err)
			}
			cfg.CertSHA256 = strings.Repeat("0", 64)
			if err := sendMail(cfg, "", "test", "test"); err == nil || !strings.Contains(err.Error(), "сертификат") && !strings.Contains(err.Error(), "Сертификат") {
				t.Fatalf("wrong certificate accepted: %v", err)
			}
			if err := verifySMTPPin([]*x509.Certificate{leaf}, "wrong.example", got.SHA256); err == nil {
				t.Fatal("pin bypassed hostname verification")
			}
		})
	}
}

func TestSMTPTestUsesDraftWithoutSaving(t *testing.T) {
	cfg, leaf := startSMTPFixture(t, "starttls")
	cfg.CertSHA256 = certSHA256(leaf)
	draft, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{}
	w := httptest.NewRecorder()
	s.testMail(w, httptest.NewRequest("POST", "/api/settings/mail/test", strings.NewReader(string(draft))))
	if w.Code != http.StatusOK || s.state.Mail.Host != "" || s.state.Mail.CertSHA256 != "" {
		t.Fatalf("SMTP draft test = %d %s, persisted %+v", w.Code, w.Body.String(), s.state.Mail)
	}
}
