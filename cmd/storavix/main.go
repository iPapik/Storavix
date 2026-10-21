package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iPapik/Storavix/internal/telemetry"
	"github.com/iPapik/Storavix/internal/web"
)

const version = "0.2.1"

type MailConfig struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Security   string `json:"security"` // tls, starttls, none
	Auth       string `json:"auth"`     // none, plain, login, crammd5, xoauth2
	Username   string `json:"username"`
	From       string `json:"from"`
	Recipients string `json:"recipients"`
	CAFile     string `json:"caFile"`
	Secret     string `json:"-"`
	HasSecret  bool   `json:"hasSecret"`
}
type Alert struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Severity  string    `json:"severity"`
	Subject   string    `json:"subject"`
	Detail    string    `json:"detail"`
	Delivered bool      `json:"delivered"`
}
type State struct {
	AdminUser       string     `json:"adminUser"`
	Salt            string     `json:"salt"`
	PassHash        string     `json:"passHash"`
	Mail            MailConfig `json:"mail"`
	EncryptedSecret string     `json:"encryptedSecret"`
	Alerts          []Alert    `json:"alerts"`
}
type server struct {
	mu            sync.Mutex
	state         State
	dataDir       string
	key           []byte
	sessions      map[string]time.Time
	loginAttempts map[string][]time.Time
	mailLast      time.Time
	secureCookies bool
	agents        map[string]telemetry.Report
}

