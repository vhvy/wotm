package app

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"wotm/internal/config"
)

type socksFixture struct {
	address     string
	requests    chan string
	connections atomic.Int32
}

func startSOCKS(t *testing.T, authenticate bool, target string) *socksFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &socksFixture{address: listener.Addr().String(), requests: make(chan string, 8)}
	var connections sync.Map
	t.Cleanup(func() {
		_ = listener.Close()
		connections.Range(func(key, value any) bool { _ = key.(net.Conn).Close(); return true })
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(conn, true)
			f.connections.Add(1)
			go func() {
				defer connections.Delete(conn)
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				var greeting [2]byte
				if _, err := io.ReadFull(conn, greeting[:]); err != nil || greeting[0] != 5 {
					return
				}
				methods := make([]byte, int(greeting[1]))
				if _, err := io.ReadFull(conn, methods); err != nil {
					return
				}
				method := byte(0)
				if authenticate {
					method = 2
				}
				supported := false
				for _, m := range methods {
					supported = supported || m == method
				}
				if !supported {
					_, _ = conn.Write([]byte{5, 255})
					return
				}
				_, _ = conn.Write([]byte{5, method})
				if authenticate {
					var header [2]byte
					if _, err := io.ReadFull(conn, header[:]); err != nil || header[0] != 1 {
						return
					}
					user := make([]byte, int(header[1]))
					if _, err := io.ReadFull(conn, user); err != nil {
						return
					}
					var length [1]byte
					if _, err := io.ReadFull(conn, length[:]); err != nil {
						return
					}
					password := make([]byte, int(length[0]))
					if _, err := io.ReadFull(conn, password); err != nil {
						return
					}
					if string(user) != "proxy-user" || string(password) != "proxy-password" {
						_, _ = conn.Write([]byte{1, 1})
						return
					}
					_, _ = conn.Write([]byte{1, 0})
				}
				var header [4]byte
				if _, err := io.ReadFull(conn, header[:]); err != nil || header[0] != 5 || header[1] != 1 {
					return
				}
				var host string
				switch header[3] {
				case 1, 4:
					size := 4
					if header[3] == 4 {
						size = 16
					}
					address := make([]byte, size)
					if _, err := io.ReadFull(conn, address); err != nil {
						return
					}
					host = net.IP(address).String()
				case 3:
					var length [1]byte
					if _, err := io.ReadFull(conn, length[:]); err != nil {
						return
					}
					address := make([]byte, int(length[0]))
					if _, err := io.ReadFull(conn, address); err != nil {
						return
					}
					host = string(address)
				default:
					return
				}
				var port [2]byte
				if _, err := io.ReadFull(conn, port[:]); err != nil {
					return
				}
				f.requests <- net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port[:]))))
				remote, err := net.DialTimeout("tcp", target, time.Second)
				if err != nil {
					_, _ = conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				defer remote.Close()
				_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				_ = conn.SetDeadline(time.Time{})
				done := make(chan struct{})
				go func() { _, _ = io.Copy(remote, conn); _ = remote.Close(); close(done) }()
				_, _ = io.Copy(conn, remote)
				_ = conn.Close()
				<-done
			}()
		}
	}()
	return f
}
func proxyConfig(address string, authenticate bool) config.Proxy {
	host, portText, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(portText)
	p := config.Proxy{ID: "office", Name: "Office SOCKS5", Address: host, Port: port}
	if authenticate {
		p.Username = "proxy-user"
		p.Password = "proxy-password"
	}
	return p
}

func TestTerminalAndSFTPThroughSOCKS5(t *testing.T) {
	for _, authenticate := range []bool{false, true} {
		t.Run(fmt.Sprintf("auth=%t", authenticate), func(t *testing.T) {
			remote := startSSH(t, nil)
			proxy := startSOCKS(t, authenticate, remote.addr)
			c := baseConfig()
			c.Proxies = []config.Proxy{proxyConfig(proxy.address, authenticate)}
			c.Hosts = []config.Host{{ID: "test", Name: "proxied", Address: "remote-only.invalid", Port: 2222, Username: "tester", ProxyID: "office", Auth: config.Auth{Mode: "password_prompt"}, TrustedHostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(remote.key.PublicKey())))}}
			f := newFixture(t, c)
			f.login(t)
			ws := f.socket(t, "test", "ssh-password")
			ready := nextEvent(t, ws)
			if ready.Type != "ready" {
				t.Fatalf("unexpected event %+v", ready)
			}
			select {
			case target := <-proxy.requests:
				if target != "remote-only.invalid:2222" {
					t.Fatal("hostname not delegated to proxy", target)
				}
			case <-time.After(time.Second):
				t.Fatal("no SOCKS5 CONNECT")
			}
			status, b := f.request(t, "GET", "/api/transfers/"+ready.TransferID, nil, "", "")
			if status != 200 {
				t.Fatalf("SFTP info %d %s", status, b)
			}
			var info struct {
				Directory string `json:"directory"`
			}
			if err := json.Unmarshal(b, &info); err != nil {
				t.Fatal(err)
			}
			query := url.Values{"directory": {info.Directory}, "name": {"via-socks.txt"}}
			r, err := http.NewRequest("PUT", f.server.URL+"/api/transfers/"+ready.TransferID+"/file?"+query.Encode(), strings.NewReader("proxied upload"))
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("Content-Type", "application/octet-stream")
			response, err := f.client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != 201 {
				t.Fatalf("upload %d %s", response.StatusCode, body)
			}
			if proxy.connections.Load() != 1 {
				t.Fatal("SFTP did not reuse SSH transport")
			}
			if err := ws.WriteMessage(2, []byte("socks-echo")); err != nil {
				t.Fatal(err)
			}
			for {
				kind, b, err := ws.ReadMessage()
				if err != nil {
					t.Fatal(err)
				}
				if kind == 2 && strings.Contains(string(b), "socks-echo") {
					break
				}
			}
		})
	}
}

