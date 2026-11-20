package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

type smtpDestination struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"`
}

func (d smtpDestination) validate() error {
	if d.Host == "" || len(d.Host) > 253 || strings.ContainsAny(d.Host, "/\\ \r\n\t") {
		return errors.New("Укажите корректный SMTP-сервер")
	}
	if d.Port < 1 || d.Port > 65535 {
		return errors.New("Порт должен быть от 1 до 65535")
	}
	switch d.Security {
	case "tls", "starttls", "none":
		return nil
	default:
		return errors.New("Неизвестный режим TLS")
	}
}

func normalizedCertSHA256(raw string) (string, error) {
	pin := strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(strings.TrimSpace(raw)))
	if pin == "" {
		if strings.TrimSpace(raw) == "" {
			return "", nil
		}
		return "", errors.New("Отпечаток сертификата должен содержать 64 шестнадцатеричных символа")
	}
	if decoded, err := hex.DecodeString(pin); err != nil || len(decoded) != sha256.Size {
		return "", errors.New("Отпечаток сертификата должен содержать 64 шестнадцатеричных символа")
	}
	return pin, nil
}

func certSHA256(leaf *x509.Certificate) string {
	sum := sha256.Sum256(leaf.Raw)
	return hex.EncodeToString(sum[:])
}

func verifySMTPPeer(peer []*x509.Certificate, host string, roots *x509.CertPool) error {
	if len(peer) == 0 {
		return errors.New("SMTP-сервер не предъявил сертификат")
	}
	intermediates := x509.NewCertPool()
	for _, certificate := range peer[1:] {
		intermediates.AddCert(certificate)
	}
	_, err := peer[0].Verify(x509.VerifyOptions{DNSName: host, Roots: roots, Intermediates: intermediates})
	return err
}

func verifySMTPPin(peer []*x509.Certificate, host, pin string) error {
	if len(peer) == 0 {
		return errors.New("SMTP-сервер не предъявил сертификат")
	}
	if certSHA256(peer[0]) != pin {
		return errors.New("Сертификат SMTP-сервера изменился. Получите новый отпечаток и сверьте его")
	}
	roots := x509.NewCertPool()
	roots.AddCert(peer[0])
	return verifySMTPPeer(peer, host, roots)
}

func smtpTLSConfig(host, caFile, pin string) (*tls.Config, error) {
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if caFile != "" {
		ca, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read CA bundle: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(ca) {
			return nil, errors.New("CA bundle contains no valid PEM certificates")
		}
		cfg.RootCAs = pool
	}
	if pin != "" {
		// Use the administrator's exact fingerprint as the trust anchor,
		// then verify hostname, validity and server usage before SMTP AUTH.
		cfg.InsecureSkipVerify = true
		cfg.VerifyConnection = func(state tls.ConnectionState) error {
			return verifySMTPPin(state.PeerCertificates, host, pin)
		}
	}
	return cfg, nil
}

func connectSMTP(ctx context.Context, d smtpDestination, tlsCfg *tls.Config) (*smtp.Client, error) {
	addr := net.JoinHostPort(d.Host, strconv.Itoa(d.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	connected := false
	defer func() {
		if !connected {
			raw.Close()
		}
	}()
	deadline := time.Now().Add(25 * time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	if err := raw.SetDeadline(deadline); err != nil {
		return nil, err
	}
	var conn net.Conn = raw
	if d.Security == "tls" {
		secured := tls.Client(raw, tlsCfg)
		if err := secured.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		conn = secured
	}
	client, err := smtp.NewClient(conn, d.Host)
	if err != nil {
		return nil, err
	}
	if d.Security == "starttls" {
		supported, _ := client.Extension("STARTTLS")
		if !supported {
			return nil, errors.New("SMTP-сервер не объявляет поддержку STARTTLS")
		}
		if err := client.StartTLS(tlsCfg); err != nil {
			return nil, err
		}
	}
	connected = true
	return client, nil
}

func (s *server) inspectSMTPCertificate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		smtpDestination
		CAFile string `json:"caFile"`
	}
	if err := decodeJSON(r, &in); err != nil {
		problem(w, 400, "Укажите сервер, порт и режим TLS")
		return
	}
	if err := in.smtpDestination.validate(); err != nil {
		problem(w, 400, err.Error())
		return
	}
	if in.Security == "none" {
		problem(w, 400, "Выберите STARTTLS или SSL/TLS")
		return
	}
	if len(in.CAFile) > 1024 {
		problem(w, 400, "Слишком длинный путь к сертификату")
		return
	}
	cfg, err := smtpTLSConfig(in.Host, in.CAFile, "")
	if err != nil {
		problem(w, 400, safeErr(err))
		return
	}
	// Inspection never sends AUTH, envelopes or messages. This handshake
	// reads the public certificate regardless of the current trust store.
	cfg.InsecureSkipVerify = true
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	client, err := connectSMTP(ctx, in.smtpDestination, cfg)
	if err != nil {
		problem(w, 502, "Не удалось получить сертификат: "+safeErr(err))
		return
	}
	defer client.Close()
	state, ok := client.TLSConnectionState()
	if !ok || len(state.PeerCertificates) == 0 {
		problem(w, 502, "SMTP-сервер не предъявил сертификат")
		return
	}
	leaf := state.PeerCertificates[0]
	pin := certSHA256(leaf)
	verified := verifySMTPPeer(state.PeerCertificates, in.Host, cfg.RootCAs)
	pinErr := verifySMTPPin(state.PeerCertificates, in.Host, pin)
	result := map[string]any{
		"sha256": pin, "subject": leaf.Subject.String(), "issuer": leaf.Issuer.String(),
		"notBefore": leaf.NotBefore, "notAfter": leaf.NotAfter, "trusted": verified == nil,
		"canPin": pinErr == nil,
	}
	if pinErr != nil {
		result["problem"] = safeErr(pinErr)
	}
	sendJSON(w, 200, result)
}
