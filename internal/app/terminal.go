package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
	"wotm/internal/config"
)

type wire struct {
	Type        string `json:"type"`
	Message     string `json:"message,omitempty"`
	Password    string `json:"password,omitempty"`
	Cols        int    `json:"cols,omitempty"`
	Rows        int    `json:"rows,omitempty"`
	PublicKey   string `json:"public_key,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Changed     bool   `json:"changed,omitempty"`
	TransferID  string `json:"transfer_id,omitempty"`
}
type socketWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (s *socketWriter) send(kind int, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return s.conn.WriteMessage(kind, b)
}
func (s *socketWriter) event(v wire) error {
	b, _ := json.Marshal(v)
	return s.send(websocket.TextMessage, b)
}
func (s *socketWriter) Write(b []byte) (int, error) {
	total := len(b)
	for len(b) > 0 {
		n := min(len(b), 16<<10)
		if err := s.send(websocket.BinaryMessage, b[:n]); err != nil {
			return 0, err
		}
		b = b[n:]
	}
	return total, nil
}
func dimensions(cols, rows int) (int, int) { return max(2, min(cols, 500)), max(2, min(rows, 300)) }

func (a *App) terminal(w http.ResponseWriter, r *http.Request) {
	c, _, _ := a.store.Snapshot()
	var host config.Host
	found := false
	for _, h := range c.Hosts {
		if h.ID == r.PathValue("id") {
			host = h
			found = true
			break
		}
	}
	if !found {
		fail(w, 404, "主机不存在")
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: sameOrigin, ReadBufferSize: 16 << 10, WriteBufferSize: 16 << 10}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	socket := &socketWriter{conn: conn}
	conn.SetReadLimit(64 << 10)
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	var init wire
	if err := conn.ReadJSON(&init); err != nil || init.Type != "init" {
		return
	}
	cols, rows := dimensions(init.Cols, init.Rows)
	var method ssh.AuthMethod
	if host.Auth.Mode == "key" {
		for _, key := range c.Keys {
			if key.ID == host.Auth.KeyID {
				signer, e := config.ParsePrivateKey(key.PrivateKey)
				if e != nil {
					_ = socket.event(wire{Type: "error", Message: "私钥解析失败"})
					return
				}
				method = ssh.PublicKeys(signer)
				break
			}
		}
	} else {
		method = ssh.Password(init.Password)
	}
	init.Password = ""
	if method == nil {
		_ = socket.event(wire{Type: "error", Message: "未找到认证密钥"})
		return
	}
	var candidate ssh.PublicKey
	hostKeyError := errors.New("host key confirmation required")
	sshConfig := &ssh.ClientConfig{User: host.Username, Auth: []ssh.AuthMethod{method}, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if host.TrustedHostKey != "" {
			trusted, _, _, _, e := ssh.ParseAuthorizedKey([]byte(host.TrustedHostKey))
			if e == nil && bytes.Equal(trusted.Marshal(), key.Marshal()) {
				return nil
			}
		}
		candidate = key
		return hostKeyError
	}}
	address := net.JoinHostPort(host.Address, strconv.Itoa(host.Port))
	nc, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		_ = socket.event(wire{Type: "error", Message: "无法连接主机，请检查地址、端口和网络"})
		return
	}
	defer nc.Close()
	_ = nc.SetDeadline(time.Now().Add(15 * time.Second))
	cc, chans, reqs, err := ssh.NewClientConn(nc, address, sshConfig)
	if err != nil {
		if candidate != nil {
			_ = socket.event(wire{Type: "host_key", PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(candidate))), Fingerprint: ssh.FingerprintSHA256(candidate), Changed: host.TrustedHostKey != ""})
		} else {
			_ = socket.event(wire{Type: "error", Message: "SSH 握手或认证失败，请检查用户名、密码、密钥及服务器配置"})
		}
		return
	}
	client := ssh.NewClient(cc, chans, reqs)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		_ = socket.event(wire{Type: "error", Message: "无法创建 SSH 会话"})
		return
	}
	defer session.Close()
	stdin, err := session.StdinPipe()
	if err != nil {
		return
	}
	session.Stdout = socket
	session.Stderr = socket
	if err = session.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}); err != nil {
		_ = socket.event(wire{Type: "error", Message: "服务器无法分配终端"})
		return
	}
	if err = session.Shell(); err != nil {
		_ = socket.event(wire{Type: "error", Message: "无法启动远程 Shell"})
		return
	}
	_ = nc.SetDeadline(time.Time{})
	transferID := token()
	cookie, _ := r.Cookie("wotm_session")
	a.mu.Lock()
	a.transfers[transferID] = transferSession{client: client, owner: cookie.Value}
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.transfers, transferID); a.mu.Unlock() }()
	if err = socket.event(wire{Type: "ready", TransferID: transferID}); err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if !a.authenticated(r) {
					_ = socket.event(wire{Type: "error", Message: "登录已失效，请重新登录"})
					_ = conn.Close()
					return
				}
				if socket.send(websocket.PingMessage, nil) != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()
	go func() {
		err := session.Wait()
		message := "远程会话已结束"
		if err != nil {
			message = "远程会话已退出或连接中断"
		}
		_ = socket.event(wire{Type: "exit", Message: message})
		_ = conn.Close()
	}()
	for {
		kind, b, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if !a.authenticated(r) {
			return
		}
		if kind == websocket.BinaryMessage {
			if _, err = stdin.Write(b); err != nil {
				return
			}
			continue
		}
		var msg wire
		if json.Unmarshal(b, &msg) != nil {
			return
		}
		if msg.Type == "resize" {
			cols, rows = dimensions(msg.Cols, msg.Rows)
			_ = session.WindowChange(rows, cols)
		}
	}
}