func TestSOCKS5FailuresNeverFallBackToDirect(t *testing.T) {
	remote := startSSH(t, nil)
	address, portText, _ := net.SplitHostPort(remote.addr)
	port, _ := strconv.Atoi(portText)
	host := config.Host{Address: address, Port: port, ProxyID: "office"}
	proxy := startSOCKS(t, true, remote.addr)
	p := proxyConfig(proxy.address, true)
	p.Password = "incorrect"
	c := baseConfig()
	c.Proxies = []config.Proxy{p}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if conn, err := dialHost(ctx, c, host); err == nil {
		conn.Close()
		t.Fatal("authentication failure fell back to direct")
	}
	c.Proxies = nil
	if conn, err := dialHost(ctx, c, host); err == nil {
		conn.Close()
		t.Fatal("missing proxy fell back to direct")
	}
	// A peer that accepts TCP but stalls SOCKS5 negotiation must obey the total connection deadline.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			_, _ = io.Copy(io.Discard, conn)
		}
	}()
	c.Proxies = []config.Proxy{proxyConfig(listener.Addr().String(), false)}
	timeout, cancelTimeout := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancelTimeout()
	start := time.Now()
	if conn, err := dialHost(timeout, c, host); err == nil {
		conn.Close()
		t.Fatal("stalled negotiation did not time out")
	}
	if time.Since(start) > time.Second {
		t.Fatal("timeout was ignored")
	}
}

func TestProxyAPIPersistenceRedactionAndReferences(t *testing.T) {
	f := newFixture(t, baseConfig())
	f.login(t)
	payload := map[string]any{"name": "Office", "address": "127.0.0.1", "port": 1080, "username": "proxy-user", "password": "proxy-password"}
	status, b := f.request(t, "POST", "/api/proxies", payload, f.revision(), "")
	if status != 200 {
		t.Fatalf("%d %s", status, b)
	}
	if strings.Contains(string(b), "proxy-password") || !strings.Contains(string(b), `"has_password":true`) {
		t.Fatal("password exposed or missing password marker")
	}
	c, revision, _ := f.store.Snapshot()
	p := c.Proxies[0]
	if p.Password != "proxy-password" {
		t.Fatal("password not stored")
	}
	delete(payload, "password")
	payload["name"] = "Renamed"
	if status, b := f.request(t, "PUT", "/api/proxies/"+p.ID, payload, revision, ""); status != 200 {
		t.Fatalf("%d %s", status, b)
	}
	c, _, _ = f.store.Snapshot()
	if c.Proxies[0].Password != "proxy-password" {
		t.Fatal("omitted password was erased")
	}
	if status, _ := f.request(t, "PUT", "/api/proxies/"+p.ID, payload, revision, ""); status != 409 {
		t.Fatal("stale proxy update accepted")
	}
	h := config.Host{ID: "test", Name: "Host", Address: "example.invalid", Port: 22, Username: "root", ProxyID: p.ID, Auth: config.Auth{Mode: "password_prompt"}}
	if status, b := f.request(t, "POST", "/api/hosts", h, f.revision(), ""); status != 200 {
		t.Fatalf("host %d %s", status, b)
	}
	if status, _ := f.request(t, "DELETE", "/api/proxies/"+p.ID, nil, f.revision(), ""); status != 400 {
		t.Fatal("referenced proxy deleted")
	}
	c, _, _ = f.store.Snapshot()
	h = c.Hosts[0]
	h.ProxyID = ""
	if status, _ := f.request(t, "PUT", "/api/hosts/"+h.ID, h, f.revision(), ""); status != 200 {
		t.Fatal("could not switch to direct")
	}
	if status, _ := f.request(t, "DELETE", "/api/proxies/"+p.ID, nil, f.revision(), ""); status != 200 {
		t.Fatal("could not delete unreferenced proxy")
	}
	// Validate disk output too, not just an in-memory snapshot.
	if _, err := config.Open(f.path); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(f.path); err != nil || strings.Contains(string(raw), "proxy-password") {
		t.Fatal("deleted proxy remained on disk")
	}
}
