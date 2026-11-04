package config

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

const MaxSize = 4 << 20

var ErrConflict = errors.New("配置已发生变化，请刷新后重试")

type Credentials struct {
	Username string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"-"`
}
type Key struct {
	ID         string `yaml:"id" json:"id"`
	Name       string `yaml:"name" json:"name"`
	PrivateKey string `yaml:"private_key" json:"-"`
}
type Auth struct {
	Mode  string `yaml:"mode" json:"mode"`
	KeyID string `yaml:"key_id,omitempty" json:"key_id,omitempty"`
}
type Host struct {
	ID             string `yaml:"id" json:"id"`
	Name           string `yaml:"name" json:"name"`
	Address        string `yaml:"address" json:"address"`
	Port           int    `yaml:"port" json:"port"`
	Username       string `yaml:"username" json:"username"`
	Auth           Auth   `yaml:"auth" json:"auth"`
	TrustedHostKey string `yaml:"trusted_host_key,omitempty" json:"trusted_host_key,omitempty"`
	Group          string `yaml:"group,omitempty" json:"group,omitempty"`
	Remark         string `yaml:"remark,omitempty" json:"remark,omitempty"`
}
type Config struct {
	Version int         `yaml:"version" json:"version"`
	Web     Credentials `yaml:"web" json:"web"`
	Keys    []Key       `yaml:"keys" json:"keys"`
	Hosts   []Host      `yaml:"hosts" json:"hosts"`
}

func ParsePrivateKey(value string) (ssh.Signer, error) {
	if len(value) > 64<<10 {
		return nil, errors.New("私钥不能超过 64 KiB")
	}
	signer, err := ssh.ParsePrivateKey([]byte(value))
	var encrypted *ssh.PassphraseMissingError
	if errors.As(err, &encrypted) {
		return nil, errors.New("不支持带口令的私钥，请上传未加密的 OpenSSH 私钥")
	}
	if err != nil {
		return nil, errors.New("私钥格式无效，或私钥已加密")
	}
	return signer, nil
}

func Validate(c Config) error {
	if c.Version != 1 {
		return errors.New("version 必须为 1")
	}
	if strings.TrimSpace(c.Web.Username) == "" || c.Web.Password == "" {
		return errors.New("web.username 和 web.password 不能为空")
	}
	if len(c.Web.Username) > 128 || len(c.Web.Password) > 1024 {
		return errors.New("Web 用户名或密码过长")
	}
	keys := map[string]bool{}
	for _, k := range c.Keys {
		if !validID(k.ID) || keys[k.ID] || strings.TrimSpace(k.Name) == "" {
			return errors.New("密钥 ID 必须为 1–64 位字母、数字、下划线或连字符，且不能重复；名称不能为空")
		}
		if _, err := ParsePrivateKey(k.PrivateKey); err != nil {
			return fmt.Errorf("密钥 %q: %w", k.Name, err)
		}
		keys[k.ID] = true
	}
	hosts := map[string]bool{}
	for _, h := range c.Hosts {
		if !validID(h.ID) || hosts[h.ID] || strings.TrimSpace(h.Name) == "" {
			return errors.New("主机 ID 必须为 1–64 位字母、数字、下划线或连字符，且不能重复；名称不能为空")
		}
		hosts[h.ID] = true
		if h.Address == "" || strings.ContainsAny(h.Address, " /\t\r\n") || strings.TrimSpace(h.Username) == "" || h.Port < 1 || h.Port > 65535 {
			return fmt.Errorf("主机 %q: 地址、用户名或端口无效", h.Name)
		}
		switch h.Auth.Mode {
		case "key":
			if !keys[h.Auth.KeyID] {
				return fmt.Errorf("主机 %q 引用了不存在的密钥", h.Name)
			}
		case "password_prompt":
			if h.Auth.KeyID != "" {
				return errors.New("密码认证不能同时指定 key_id")
			}
		default:
			return errors.New("认证模式必须为 key 或 password_prompt")
		}
		if h.TrustedHostKey != "" {
			_, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(h.TrustedHostKey))
			if err != nil || len(bytes.TrimSpace(rest)) != 0 {
				return fmt.Errorf("主机 %q 的 trusted_host_key 无效", h.Name)
			}
		}
	}
	return nil
}

func validID(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func decode(b []byte) (Config, error) {
	var c Config
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	if err := d.Decode(&c); err != nil {
		return c, errors.New("YAML 解析失败：请检查语法、字段名称和字段类型")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return c, errors.New("仅支持一个 YAML 文档")
	}
	if c.Keys == nil {
		c.Keys = []Key{}
	}
	if c.Hosts == nil {
		c.Hosts = []Host{}
	}
	return c, Validate(c)
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func read(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxSize {
		return nil, errors.New("配置文件超过 4 MiB 限制")
	}
	return b, nil
}

type Store struct {
	mu        sync.Mutex
	path      string
	c         Config
	revision  string
	loadError string
}

// Credentials returns the last validated credentials without reading the file on every keystroke.
func (s *Store) Credentials() Credentials {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.c.Web
}

func Open(path string) (*Store, error) {
	b, err := read(path)
	if err != nil {
		return nil, err
	}
	c, err := decode(b)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, c: c, revision: digest(b)}, nil
}
func clone(c Config) Config {
	c.Keys = append([]Key{}, c.Keys...)
	c.Hosts = append([]Host{}, c.Hosts...)
	return c
}
func (s *Store) refreshLocked() {
	b, err := read(s.path)
	if err != nil {
		s.loadError = "配置文件读取失败；继续使用最近一次有效配置"
		return
	}
	if digest(b) == s.revision {
		s.loadError = ""
		return
	}
	c, err := decode(b)
	if err != nil {
		s.loadError = err.Error()
		return
	}
	s.c = c
	s.revision = digest(b)
	s.loadError = ""
}
func (s *Store) Snapshot() (Config, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	return clone(s.c), s.revision, s.loadError
}
func (s *Store) Watch(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			s.refreshLocked()
			s.mu.Unlock()
		}
	}
}
func (s *Store) Update(expected string, mutate func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	if expected == "" || expected != s.revision || s.loadError != "" {
		return ErrConflict
	}
	c := clone(s.c)
	if err := mutate(&c); err != nil {
		return err
	}
	if err := Validate(c); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if len(b) > MaxSize {
		return errors.New("配置超过 4 MiB 限制")
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".wotm-*.yaml")
	if err != nil {
		return errors.New("无法创建配置临时文件")
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return errors.New("配置写入失败")
	}
	// Check disk again immediately before replacement. External writers must not run concurrently.
	current, err := read(s.path)
	if err != nil || digest(current) != expected {
		return ErrConflict
	}
	if err = os.Rename(tmp, s.path); err != nil {
		return errors.New("无法替换配置文件，请检查目录权限，并使用目录挂载")
	}
	s.c = c
	s.revision = digest(b)
	s.loadError = ""
	return nil
}

func Initialize(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	c := Config{Version: 1, Web: Credentials{Username: "admin", Password: hex.EncodeToString(raw)}, Keys: []Key{}, Hosts: []Host{}}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
