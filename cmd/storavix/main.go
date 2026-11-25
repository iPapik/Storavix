package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
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
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/iPapik/Storavix/internal/telemetry"
	"github.com/iPapik/Storavix/internal/web"
)

const version = "0.2.24"

type MailConfig struct {
	Host            string   `json:"host"`
	Port            int      `json:"port"`
	Security        string   `json:"security"` // tls, starttls, none
	Auth            string   `json:"auth"`     // none, plain, login, crammd5, xoauth2
	Username        string   `json:"username"`
	From            string   `json:"from"`
	Recipients      string   `json:"recipients"`
	CAFile          string   `json:"caFile"`
	CertSHA256      string   `json:"certSha256,omitempty"`
	Secret          string   `json:"-"`
	HasSecret       bool     `json:"hasSecret"`
	Alerts          []string `json:"alerts,omitempty"`
	ServerTempLimit int      `json:"serverTempLimit,omitempty"`
	SwitchTempLimit int      `json:"switchTempLimit,omitempty"`
}
type Alert struct {
	ID            string    `json:"id"`
	CreatedAt     time.Time `json:"createdAt"`
	Severity      string    `json:"severity"`
	Subject       string    `json:"subject"`
	Detail        string    `json:"detail"`
	Delivered     bool      `json:"delivered"`
	Kind          string    `json:"kind,omitempty"`
	Delivery      string    `json:"delivery,omitempty"`
	DeliveryError string    `json:"deliveryError,omitempty"`
	TelegramDelivery string `json:"telegramDelivery,omitempty"`
	TelegramDeliveryError string `json:"telegramDeliveryError,omitempty"`
}
type State struct {
	Users           map[string]StoredAccount  `json:"users,omitempty"`
	AdminUser       string                    `json:"adminUser"`
	Salt            string                    `json:"salt"`
	PassHash        string                    `json:"passHash"`
	Mail            MailConfig                `json:"mail"`
	EncryptedSecret string                    `json:"encryptedSecret"`
	Telegram TelegramConfig `json:"telegram,omitempty"`
	EncryptedTelegramToken string `json:"encryptedTelegramToken,omitempty"`
	Monitor         map[string]monitorState   `json:"monitor,omitempty"`
	Alerts          []Alert                   `json:"alerts"`
	Dashboard       *DashboardConfig          `json:"dashboard,omitempty"`
	Network         []NetworkConfig           `json:"network,omitempty"`
	SNMPTraps       SNMPTrapConfig            `json:"snmpTraps,omitempty"`
	Syslog          SyslogConfig              `json:"syslog,omitempty"`
	Locations       map[string]DeviceLocation `json:"locations,omitempty"`
}
type server struct {
	mu             sync.Mutex
	state          State
	dataDir        string
	key            []byte
	sessions       map[string]userSession
	loginAttempts  map[string][]time.Time
	mailLast       time.Time
	telegramLast time.Time
	secureCookies  bool
	agents         map[string]telemetry.Report
	deletedAgents  map[string]bool
	networkSamples map[string]networkSample
	networkWake    chan struct{}
	syslog         *syslogReceiver
	snmp           *snmpJournal
	geocoder       geoSearch
}

