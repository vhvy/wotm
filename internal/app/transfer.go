package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const maxUploadSize = 256 << 20

type transferSession struct {
	client *ssh.Client
	owner  string
}

func (a *App) transfer(w http.ResponseWriter, r *http.Request) (*ssh.Client, bool) {
	cookie, err := r.Cookie("wotm_session")
	if err != nil {
		fail(w, 401, "请先登录")
		return nil, false
	}
	a.mu.Lock()
	transfer, ok := a.transfers[r.PathValue("id")]
	a.mu.Unlock()
	if !ok || transfer.owner != cookie.Value {
		fail(w, 404, "终端会话已结束，请重新连接后上传")
		return nil, false
	}
	return transfer.client, true
}

// Each HTTP operation owns its SFTP subsystem, while reusing the authenticated SSH transport.
// Cancellation closes only this subsystem, never the interactive terminal.
func openSFTP(ctx context.Context, client *ssh.Client) (*sftp.Client, func(), error) {
	type opened struct {
		session *ssh.Session
		err     error
	}
	result := make(chan opened)
	go func() {
		session, err := client.NewSession()
		select {
		case result <- opened{session, err}:
		case <-ctx.Done():
			if session != nil {
				_ = session.Close()
			}
		}
	}()
	var session *ssh.Session
	select {
	case value := <-result:
		if value.err != nil {
			return nil, nil, value.err
		}
		session = value.session
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	stop := context.AfterFunc(ctx, func() { _ = session.Close() })
	cleanup := func() { stop(); _ = session.Close() }
	stdin, err := session.StdinPipe()
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if err = session.RequestSubsystem("sftp"); err != nil {
		cleanup()
		return nil, nil, err
	}
	sc, err := sftp.NewClientPipe(stdout, stdin)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return sc, func() { cleanup(); _ = sc.Close() }, nil
}

func (a *App) transferInfo(w http.ResponseWriter, r *http.Request) {
	client, ok := a.transfer(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	sc, close, err := openSFTP(ctx, client)
	if err != nil {
		fail(w, 502, "无法启动 SFTP，请确认服务器已启用 SFTP 子系统")
		return
	}
	defer close()
	directory, err := sc.Getwd()
	if err != nil {
		fail(w, 502, "无法读取远程默认目录")
		return
	}
	writeJSON(w, 200, map[string]any{"directory": directory, "max_size": maxUploadSize})
}

func (a *App) uploadFile(w http.ResponseWriter, r *http.Request) {
	client, ok := a.transfer(w, r)
	if !ok {
		return
	}
	name, directory := r.URL.Query().Get("name"), r.URL.Query().Get("directory")
	if name == "" || name == "." || name == ".." || len(name) > 255 || strings.ContainsAny(name, "/\\\x00\r\n") || !path.IsAbs(directory) || strings.ContainsAny(directory, "\x00\r\n") {
		fail(w, 400, "文件名无效，或目标目录不是 Linux 绝对路径")
		return
	}
	if r.ContentLength > maxUploadSize {
		fail(w, 413, "单个文件不能超过 256 MiB")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(15 * time.Minute))
	defer http.NewResponseController(w).SetReadDeadline(time.Time{})
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	sc, close, err := openSFTP(ctx, client)
	if err != nil {
		fail(w, 502, "SFTP 连接失败，请检查终端连接及服务器配置")
		return
	}
	defer close()
	directory = path.Clean(directory)
	stat, err := sc.Stat(directory)
	if err != nil || !stat.IsDir() {
		fail(w, 400, "目标目录不存在或无权访问")
		return
	}
	destination := path.Join(directory, name)
	// Exclusive creation refuses existing files and symlinks; never truncate a remote file.
	file, err := sc.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		if _, statErr := sc.Lstat(destination); statErr == nil {
			fail(w, 409, "同名文件已存在，请更换文件名或目标目录")
		} else {
			fail(w, 403, "无法创建远程文件，请检查目录权限")
		}
		return
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = sc.Remove(destination)
		}
	}()
	written, err := io.Copy(file, r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, 413, "单个文件不能超过 256 MiB")
		} else {
			fail(w, 502, "上传中断，未完成的文件将被清理")
		}
		return
	}
	if err = file.Close(); err != nil {
		fail(w, 502, "远程文件保存失败")
		return
	}
	complete = true
	writeJSON(w, 201, map[string]any{"path": destination, "size": written})
}
