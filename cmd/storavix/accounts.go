package main

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	roleViewer   = "viewer"
	roleOperator = "operator"
	roleAdmin    = "admin"

	scopeServers  = "servers"
	scopeNetwork  = "infrastructure"
	scopeSwitches = "switches"
	scopeAll      = "all"
)

type Account struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Scope    string `json:"scope"`
	Owner    bool   `json:"owner,omitempty"`
}

type StoredAccount struct {
	Account
	Salt     string `json:"salt"`
	PassHash string `json:"passHash"`
}

type userSession struct {
	AccountKey string
	Expires    time.Time
}

func (a Account) admin() bool { return a.Role == roleAdmin && a.Scope == scopeAll }
func (a Account) network() bool {
	return a.Scope == scopeNetwork || a.Scope == scopeAll || a.Scope == scopeSwitches
}
func (a Account) servers() bool {
	return a.Scope == scopeServers || a.Scope == scopeNetwork || a.Scope == scopeAll
}
func (a Account) full() bool  { return a.Scope == scopeAll }
func (a Account) write() bool { return a.Role == roleOperator || a.admin() }

func (a Account) valid() bool {
	if a.Role == roleAdmin {
		return a.Scope == scopeAll
	}
	return (a.Role == roleOperator || a.Role == roleViewer) &&
		(a.Scope == scopeServers || a.Scope == scopeNetwork || a.Scope == scopeAll || a.Scope == scopeSwitches)
}

func accountKey(username string) string { return strings.ToLower(strings.TrimSpace(username)) }

func (s *server) accountLocked(key string) (StoredAccount, bool) {
	if key == accountKey(s.state.AdminUser) && s.state.AdminUser != "" {
		return StoredAccount{
			Account: Account{Username: s.state.AdminUser, Role: roleAdmin, Scope: scopeAll, Owner: true},
			Salt:    s.state.Salt, PassHash: s.state.PassHash,
		}, true
	}
	user, exists := s.state.Users[key]
	return user, exists && user.valid()
}

