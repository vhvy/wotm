package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"wotm/internal/config"
)

type loginSession struct {
	Expires    time.Time
	Credential [32]byte
}
type attempt struct {
	Count int
	Until time.Time
}
type App struct {
	store     *config.Store
	assets    fs.FS
	version   string
	mu        sync.Mutex
	sessions  map[string]loginSession
	attempts  map[string]attempt
	transfers map[string]transferSession
}

func New(store *config.Store, assets fs.FS, version string) http.Handler {
	a := &App{store: store, assets: assets, version: version, sessions: map[string]loginSession{}, attempts: map[string]attempt{}, transfers: map[string]transferSession{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "version": version})
	})
	mux.HandleFunc("POST /api/login", a.login)
	mux.Handle("POST /api/logout", a.require(http.HandlerFunc(a.logout)))
	mux.Handle("GET /api/config", a.require(http.HandlerFunc(a.snapshot)))
	mux.Handle("POST /api/keys", a.require(http.HandlerFunc(a.createKey)))
	mux.Handle("PUT /api/keys/{id}", a.require(http.HandlerFunc(a.renameKey)))
	mux.Handle("DELETE /api/keys/{id}", a.require(http.HandlerFunc(a.deleteKey)))
	mux.Handle("POST /api/proxies", a.require(http.HandlerFunc(a.saveProxy)))
	mux.Handle("PUT /api/proxies/{id}", a.require(http.HandlerFunc(a.saveProxy)))
	mux.Handle("DELETE /api/proxies/{id}", a.require(http.HandlerFunc(a.deleteProxy)))
	mux.Handle("POST /api/hosts", a.require(http.HandlerFunc(a.saveHost)))
	mux.Handle("PUT /api/hosts/{id}", a.require(http.HandlerFunc(a.saveHost)))
	mux.Handle("DELETE /api/hosts/{id}", a.require(http.HandlerFunc(a.deleteHost)))
	mux.Handle("PUT /api/hosts/{id}/trust", a.require(http.HandlerFunc(a.trustHost)))
	mux.Handle("PUT /api/credentials", a.require(http.HandlerFunc(a.credentials)))
	mux.Handle("GET /api/terminal/{id}", a.require(http.HandlerFunc(a.terminal)))
	mux.Handle("GET /api/transfers/{id}", a.require(http.HandlerFunc(a.transferInfo)))
	mux.Handle("PUT /api/transfers/{id}/file", a.require(http.HandlerFunc(a.uploadFile)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "接口不存在") })
	mux.HandleFunc("/", a.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; font-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if !sameOrigin(r) {
				fail(w, 403, "请求来源不匹配")
				return
			}
			isUpload := r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/transfers/") && r.Header.Get("Content-Type") == "application/octet-stream"
			if r.Method != "DELETE" && !isUpload && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				fail(w, 415, "请使用 application/json")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host)
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func body(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		fail(w, 400, "请求内容无效或过大")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, "请求必须是单个 JSON 对象")
		return false
	}
	return true
}
func token() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}
func credential(c config.Credentials) [32]byte {
	return sha256.Sum256([]byte(c.Username + "\x00" + c.Password))
}
func equal(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
func (a *App) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie("wotm_session")
	if err != nil {
		return false
	}
	c := a.store.Credentials()
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[cookie.Value]
	if !ok {
		return false
	}
	if time.Now().After(s.Expires) || s.Credential != credential(c) {
		delete(a.sessions, cookie.Value)
		return false
	}
	return true
}
func (a *App) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authenticated(r) {
			fail(w, 401, "请先登录")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !body(w, r, &v) {
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	now := time.Now()
	a.mu.Lock()
	for k, v := range a.attempts {
		if now.After(v.Until) {
			delete(a.attempts, k)
		}
	}
	for k, v := range a.sessions {
		if now.After(v.Expires) {
			delete(a.sessions, k)
		}
	}
	at := a.attempts[ip]
	if at.Count >= 10 && now.Before(at.Until) {
		a.mu.Unlock()
		fail(w, 429, "尝试过于频繁，请稍后再试")
		return
	}
	if len(a.attempts) >= 4096 || len(a.sessions) >= 1024 {
		a.mu.Unlock()
		fail(w, 429, "会话过多，请稍后再试")
		return
	}
	if at.Count == 0 {
		at.Until = now.Add(5 * time.Minute)
	}
	at.Count++
	a.attempts[ip] = at
	a.mu.Unlock()
	c, _, _ := a.store.Snapshot()
	userOK, passOK := equal(v.Username, c.Web.Username), equal(v.Password, c.Web.Password)
	if !userOK || !passOK {
		fail(w, 401, "用户名或密码不正确")
		return
	}
	id := token()
	a.mu.Lock()
	delete(a.attempts, ip)
	a.sessions[id] = loginSession{Expires: now.Add(12 * time.Hour), Credential: credential(c.Web)}
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "wotm_session", Value: id, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 3600})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("wotm_session"); err == nil {
		a.mu.Lock()
		delete(a.sessions, c.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "wotm_session", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

type keyView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public_key"`
}

func (a *App) snapshot(w http.ResponseWriter, r *http.Request) {
	c, rev, loadError := a.store.Snapshot()
	keys := make([]keyView, 0, len(c.Keys))
	for _, k := range c.Keys {
		s, _ := config.ParsePrivateKey(k.PrivateKey)
		p := s.PublicKey()
		keys = append(keys, keyView{k.ID, k.Name, p.Type(), ssh.FingerprintSHA256(p), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(p)))})
	}
	proxies := make([]proxyView, 0, len(c.Proxies))
	for _, p := range c.Proxies {
		proxies = append(proxies, proxyView{Proxy: p, HasPassword: p.Password != ""})
	}
	writeJSON(w, 200, map[string]any{"revision": rev, "load_error": loadError, "username": c.Web.Username, "keys": keys, "proxies": proxies, "hosts": c.Hosts, "version": a.version})
}
func (a *App) update(w http.ResponseWriter, r *http.Request, f func(*config.Config) error) {
	if err := a.store.Update(strings.Trim(r.Header.Get("If-Match"), "\""), f); err != nil {
		status := 400
		if errors.Is(err, config.ErrConflict) {
			status = 409
		}
		fail(w, status, err.Error())
		return
	}
	a.snapshot(w, r)
}
func (a *App) createKey(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Name       string `json:"name"`
		PrivateKey string `json:"private_key"`
		Generate   bool   `json:"generate"`
	}
	if !body(w, r, &v) {
		return
	}
	if v.Generate {
		if v.PrivateKey != "" {
			fail(w, 400, "生成密钥时不能同时上传私钥")
			return
		}
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fail(w, 500, "密钥生成失败")
			return
		}
		block, err := ssh.MarshalPrivateKey(key, "wotm")
		if err != nil {
			fail(w, 500, "密钥编码失败")
			return
		}
		v.PrivateKey = string(pem.EncodeToMemory(block))
	}
	if _, err := config.ParsePrivateKey(v.PrivateKey); err != nil {
		fail(w, 400, err.Error())
		return
	}
	a.update(w, r, func(c *config.Config) error {
		c.Keys = append(c.Keys, config.Key{ID: token()[:16], Name: strings.TrimSpace(v.Name), PrivateKey: v.PrivateKey})
		return nil
	})
}
func (a *App) renameKey(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Name string `json:"name"`
	}
	if !body(w, r, &v) {
		return
	}
	a.update(w, r, func(c *config.Config) error {
		for i := range c.Keys {
			if c.Keys[i].ID == r.PathValue("id") {
				c.Keys[i].Name = strings.TrimSpace(v.Name)
				return nil
			}
		}
		return errors.New("密钥不存在")
	})
}
func (a *App) deleteKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.update(w, r, func(c *config.Config) error {
		for _, h := range c.Hosts {
			if h.Auth.KeyID == id {
				return errors.New("密钥正在被主机使用，请先解除关联")
			}
		}
		for i, k := range c.Keys {
			if k.ID == id {
				c.Keys = append(c.Keys[:i], c.Keys[i+1:]...)
				return nil
			}
		}
		return errors.New("密钥不存在")
	})
}
func (a *App) saveHost(w http.ResponseWriter, r *http.Request) {
	var h config.Host
	if !body(w, r, &h) {
		return
	}
	h.Name = strings.TrimSpace(h.Name)
	h.Address = strings.TrimSpace(h.Address)
	h.Username = strings.TrimSpace(h.Username)
	if h.Auth.Mode == "password_prompt" {
		h.Auth.KeyID = ""
	}
	a.update(w, r, func(c *config.Config) error {
		if r.Method == "POST" {
			h.ID = token()[:16]
			h.TrustedHostKey = ""
			c.Hosts = append(c.Hosts, h)
			return nil
		}
		for i, old := range c.Hosts {
			if old.ID == r.PathValue("id") {
				h.ID = old.ID
				h.TrustedHostKey = old.TrustedHostKey
				if old.Address != h.Address || old.Port != h.Port {
					h.TrustedHostKey = ""
				}
				c.Hosts[i] = h
				return nil
			}
		}
		return errors.New("主机不存在")
	})
}
func (a *App) deleteHost(w http.ResponseWriter, r *http.Request) {
	a.update(w, r, func(c *config.Config) error {
		for i, h := range c.Hosts {
			if h.ID == r.PathValue("id") {
				c.Hosts = append(c.Hosts[:i], c.Hosts[i+1:]...)
				return nil
			}
		}
		return errors.New("主机不存在")
	})
}
func (a *App) trustHost(w http.ResponseWriter, r *http.Request) {
	var v struct {
		PublicKey string `json:"public_key"`
	}
	if !body(w, r, &v) {
		return
	}
	p, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(v.PublicKey))
	if err != nil || len(strings.TrimSpace(string(rest))) != 0 {
		fail(w, 400, "主机公钥无效")
		return
	}
	a.update(w, r, func(c *config.Config) error {
		for i := range c.Hosts {
			if c.Hosts[i].ID == r.PathValue("id") {
				c.Hosts[i].TrustedHostKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(p)))
				return nil
			}
		}
		return errors.New("主机不存在")
	})
}
func (a *App) credentials(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !body(w, r, &v) {
		return
	}
	a.update(w, r, func(c *config.Config) error {
		c.Web.Username = strings.TrimSpace(v.Username)
		if v.Password != "" {
			c.Web.Password = v.Password
		}
		return nil
	})
}
func (a *App) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return
	}
	if r.URL.Path == "/" {
		if _, err := fs.Stat(a.assets, "index.html"); err != nil {
			http.Error(w, "Frontend not built. Run: cd web && npm ci && npm run build", 503)
			return
		}
	}
	http.FileServer(http.FS(a.assets)).ServeHTTP(w, r)
}