func main() {
	done, dir, addr, err := executeCLI(os.Args[1:], os.Stdin, os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	if done {
		return
	}
	if err := serve(dir, addr); err != nil {
		log.Fatal(err)
	}
}

func serve(dir, addr string) error {
	s, err := newServer(dir)
	if err != nil {
		return err
	}
	log.Printf("Storavix %s listening on %s", version, addr)
	for _, u := range accessURLs(addr) {
		log.Printf("Open Storavix in browser: %s", u)
	}
	if !isLoopbackBind(addr) {
		log.Print("SECURITY: LAN access uses HTTP; restrict firewall access to trusted subnets and deploy HTTPS reverse proxy before untrusted networks")
	}
	receiver, err := openSyslog(dir)
	if err != nil {
		log.Printf("open syslog database: %v", err)
	} else {
		s.syslog = receiver
		defer receiver.close()
		if err := receiver.configure(s.state.Syslog.defaults(), nil); err != nil {
			receiver.listenError = err.Error()
			log.Printf("start syslog listeners: %v", err)
		}
	}
	journal, err := openSNMPJournal(dir)
	if err != nil {
		log.Printf("open SNMP journal: %v", err)
	} else {
		s.snmp = journal
		defer journal.close()
		if err := s.configureSNMPTraps(s.state.SNMPTraps.defaults(), nil); err != nil {
			journal.startError = err.Error()
			log.Printf("start SNMP notifications: %v", err)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	networkDone := make(chan struct{})
	go func() {
		defer close(networkDone)
		s.pollNetwork(ctx)
	}()
	var monitoring sync.WaitGroup
	monitoring.Add(3)
	go func() { defer monitoring.Done(); s.runOfflineMonitor(ctx) }()
	go func() { defer monitoring.Done(); s.runMonitorMail(ctx) }()
	go func() { defer monitoring.Done(); s.runMonitorTelegram(ctx) }()
	defer func() { stop(); <-networkDone; monitoring.Wait() }()
	h := s.routes()
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 90 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if srv.Shutdown(shutdown) != nil {
			_ = srv.Close()
		}
	}()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-shutdownDone
	return nil
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
	s := &server{dataDir: dir, sessions: map[string]userSession{}, loginAttempts: map[string][]time.Time{}, secureCookies: os.Getenv("STORAVIX_SECURE_COOKIES") == "true", agents: map[string]telemetry.Report{}, deletedAgents: map[string]bool{}, networkSamples: make(map[string]networkSample), networkWake: make(chan struct{}, 1)}
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
	return decodeJSONLimit(r, v, 32768)
}
func decodeJSONLimit(r *http.Request, v any, limit int64) error {
	r.Body = http.MaxBytesReader(nil, r.Body, limit)
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
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", web.Handler())
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		sendJSON(w, 200, map[string]any{"status": "ok", "version": version})
	})
	mux.HandleFunc("GET /api/session", s.getSession)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.requireAuth(s.logout))
	mux.HandleFunc("GET /api/summary", s.requireAuth(s.summary))
	mux.HandleFunc("GET /api/settings/mail/events", s.authorize(Account.admin, func(w http.ResponseWriter, _ *http.Request) { sendJSON(w, 200, alertRules) }))
	mux.HandleFunc("GET /api/settings/telegram", s.authorize(Account.admin, s.getTelegram))
	mux.HandleFunc("PUT /api/settings/telegram", s.authorize(Account.admin, s.setTelegram))
	mux.HandleFunc("POST /api/settings/telegram/test", s.authorize(Account.admin, s.testTelegram))
	mux.HandleFunc("POST /api/settings/telegram/chats", s.authorize(Account.admin, s.telegramChats))
	mux.HandleFunc("GET /api/settings/mail", s.authorize(Account.admin, s.getMail))
	mux.HandleFunc("PUT /api/settings/mail", s.authorize(Account.admin, s.setMail))
	mux.HandleFunc("GET /api/settings/syslog", s.authorize(Account.admin, s.getSyslogSettings))
	mux.HandleFunc("PUT /api/settings/syslog", s.authorize(Account.admin, s.putSyslogSettings))
	mux.HandleFunc("GET /api/syslog", s.authorize(Account.full, s.syslogMessages))
	mux.HandleFunc("PUT /api/locations/{kind}/{id}", s.authorize(Account.write, s.saveLocation))
	mux.HandleFunc("DELETE /api/locations/{kind}/{id}", s.authorize(Account.write, s.saveLocation))
	mux.HandleFunc("POST /api/locations/search", s.authorize(Account.write, s.findLocation))

	mux.HandleFunc("POST /api/settings/mail/certificate", s.authorize(Account.admin, s.inspectSMTPCertificate))
	mux.HandleFunc("POST /api/settings/mail/test", s.authorize(Account.admin, s.testMail))
	mux.HandleFunc("GET /api/alerts", s.authorize(Account.full, s.alerts))
	mux.HandleFunc("POST /api/alerts/test", s.authorize(Account.admin, s.testAlert))
	mux.HandleFunc("POST /api/account/password", s.requireAuth(s.changePassword))
	mux.HandleFunc("GET /api/settings/users", s.authorize(Account.admin, s.getAccounts))
	mux.HandleFunc("POST /api/settings/users", s.authorize(Account.admin, s.sendAccountChange))
	mux.HandleFunc("PUT /api/settings/users/{username}", s.authorize(Account.admin, s.sendAccountChange))
	mux.HandleFunc("DELETE /api/settings/users/{username}", s.authorize(Account.admin, s.sendAccountChange))

	mux.HandleFunc("GET /api/agents", s.authorize(Account.servers, s.getAgents))
	mux.HandleFunc("GET /api/agents/{id}", s.authorize(Account.servers, s.getAgent))
	mux.HandleFunc("GET /api/dashboard", s.requireAuth(s.getDashboardData))
	mux.HandleFunc("GET /api/dashboard/config", s.requireAuth(s.getDashboardConfig))
	mux.HandleFunc("PUT /api/dashboard/config", s.authorize(func(a Account) bool { return a.full() && a.write() }, s.putDashboardConfig))
	mux.HandleFunc("GET /api/network", s.authorize(Account.network, s.getNetwork))
	mux.HandleFunc("GET /api/network/events", s.authorize(Account.network, s.getSNMPEvents))
	mux.HandleFunc("GET /api/settings/snmp", s.authorize(Account.admin, s.snmpTrapStatus))
	mux.HandleFunc("PUT /api/settings/snmp", s.authorize(Account.admin, s.saveSNMPTraps))

	mux.HandleFunc("PUT /api/network/{id}", s.authorize(func(a Account) bool { return a.network() && a.write() }, s.putNetworkDevice))
	mux.HandleFunc("DELETE /api/network/{id}", s.authorize(func(a Account) bool { return a.network() && a.write() }, s.deleteNetworkDevice))
	mux.HandleFunc("POST /api/network/check", s.authorize(func(a Account) bool { return a.network() && a.write() }, s.requestNetworkPoll))
	mux.HandleFunc("DELETE /api/agents/{id}", s.authorize(Account.admin, s.deleteAgent))
	mux.HandleFunc("POST /api/agent/report", s.ingestAgent)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" || r.URL.Path == "/style.css" || strings.HasSuffix(r.URL.Path, ".js") {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data: https://tile.openstreetmap.org; base-uri 'none'; frame-ancestors 'none'")
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
	key := accountKey(in.Username)
	user, exists := s.accountLocked(key)
	if !exists {
		// Spend the same amount of work for an unknown login.
		user.Salt, user.PassHash = s.state.Salt, s.state.PassHash
	}
	s.mu.Unlock()
	if len(in.Password) > 1024 || !verifyAccountPassword(in.Password, user) || !exists {
		problem(w, 401, "Неверный логин или пароль")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes(32))
	s.mu.Lock()
	current, exists := s.accountLocked(key)
	if !exists || current != user {
		s.mu.Unlock()
		problem(w, 401, "Неверный логин или пароль")
		return
	}
	for key, session := range s.sessions {
		if !now.Before(session.Expires) {
			delete(s.sessions, key)
		}
	}
	s.sessions[token] = userSession{AccountKey: key, Expires: time.Now().Add(12 * time.Hour)}
	delete(s.loginAttempts, ip)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "storavix_session", Value: token, Path: "/", HttpOnly: true, Secure: s.secureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	sendJSON(w, 200, map[string]any{"status": "ok", "user": current.Account})
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
	hosts := len(s.agents)
	arrays, drives := s.storageCounts()
	network := len(s.state.Network)
	user := requestAccount(r)
	if !user.network() {
		network = 0
	}
	if !user.full() {
		total = 0
	}
	if !user.admin() {
		mailReady = false
	}
	online := s.onlineCount()
	if !user.servers() {
		hosts, arrays, drives, online = 0, 0, 0, 0
	}
	s.mu.Unlock()
	sendJSON(w, 200, map[string]any{"hosts": hosts, "arrays": arrays, "drives": drives, "network": network, "events": total, "mailConfigured": mailReady, "collectorsActive": online, "mode": "live", "version": version})
}
func (s *server) getMail(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.state.Mail
	cfg.HasSecret = s.state.EncryptedSecret != ""
	s.mu.Unlock()
	sendJSON(w, 200, cfg)
}
func validateMail(c MailConfig) error {
	if err := (smtpDestination{c.Host, c.Port, c.Security}).validate(); err != nil {
		return err
	}
	pin, err := normalizedCertSHA256(c.CertSHA256)
	if err != nil {
		return err
	}
	if pin != "" && c.Security == "none" {
		return errors.New("Отпечаток сертификата применяется только при STARTTLS или SSL/TLS")
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

type mailSettingsInput struct {
	MailConfig
	Password string `json:"password"`
}

func (s *server) setMail(w http.ResponseWriter, r *http.Request) {
	var in mailSettingsInput
	if e := decodeJSON(r, &in); e != nil {
		problem(w, 400, "Неверные поля: "+e.Error())
		return
	}
	if e := validateMail(in.MailConfig); e != nil {
		problem(w, 400, e.Error())
		return
	}
	if e := validateAlertSelection(in.MailConfig); e != nil {
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
	in.CertSHA256, _ = normalizedCertSHA256(in.CertSHA256)
	previous, previousSecret := s.state.Mail, s.state.EncryptedSecret
	s.state.Mail = in.MailConfig
	s.state.EncryptedSecret = enc
	if e := s.saveLocked(); e != nil {
		s.state.Mail, s.state.EncryptedSecret = previous, previousSecret
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
func (s *server) mailForTest(r *http.Request) (MailConfig, string, error) {
	if r.Body == http.NoBody || r.ContentLength == 0 {
		return s.mailSnapshot()
	}
	var in mailSettingsInput
	if err := decodeJSON(r, &in); err != nil {
		return MailConfig{}, "", errors.New("Неверные настройки SMTP")
	}
	if err := validateMail(in.MailConfig); err != nil {
		return MailConfig{}, "", err
	}
	if len(in.Password) > 4096 {
		return MailConfig{}, "", errors.New("Слишком длинный секрет")
	}
	secret := in.Password
	if in.Auth != "none" && secret == "" {
		saved, previous, err := s.mailSnapshot()
		if err != nil {
			return MailConfig{}, "", err
		}
		if strings.EqualFold(saved.Host, in.Host) && saved.Username == in.Username {
			secret = previous
		}
		if secret == "" {
			return MailConfig{}, "", errors.New("Укажите пароль или токен для этого SMTP-сервера и логина")
		}
	}
	return in.MailConfig, secret, nil
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
	cfg, secret, e := s.mailForTest(r)
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
	occurred := time.Now().UTC()
	if !s.checkMailRate() {
		problem(w, 429, "Повторная отправка возможна через 15 секунд")
		return
	}
	cfg, secret, e := s.mailForTest(r)
	if e == nil {
		e = sendMail(cfg, secret, "[TEST] Storavix — тестовое оповещение", "Это тестовое уведомление Storavix для проверки SMTP.")
	}
	alert := Alert{ID: hex.EncodeToString(randomBytes(8)), CreatedAt: occurred, Severity: "info", Subject: "Тестовое оповещение", Detail: "Проверка маршрута SMTP", Delivered: e == nil}
	if e != nil {
		alert.Delivery = "failed"
		alert.DeliveryError = safeErr(e)
	} else {
		alert.Delivery = "sent"
	}
	s.mu.Lock()
	previous := s.state.Alerts
	s.state.Alerts = trimAlerts(append([]Alert{alert}, previous...))
	saveErr := s.saveLocked()
	if saveErr != nil {
		s.state.Alerts = previous
	}
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
	return sendMailContext(context.Background(), cfg, secret, subject, body)
}

func sendMailContext(ctx context.Context, cfg MailConfig, secret, subject, body string) error {
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
	pin, _ := normalizedCertSHA256(cfg.CertSHA256)
	tlsCfg, e := smtpTLSConfig(cfg.Host, cfg.CAFile, pin)
	if e != nil {
		return e
	}
	client, e := connectSMTP(ctx, smtpDestination{cfg.Host, cfg.Port, cfg.Security}, tlsCfg)
	if e != nil {
		return e
	}
	defer client.Close()
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