func (s *server) sessionAccount(r *http.Request) (Account, bool) {
	cookie, err := r.Cookie("storavix_session")
	if err != nil {
		return Account{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, exists := s.sessions[cookie.Value]
	if !exists || !time.Now().Before(session.Expires) {
		delete(s.sessions, cookie.Value)
		return Account{}, false
	}
	user, exists := s.accountLocked(session.AccountKey)
	return user.Account, exists
}

type accountContextKey struct{}

func requestAccount(r *http.Request) Account {
	user, _ := r.Context().Value(accountContextKey{}).(Account)
	return user
}

func (s *server) authorize(allowed func(Account) bool, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.sessionAccount(r)
		if !ok {
			problem(w, http.StatusUnauthorized, "Войдите в Storavix")
			return
		}
		if allowed != nil && !allowed(user) {
			problem(w, http.StatusForbidden, "Недостаточно прав для этой операции")
			return
		}
		handler(w, r.WithContext(context.WithValue(r.Context(), accountContextKey{}, user)))
	}
}

func (s *server) requireAuth(handler http.HandlerFunc) http.HandlerFunc {
	return s.authorize(nil, handler)
}

func (s *server) getSession(w http.ResponseWriter, r *http.Request) {
	user, exists := s.sessionAccount(r)
	result := map[string]any{"authenticated": exists, "version": version}
	if exists {
		result["user"] = user
	}
	sendJSON(w, http.StatusOK, result)
}

func (s *server) getAccounts(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	owner, _ := s.accountLocked(accountKey(s.state.AdminUser))
	users := make([]Account, 0, len(s.state.Users)+1)
	users = append(users, owner.Account)
	for _, u := range s.state.Users {
		users = append(users, u.Account)
	}
	s.mu.Unlock()
	sort.Slice(users[1:], func(i, j int) bool { return accountKey(users[i+1].Username) < accountKey(users[j+1].Username) })
	sendJSON(w, http.StatusOK, users)
}

var loginName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._@-]{0,63}$`)

func (s *server) sendAccountChange(w http.ResponseWriter, r *http.Request) {
	actor := accountKey(requestAccount(r).Username)
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
		Scope    string `json:"scope"`
	}
	create := r.Method == http.MethodPost
	if r.Method != http.MethodDelete && decodeJSON(r, &in) != nil {
		problem(w, http.StatusBadRequest, "Некорректные настройки пользователя")
		return
	}
	name := strings.TrimSpace(in.Username)
	if !create {
		name = r.PathValue("username")
	}
	key := accountKey(name)
	user := StoredAccount{Account: Account{Username: name, Role: in.Role, Scope: in.Scope}}
	if r.Method != http.MethodDelete && (!loginName.MatchString(name) || !user.valid() ||
		(create || in.Password != "") && !validAccountPassword(in.Password)) {
		problem(w, http.StatusBadRequest, "Проверьте логин, пароль (8–1024 символа), роль и доступ")
		return
	}
	if in.Password != "" {
		user.Salt, user.PassHash = hashAccountPassword(in.Password)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.accountLocked(key)
	if exists && (old.Owner || key == actor) {
		problem(w, http.StatusForbidden, "Измените свой пароль во вкладке «Безопасность». Учётная запись владельца защищена.")
		return
	}
	if create && exists {
		problem(w, http.StatusConflict, "Этот логин уже занят")
		return
	}
	if !create && !exists {
		problem(w, http.StatusNotFound, "Пользователь не найден")
		return
	}
	if create && len(s.state.Users) >= 300 {
		problem(w, http.StatusConflict, "Достигнут предел учётных записей")
		return
	}
	previous := s.state.Users
	next := make(map[string]StoredAccount, len(previous)+1)
	for k, v := range previous {
		next[k] = v
	}
	if r.Method == http.MethodDelete {
		delete(next, key)
	} else {
		if exists {
			user.Username = old.Username
		}
		if user.Salt == "" {
			user.Salt, user.PassHash = old.Salt, old.PassHash
		}
		next[key] = user
	}
	s.state.Users = next
	if err := s.saveLocked(); err != nil {
		s.state.Users = previous
		problem(w, http.StatusInternalServerError, "Не удалось сохранить учётную запись")
		return
	}
	s.revokeSessionsLocked(key)
	status := http.StatusOK
	if create {
		status = http.StatusCreated
	}
	sendJSON(w, status, map[string]string{"status": "ok"})
}

func (s *server) revokeSessionsLocked(key string) {
	for token, session := range s.sessions {
		if session.AccountKey == key {
			delete(s.sessions, token)
		}
	}
}

func validAccountPassword(password string) bool {
	return len([]rune(password)) >= 8 && len(password) <= 1024
}

func hashAccountPassword(password string) (saltHex, hash string) {
	salt := randomBytes(16)
	return hex.EncodeToString(salt), pbkdf2(password, salt)
}

func verifyAccountPassword(password string, user StoredAccount) bool {
	salt, _ := hex.DecodeString(user.Salt)
	hash := pbkdf2(password, salt)
	return subtle.ConstantTimeCompare([]byte(hash), []byte(user.PassHash)) == 1
}

func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if decodeJSON(r, &in) != nil || !validAccountPassword(in.New) || len(in.Old) > 1024 {
		problem(w, http.StatusBadRequest, "Новый пароль должен содержать от 8 символов и занимать до 1024 байт")
		return
	}
	key := accountKey(requestAccount(r).Username)
	s.mu.Lock()
	old, exists := s.accountLocked(key)
	s.mu.Unlock()
	if !exists || !verifyAccountPassword(in.Old, old) {
		problem(w, http.StatusForbidden, "Текущий пароль неверен")
		return
	}
	salt, hash := hashAccountPassword(in.New)
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.accountLocked(key)
	if !exists || old != current {
		problem(w, http.StatusConflict, "Учётная запись изменилась. Войдите снова")
		return
	}
	updated := current
	updated.Salt, updated.PassHash = salt, hash
	if updated.Owner {
		s.state.Salt, s.state.PassHash = salt, hash
	} else {
		s.state.Users[key] = updated
	}
	if err := s.saveLocked(); err != nil {
		if updated.Owner {
			s.state.Salt, s.state.PassHash = current.Salt, current.PassHash
		} else {
			s.state.Users[key] = current
		}
		problem(w, http.StatusInternalServerError, "Не удалось сохранить пароль")
		return
	}
	s.revokeSessionsLocked(key)
	http.SetCookie(w, &http.Cookie{Name: "storavix_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.secureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode})
	sendJSON(w, http.StatusOK, map[string]string{"status": "changed"})
}
