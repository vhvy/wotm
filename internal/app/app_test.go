package app

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
	"wotm/internal/config"
)

type fixture struct {
	server *httptest.Server
	client *http.Client
	store  *config.Store
	path   string
}

func newFixture(t *testing.T, c config.Config) *fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	b, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(New(store, fstest.MapFS{"index.html": {Data: []byte("<html>WOTM</html>")}}, "test"))
	t.Cleanup(s.Close)
	jar, _ := cookiejar.New(nil)
	return &fixture{s, &http.Client{Jar: jar, Timeout: 5 * time.Second}, store, path}
}
func baseConfig() config.Config {
	return config.Config{Version: 1, Web: config.Credentials{Username: "admin", Password: "plain-password"}, Keys: []config.Key{}, Hosts: []config.Host{}}
}
func (f *fixture) request(t *testing.T, method, path string, data any, revision, origin string) (int, []byte) {
	t.Helper()
	var raw []byte
	if data != nil {
		raw, _ = json.Marshal(data)
	}
	r, err := http.NewRequest(method, f.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	if revision != "" {
		r.Header.Set("If-Match", revision)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	resp, err := f.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, b
}
func (f *fixture) login(t *testing.T) {
	t.Helper()
	status, b := f.request(t, "POST", "/api/login", map[string]string{"username": "admin", "password": "plain-password"}, "", "")
	if status != 200 {
		t.Fatalf("login: %d %s", status, b)
	}
}
func (f *fixture) revision() string { _, rev, _ := f.store.Snapshot(); return rev }
func TestAuthenticationOriginAndRedaction(t *testing.T) {
	f := newFixture(t, baseConfig())
	if status, _ := f.request(t, "GET", "/api/config", nil, "", ""); status != 401 {
		t.Fatal(status)
	}
	if status, _ := f.request(t, "POST", "/api/login", map[string]string{"username": "admin", "password": "plain-password"}, "", "https://evil.example"); status != 403 {
		t.Fatal(status)
	}
	f.login(t)
	status, b := f.request(t, "POST", "/api/keys", map[string]any{"name": "Main", "generate": true}, f.revision(), "")
	if status != 200 {
		t.Fatalf("%d %s", status, b)
	}
	if strings.Contains(string(b), "PRIVATE KEY") || strings.Contains(string(b), "plain-password") || strings.Contains(string(b), "private_key") {
		t.Fatal("secret leaked")
	}
	c, _, _ := f.store.Snapshot()
	if len(c.Keys) != 1 || !strings.Contains(c.Keys[0].PrivateKey, "OPENSSH PRIVATE KEY") {
		t.Fatal("private key not saved")
	}
	if c.Web.Password != "plain-password" {
		t.Fatal("web password must remain plain text")
	}
	u, _ := url.Parse(f.server.URL)
	cookies := f.client.Jar.Cookies(u)
	if len(cookies) != 1 {
		t.Fatal("session missing")
	}
	if status, _ := f.request(t, "POST", "/api/hosts", config.Host{ID: "h", Name: "server", Address: "localhost", Port: 22, Username: "root", Auth: config.Auth{Mode: "key", KeyID: c.Keys[0].ID}}, f.revision(), ""); status != 200 {
		t.Fatal(status)
	}
	if status, _ := f.request(t, "DELETE", "/api/keys/"+c.Keys[0].ID, nil, f.revision(), ""); status != 400 {
		t.Fatal("deleted referenced key")
	}
	if status, _ := f.request(t, "PUT", "/api/credentials", map[string]string{"username": "admin", "password": "changed"}, f.revision(), ""); status != 200 {
		t.Fatal(status)
	}
	if status, _ := f.request(t, "GET", "/api/config", nil, "", ""); status != 401 {
		t.Fatal("old session not invalidated")
	}
}
func TestAPIConflictsAndManualEdits(t *testing.T) {
	f := newFixture(t, baseConfig())
	f.login(t)
	old := f.revision()
	c, _, _ := f.store.Snapshot()
	c.Hosts = []config.Host{{ID: "manual", Name: "manual", Address: "localhost", Port: 22, Username: "root", Auth: config.Auth{Mode: "password_prompt"}}}
	b, _ := yaml.Marshal(c)
	if err := os.WriteFile(f.path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if status, _ := f.request(t, "POST", "/api/keys", map[string]any{"name": "stale", "generate": true}, old, ""); status != 409 {
		t.Fatal("stale write accepted")
	}
	status, b := f.request(t, "GET", "/api/config", nil, "", "")
	if status != 200 || !bytes.Contains(b, []byte("manual")) {
		t.Fatalf("%d %s", status, b)
	}
	if err := os.WriteFile(f.path, []byte("web: [not valid"), 0600); err != nil {
		t.Fatal(err)
	}
	status, b = f.request(t, "GET", "/api/config", nil, "", "")
	if status != 200 || !bytes.Contains(b, []byte("manual")) || !bytes.Contains(b, []byte("YAML 解析失败")) {
		t.Fatalf("%d %s", status, b)
	}
}
func TestLoginRateLimit(t *testing.T) {
	f := newFixture(t, baseConfig())
	for i := 0; i < 10; i++ {
		status, _ := f.request(t, "POST", "/api/login", map[string]string{"username": "admin", "password": "wrong"}, "", "")
		if status != 401 {
			t.Fatal(status)
		}
	}
	if status, _ := f.request(t, "POST", "/api/login", map[string]string{"username": "admin", "password": "plain-password"}, "", ""); status != 429 {
		t.Fatal(status)
	}
}

type sshFixture struct {
	addr   string
	key    ssh.Signer
	resize chan bool
}

func startSSH(t *testing.T, publicKey ssh.PublicKey) *sshFixture {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if meta.User() == "tester" && string(password) == "ssh-password" {
			return nil, nil
		}
		return nil, fmt.Errorf("denied")
	}, PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if meta.User() == "tester" && publicKey != nil && bytes.Equal(publicKey.Marshal(), key.Marshal()) {
			return nil, nil
		}
		return nil, fmt.Errorf("denied")
	}}
	cfg.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fixture := &sshFixture{listener.Addr().String(), signer, make(chan bool, 10)}
	files := sftp.InMemHandler()
	var connections sync.Map
	t.Cleanup(func() { listener.Close(); connections.Range(func(k, v any) bool { k.(net.Conn).Close(); return true }) })
	go func() {
		for {
			nc, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(nc, true)
			go func() {
				defer connections.Delete(nc)
				defer nc.Close()
				sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				defer sc.Close()
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					if ch.ChannelType() != "session" {
						ch.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					channel, requests, err := ch.Accept()
					if err != nil {
						return
					}
					go func() {
						defer channel.Close()
						for req := range requests {
							switch req.Type {
							case "subsystem":
								var subsystem struct{ Name string }
								if ssh.Unmarshal(req.Payload, &subsystem) != nil || subsystem.Name != "sftp" {
									_ = req.Reply(false, nil)
									continue
								}
								_ = req.Reply(true, nil)
								server := sftp.NewRequestServer(channel, files)
								_ = server.Serve()
								_ = server.Close()
								return
							case "pty-req":
								req.Reply(true, nil)
							case "shell":
								req.Reply(true, nil)
								go func() { _, _ = channel.Write([]byte("hello-terminal\r\n")); _, _ = io.Copy(channel, channel) }()
							case "window-change":
								select {
								case fixture.resize <- true:
								default:
								}
							default:
								if req.WantReply {
									req.Reply(false, nil)
								}
							}
						}
					}()
				}
			}()
		}
	}()
	return fixture
}
func (f *fixture) socket(t *testing.T, id, password string) *websocket.Conn {
	t.Helper()
	u, _ := url.Parse(f.server.URL)
	header := http.Header{}
	header.Set("Origin", f.server.URL)
	for _, cookie := range f.client.Jar.Cookies(u) {
		header.Add("Cookie", cookie.String())
	}
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.server.URL, "http")+"/api/terminal/"+id, header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := ws.WriteJSON(wire{Type: "init", Password: password, Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	return ws
}
func nextEvent(t *testing.T, ws *websocket.Conn) wire {
	t.Helper()
	for {
		kind, b, err := ws.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.TextMessage {
			var v wire
			if err = json.Unmarshal(b, &v); err != nil {
				t.Fatal(err)
			}
			return v
		}
	}
}
func TestTerminalPasswordTrustResizeAndEcho(t *testing.T) {
	sshServer := startSSH(t, nil)
	address, portText, _ := net.SplitHostPort(sshServer.addr)
	var port int
	fmt.Sscan(portText, &port)
	c := baseConfig()
	c.Hosts = []config.Host{{ID: "test", Name: "SSH", Address: address, Port: port, Username: "tester", Auth: config.Auth{Mode: "password_prompt"}}}
	f := newFixture(t, c)
	f.login(t)
	ws := f.socket(t, "test", "ssh-password")
	candidate := nextEvent(t, ws)
	if candidate.Type != "host_key" || candidate.Changed || candidate.Fingerprint != ssh.FingerprintSHA256(sshServer.key.PublicKey()) {
		t.Fatalf("unexpected event %+v", candidate)
	}
	ws.Close()
	if status, b := f.request(t, "PUT", "/api/hosts/test/trust", map[string]string{"public_key": candidate.PublicKey}, f.revision(), ""); status != 200 {
		t.Fatalf("%d %s", status, b)
	}
	ws = f.socket(t, "test", "ssh-password")
	if event := nextEvent(t, ws); event.Type != "ready" {
		t.Fatalf("%+v", event)
	}
	if err := ws.WriteJSON(wire{Type: "resize", Cols: 120, Rows: 40}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sshServer.resize:
	case <-time.After(3 * time.Second):
		t.Fatal("resize not forwarded")
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte("echo-marker\n")); err != nil {
		t.Fatal(err)
	}
	var output string
	for !strings.Contains(output, "echo-marker") {
		kind, b, err := ws.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.BinaryMessage {
			output += string(b)
		}
	}
	ws.Close()
	ws = f.socket(t, "test", "wrong-password")
	if event := nextEvent(t, ws); event.Type != "error" {
		t.Fatalf("%+v", event)
	}
	ws.Close()
	_, wrongKey, _ := ed25519.GenerateKey(rand.Reader)
	wrongSigner, _ := ssh.NewSignerFromKey(wrongKey)
	if err := f.store.Update(f.revision(), func(c *config.Config) error {
		c.Hosts[0].TrustedHostKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(wrongSigner.PublicKey())))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ws = f.socket(t, "test", "ssh-password")
	if event := nextEvent(t, ws); event.Type != "host_key" || !event.Changed {
		t.Fatalf("changed host key not blocked: %+v", event)
	}
}
func TestTerminalPrivateKeyAuthentication(t *testing.T) {
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(private)
	block, _ := ssh.MarshalPrivateKey(private, "test")
	sshServer := startSSH(t, signer.PublicKey())
	address, portText, _ := net.SplitHostPort(sshServer.addr)
	var port int
	fmt.Sscan(portText, &port)
	c := baseConfig()
	c.Keys = []config.Key{{ID: "key", Name: "test", PrivateKey: string(pem.EncodeToMemory(block))}}
	c.Hosts = []config.Host{{ID: "test", Name: "SSH", Address: address, Port: port, Username: "tester", Auth: config.Auth{Mode: "key", KeyID: "key"}, TrustedHostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshServer.key.PublicKey())))}}
	f := newFixture(t, c)
	f.login(t)
	ws := f.socket(t, "test", "")
	if event := nextEvent(t, ws); event.Type != "ready" {
		t.Fatalf("%+v", event)
	}
}