func main() {
	done, dir, addr, err := executeCLI(os.Args[1:], os.Stdin, os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	if done {
		return
	}
	s, err := newServer(dir)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Storavix %s listening on %s", version, addr)
	for _, u := range accessURLs(addr) {
		log.Printf("Open Storavix in browser: %s", u)
	}
	if !isLoopbackBind(addr) {
		log.Print("SECURITY: LAN access uses HTTP; restrict firewall access to trusted subnets and deploy HTTPS reverse proxy before untrusted networks")
	}
	h := s.routes()
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 90 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func env(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}
func isLoopbackBind(addr string) bool {
	h, _, e := net.SplitHostPort(addr)
	return e == nil && (h == "localhost" || net.ParseIP(h) != nil && net.ParseIP(h).IsLoopback())
}
func randomBytes(n int) []byte {
	v := make([]byte, n)
	if _, e := rand.Read(v); e != nil {
		panic(e)
	}
	return v
}
func pbkdf2(password string, salt []byte) string { // PBKDF2-HMAC-SHA256, 240000 iterations, 32 bytes
	mac := hmac.New(sha256.New, []byte(password))
	mac.Write(salt)
	mac.Write([]byte{0, 0, 0, 1})
	u := mac.Sum(nil)
	t := append([]byte(nil), u...)
	for i := 1; i < 240000; i++ {
		mac = hmac.New(sha256.New, []byte(password))
		mac.Write(u)
		u = mac.Sum(nil)
		for j := range t {
			t[j] ^= u[j]
		}
	}
	return hex.EncodeToString(t)
}
func newServer(dir string) (*server, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &server{dataDir: dir, sessions: map[string]time.Time{}, loginAttempts: map[string][]time.Time{}, secureCookies: os.Getenv("STORAVIX_SECURE_COOKIES") == "true", agents: map[string]telemetry.Report{}}
	keyPath := filepath.Join(dir, "master.key")
	if keyEnv := os.Getenv("STORAVIX_MASTER_KEY"); keyEnv != "" {
		key, e := base64.StdEncoding.DecodeString(keyEnv)
		if e != nil || len(key) != 32 {
			return nil, errors.New("STORAVIX_MASTER_KEY must be base64 of 32 random bytes")
		}
		s.key = key
	} else if key, e := os.ReadFile(keyPath); e == nil {
		if len(key) != 32 {
			return nil, errors.New("master.key must contain 32 bytes")
		}
		s.key = key
	} else if errors.Is(e, os.ErrNotExist) {
		s.key = randomBytes(32)
		if e = os.WriteFile(keyPath, s.key, 0600); e != nil {
			return nil, e
		}
	} else {
		return nil, e
	}
	file := filepath.Join(dir, "state.json")
	if b, e := os.ReadFile(file); e == nil {
		if e = json.Unmarshal(b, &s.state); e != nil {
			return nil, fmt.Errorf("reading state: %w", e)
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	if err := s.loadAgents(); err != nil {
		return nil, err
	}
	if s.state.PassHash == "" {
		username := env("STORAVIX_ADMIN_USER", "admin")
		pass := os.Getenv("STORAVIX_ADMIN_PASSWORD")
		generated := false
		if pass == "" {
			pass = base64.RawURLEncoding.EncodeToString(randomBytes(18))
			generated = true
		}
		if len(pass) < 8 {
			return nil, errors.New("STORAVIX_ADMIN_PASSWORD must contain at least 8 characters")
		}
		salt := randomBytes(16)
		s.state.AdminUser = username
		s.state.Salt = hex.EncodeToString(salt)
		s.state.PassHash = pbkdf2(pass, salt)
		s.state.Mail.Port = 587
		s.state.Mail.Security = "starttls"
		s.state.Mail.Auth = "plain"
		if e := s.saveLocked(); e != nil {
			return nil, e
		}
		if generated {
			log.Printf("FIRST START — username: %s  temporary password: %s (save now; shown only once)", username, pass)
		} else {
			log.Printf("FIRST START — administrator account %q created from environment", username)
		}
	}
	return s, nil
}
func (s *server) saveLocked() error {
	b, e := json.MarshalIndent(s.state, "", "  ")
	if e != nil {
		return e
	}
	tmp := filepath.Join(s.dataDir, "state.tmp")
	if e = os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, filepath.Join(s.dataDir, "state.json"))
}
func (s *server) encrypt(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	b, e := aes.NewCipher(s.key)
	if e != nil {
		return "", e
	}
	a, e := cipher.NewGCM(b)
	if e != nil {
		return "", e
	}
	nonce := randomBytes(a.NonceSize())
	return base64.StdEncoding.EncodeToString(a.Seal(nonce, nonce, []byte(v), nil)), nil
}
func (s *server) decrypt(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	raw, e := base64.StdEncoding.DecodeString(v)
	if e != nil {
		return "", e
	}
	b, e := aes.NewCipher(s.key)
	if e != nil {
		return "", e
	}
	a, e := cipher.NewGCM(b)
	if e != nil {
		return "", e
	}
	if len(raw) < a.NonceSize() {
		return "", errors.New("invalid encrypted secret")
	}
	p, e := a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], nil)
	return string(p), e
}
func sendJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, status int, msg string) {
	sendJSON(w, status, map[string]string{"error": msg})
}
func decodeJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 32768)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}
func (s *server) auth(r *http.Request) bool {
	c, e := r.Cookie("storavix_session")
	if e != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[c.Value]
	return ok && time.Now().Before(exp)
}
func (s *server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth(r) {
			problem(w, 401, "Требуется вход администратора")
			return
		}
		h(w, r)
	}
}
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", web.Handler())
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		sendJSON(w, 200, map[string]any{"status": "ok", "version": version})
	})
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		sendJSON(w, 200, map[string]any{"authenticated": s.auth(r), "version": version})
	})
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.requireAuth(s.logout))
	mux.HandleFunc("GET /api/summary", s.requireAuth(s.summary))
	mux.HandleFunc("GET /api/settings/mail", s.requireAuth(s.getMail))
	mux.HandleFunc("PUT /api/settings/mail", s.requireAuth(s.setMail))
	mux.HandleFunc("POST /api/settings/mail/test", s.requireAuth(s.testMail))
	mux.HandleFunc("GET /api/alerts", s.requireAuth(s.alerts))
	mux.HandleFunc("POST /api/alerts/test", s.requireAuth(s.testAlert))
	mux.HandleFunc("POST /api/account/password", s.requireAuth(s.changePassword))
	mux.HandleFunc("GET /api/agents", s.requireAuth(s.getAgents))
	mux.HandleFunc("POST /api/agent/report", s.ingestAgent)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'")
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := http.NewRequest("GET", origin, nil)
				if err != nil || u.URL.Host != r.Host {
					problem(w, 403, "Origin not allowed")
					return
				}
			}
			if r.URL.Path != "/api/login" && strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("X-Storavix-Request") != "1" {
				problem(w, 403, "Missing request header")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decodeJSON(r, &in) != nil {
		problem(w, 400, "Неверный запрос")
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	s.mu.Lock()
	now := time.Now()
	old := s.loginAttempts[ip]
	filtered := old[:0]
	for _, t := range old {
		if now.Sub(t) < time.Minute {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) >= 8 {
		s.mu.Unlock()
		problem(w, 429, "Слишком много попыток, повторите через минуту")
		return
	}
	s.loginAttempts[ip] = append(filtered, now)
	user := s.state.AdminUser
	saltHex := s.state.Salt
	correct := s.state.PassHash
	s.mu.Unlock()
	salt, _ := hex.DecodeString(saltHex)
	hash := pbkdf2(in.Password, salt)
	if subtle.ConstantTimeCompare([]byte(hash), []byte(correct)) != 1 || subtle.ConstantTimeCompare([]byte(in.Username), []byte(user)) != 1 {
		problem(w, 401, "Неверный логин или пароль")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes(32))
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(12 * time.Hour)
	s.loginAttempts[ip] = nil
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "storavix_session", Value: token, Path: "/", HttpOnly: true, Secure: s.secureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	sendJSON(w, 200, map[string]string{"status": "ok"})
}
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie("storavix_session")
	s.mu.Lock()
	if c != nil {
		delete(s.sessions, c.Value)
	}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "storavix_session", Value: "", Path: "/", HttpOnly: true, Secure: s.secureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	sendJSON(w, 200, map[string]string{"status": "ok"})
}
func (s *server) summary(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	total := len(s.state.Alerts)
	mailReady := s.state.Mail.Host != "" && s.state.Mail.From != "" && s.state.Mail.Recipients != ""
	s.mu.Unlock()
	sendJSON(w, 200, map[string]any{"hosts": len(s.agents), "arrays": s.arrayCount(), "drives": 0, "network": 0, "events": total, "mailConfigured": mailReady, "collectorsActive": s.onlineCount(), "mode": "live", "version": version})
}
func (s *server) getMail(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.state.Mail
	cfg.HasSecret = s.state.EncryptedSecret != ""
	s.mu.Unlock()
	sendJSON(w, 200, cfg)
}
func validateMail(c MailConfig) error {
	if c.Host == "" || strings.ContainsAny(c.Host, "/\\ \r\n\t") {
		return errors.New("Укажите корректный SMTP-сервер")
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("Порт должен быть от 1 до 65535")
	}
	if c.Security != "tls" && c.Security != "starttls" && c.Security != "none" {
		return errors.New("Неизвестный режим TLS")
	}
	if c.Auth != "none" && c.Auth != "plain" && c.Auth != "login" && c.Auth != "crammd5" && c.Auth != "xoauth2" {
		return errors.New("Неизвестный метод авторизации")
	}
	if c.Security == "none" && c.Auth != "none" {
		return errors.New("Авторизация по незашифрованному SMTP запрещена")
	}
	if _, e := mail.ParseAddress(c.From); e != nil {
		return errors.New("Неверный адрес отправителя")
	}
	if _, e := parseRecipients(c.Recipients); e != nil {
		return e
	}
	if c.Auth != "none" && strings.TrimSpace(c.Username) == "" {
		return errors.New("Укажите SMTP-логин")
	}
	if len(c.CAFile) > 1024 {
		return errors.New("Слишком длинный путь к сертификату")
	}
	return nil
}
func parseRecipients(v string) ([]string, error) {
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	if len(parts) == 0 || len(parts) > 30 {
		return nil, errors.New("Укажите от 1 до 30 получателей")
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		a, e := mail.ParseAddress(p)
		if e != nil || a.Address == "" {
			return nil, errors.New("Некорректный адрес получателя: " + p)
		}
		out = append(out, a.Address)
	}
	return out, nil
}
func (s *server) setMail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MailConfig
		Password string `json:"password"`
	}
	if e := decodeJSON(r, &in); e != nil {
		problem(w, 400, "Неверные поля: "+e.Error())
		return
	}
	if e := validateMail(in.MailConfig); e != nil {
		problem(w, 400, e.Error())
		return
	}
	if len(in.Password) > 4096 {
		problem(w, 400, "Слишком длинный секрет")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	enc := s.state.EncryptedSecret
	if in.Password != "" {
		var e error
		enc, e = s.encrypt(in.Password)
		if e != nil {
			problem(w, 500, "Не удалось зашифровать секрет")
			return
		}
	}
	if in.Auth != "none" && enc == "" {
		problem(w, 400, "Для авторизации нужен пароль или OAuth2-токен")
		return
	}
	in.MailConfig.HasSecret = enc != ""
	s.state.Mail = in.MailConfig
	s.state.EncryptedSecret = enc
	if e := s.saveLocked(); e != nil {
		problem(w, 500, "Не удалось сохранить настройки")
		return
	}
	sendJSON(w, 200, map[string]string{"status": "saved"})
}
func (s *server) mailSnapshot() (MailConfig, string, error) {
	s.mu.Lock()
	c := s.state.Mail
	enc := s.state.EncryptedSecret
	s.mu.Unlock()
	secret, e := s.decrypt(enc)
	return c, secret, e
}
func (s *server) checkMailRate() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.mailLast) < 15*time.Second {
		return false
	}
	s.mailLast = time.Now()
	return true
}
func (s *server) testMail(w http.ResponseWriter, r *http.Request) {
	if !s.checkMailRate() {
		problem(w, 429, "Повторная отправка возможна через 15 секунд")
		return
	}
	cfg, secret, e := s.mailSnapshot()
	if e == nil {
		e = sendMail(cfg, secret, "Storavix — проверка SMTP", "Проверка настроек почтовых уведомлений Storavix выполнена успешно.")
	}
	if e != nil {
		problem(w, 502, "Ошибка SMTP: "+safeErr(e))
		return
	}
	sendJSON(w, 200, map[string]string{"status": "sent"})
}
func (s *server) alerts(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	a := append([]Alert(nil), s.state.Alerts...)
	s.mu.Unlock()
	if a == nil {
		a = []Alert{}
	}
	sendJSON(w, 200, a)
}
func (s *server) testAlert(w http.ResponseWriter, r *http.Request) {
	if !s.checkMailRate() {
		problem(w, 429, "Повторная отправка возможна через 15 секунд")
		return
	}
	cfg, secret, e := s.mailSnapshot()
	if e == nil {
		e = sendMail(cfg, secret, "[TEST] Storavix — тестовое оповещение", "Это тестовое оповещение Storavix. Реальных неисправностей не обнаружено: мониторинг оборудования ещё не подключён.")
	}
	alert := Alert{ID: hex.EncodeToString(randomBytes(8)), CreatedAt: time.Now().UTC(), Severity: "info", Subject: "Тестовое оповещение", Detail: "Проверка маршрута SMTP", Delivered: e == nil}
	s.mu.Lock()
	s.state.Alerts = append([]Alert{alert}, s.state.Alerts...)
	if len(s.state.Alerts) > 200 {
		s.state.Alerts = s.state.Alerts[:200]
	}
	saveErr := s.saveLocked()
	s.mu.Unlock()
	if saveErr != nil {
		problem(w, 500, "Не удалось сохранить событие")
		return
	}
	if e != nil {
		problem(w, 502, "Оповещение сохранено, отправка не удалась: "+safeErr(e))
		return
	}
	sendJSON(w, 200, map[string]string{"status": "sent"})
}
func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if decodeJSON(r, &in) != nil || len(in.New) < 8 || len(in.New) > 1024 {
		problem(w, 400, "Новый пароль должен содержать 8–1024 символа")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	salt, _ := hex.DecodeString(s.state.Salt)
	if subtle.ConstantTimeCompare([]byte(pbkdf2(in.Old, salt)), []byte(s.state.PassHash)) != 1 {
		problem(w, 403, "Текущий пароль неверен")
		return
	}
	salt = randomBytes(16)
	s.state.Salt = hex.EncodeToString(salt)
	s.state.PassHash = pbkdf2(in.New, salt)
	if e := s.saveLocked(); e != nil {
		problem(w, 500, "Ошибка сохранения")
		return
	}
	s.sessions = map[string]time.Time{}
	http.SetCookie(w, &http.Cookie{Name: "storavix_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode})
	sendJSON(w, 200, map[string]string{"status": "changed"})
}
func safeErr(e error) string {
	msg := e.Error()
	if len(msg) > 350 {
		msg = msg[:350]
	}
	return msg
}

type loginAuth struct {
	username, secret string
	step             int
}

func (a *loginAuth) Start(*smtp.ServerInfo) (string, []byte, error) { return "LOGIN", nil, nil }
func (a *loginAuth) Next(_ []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	a.step++
	if a.step == 1 {
		return []byte(a.username), nil
	}
	if a.step == 2 {
		return []byte(a.secret), nil
	}
	return nil, errors.New("unexpected LOGIN authentication challenge")
}

type oauthAuth struct{ user, token string }

func (a oauthAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + a.user + "\x01auth=Bearer " + a.token + "\x01\x01"), nil
}
func (a oauthAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return []byte{}, nil
	}
	return nil, nil
}
func sendMail(cfg MailConfig, secret, subject, body string) error {
	if e := validateMail(cfg); e != nil {
		return e
	}
	recips, e := parseRecipients(cfg.Recipients)
	if e != nil {
		return e
	}
	from, e := mail.ParseAddress(cfg.From)
	if e != nil {
		return e
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		ca, e := os.ReadFile(cfg.CAFile)
		if e != nil {
			return fmt.Errorf("read CA bundle: %w", e)
		}
		pool, e := x509.SystemCertPool()
		if e != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(ca) {
			return errors.New("CA bundle contains no valid PEM certificates")
		}
		tlsCfg.RootCAs = pool
	}
	var conn net.Conn
	if cfg.Security == "tls" {
		conn, e = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, e = dialer.Dial("tcp", addr)
	}
	if e != nil {
		return e
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(25 * time.Second))
	client, e := smtp.NewClient(conn, cfg.Host)
	if e != nil {
		return e
	}
	defer client.Close()
	if cfg.Security == "starttls" {
		supported, _ := client.Extension("STARTTLS")
		if !supported {
			return errors.New("server does not advertise STARTTLS")
		}
		if e = client.StartTLS(tlsCfg); e != nil {
			return e
		}
	}
	if cfg.Auth != "none" {
		if secret == "" {
			return errors.New("SMTP secret is empty")
		}
		var auth smtp.Auth
		switch cfg.Auth {
		case "plain":
			auth = smtp.PlainAuth("", cfg.Username, secret, cfg.Host)
		case "login":
			auth = &loginAuth{username: cfg.Username, secret: secret}
		case "crammd5":
			auth = smtp.CRAMMD5Auth(cfg.Username, secret)
		case "xoauth2":
			auth = oauthAuth{user: cfg.Username, token: secret}
		}
		if e = client.Auth(auth); e != nil {
			return e
		}
	}
	if e = client.Mail(from.Address); e != nil {
		return e
	}
	for _, to := range recips {
		if e = client.Rcpt(to); e != nil {
			return e
		}
	}
	writer, e := client.Data()
	if e != nil {
		return e
	}

	subject = strings.NewReplacer("\r", " ", "\n", " ").Replace(subject)
	boundary := "storavix-" + hex.EncodeToString(randomBytes(12))
	msg := "From: " + from.String() + "\r\nTo: " + strings.Join(recips, ", ") + "\r\nSubject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n--" + boundary + "\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" + body + "\r\n--" + boundary + "\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n<html><body><h2>Storavix</h2><p>" + html.EscapeString(body) + "</p></body></html>\r\n--" + boundary + "--\r\n"
	if _, e = io.WriteString(writer, msg); e != nil {
		return e
	}
	if e = writer.Close(); e != nil {
		return e
	}
	return client.Quit()
}
