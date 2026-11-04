package app

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"wotm/internal/config"
)

func TestSFTPUploadUsesTerminalCredentialsAndRefusesOverwrite(t *testing.T) {
	remote := startSSH(t, nil)
	address, portText, _ := net.SplitHostPort(remote.addr)
	var port int
	fmt.Sscan(portText, &port)
	c := baseConfig()
	c.Hosts = []config.Host{{ID: "test", Name: "SSH", Address: address, Port: port, Username: "tester", Auth: config.Auth{Mode: "password_prompt"}, TrustedHostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(remote.key.PublicKey())))}}
	f := newFixture(t, c)
	f.login(t)
	ws := f.socket(t, "test", "ssh-password")
	ready := nextEvent(t, ws)
	if ready.Type != "ready" || ready.TransferID == "" {
		t.Fatalf("%+v", ready)
	}
	status, body := f.request(t, "GET", "/api/transfers/"+ready.TransferID, nil, "", "")
	if status != 200 || !bytes.Contains(body, []byte(`"directory":"/"`)) {
		t.Fatalf("info: %d %s", status, body)
	}
	upload := func(name string, data string, origin string, length int64) (int, []byte) {
		t.Helper()
		query := url.Values{"directory": {"/"}, "name": {name}}
		r, err := http.NewRequest("PUT", f.server.URL+"/api/transfers/"+ready.TransferID+"/file?"+query.Encode(), strings.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/octet-stream")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if length > 0 {
			r.ContentLength = length
		}
		response, err := f.client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		b, _ := io.ReadAll(response.Body)
		return response.StatusCode, b
	}
	if status, b := upload("hello 世界.txt", "upload-content", "", 0); status != 201 {
		t.Fatalf("upload %d %s", status, b)
	}
	if status, b := upload("hello 世界.txt", "overwritten", "", 0); status != 409 {
		t.Fatalf("overwrite %d %s", status, b)
	}
	if status, _ := upload("../escape.txt", "bad", "", 0); status != 400 {
		t.Fatal("path traversal accepted", status)
	}
	if status, _ := upload("evil.txt", "bad", "https://evil.example", 0); status != 403 {
		t.Fatal("cross-origin upload accepted", status)
	}
	// A different Web login cannot use another login's transfer capability.
	otherJar, _ := url.Parse(f.server.URL)
	oldCookies := f.client.Jar.Cookies(otherJar)
	f.login(t)
	if status, _ := f.request(t, "GET", "/api/transfers/"+ready.TransferID, nil, "", ""); status != 404 {
		t.Fatal("cross-session access accepted", status)
	}
	f.client.Jar.SetCookies(otherJar, oldCookies)
	client, err := ssh.Dial("tcp", remote.addr, &ssh.ClientConfig{User: "tester", Auth: []ssh.AuthMethod{ssh.Password("ssh-password")}, HostKeyCallback: ssh.FixedHostKey(remote.key.PublicKey()), Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sc, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	file, err := sc.Open("/hello 世界.txt")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(file)
	file.Close()
	if err != nil || string(contents) != "upload-content" {
		t.Fatalf("remote contents %q %v", contents, err)
	}
	// The original terminal remains usable after all SFTP requests.
	if err := ws.WriteMessage(2, []byte("still-connected")); err != nil {
		t.Fatal(err)
	}
	for {
		kind, b, err := ws.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind == 2 && bytes.Contains(b, []byte("still-connected")) {
			break
		}
	}
	ws.Close()
	deadline := time.Now().Add(time.Second)
	for {
		status, _ := f.request(t, "GET", "/api/transfers/"+ready.TransferID, nil, "", "")
		if status == 404 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed terminal retained transfer capability")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
