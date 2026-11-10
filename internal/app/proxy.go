package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
	"wotm/internal/config"
)

type proxyView struct {
	config.Proxy
	HasPassword bool `json:"has_password"`
}

func (a *App) saveProxy(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     string  `json:"name"`
		Address  string  `json:"address"`
		Port     int     `json:"port"`
		Username string  `json:"username"`
		Password *string `json:"password"`
	}
	if !body(w, r, &input) {
		return
	}
	a.update(w, r, func(c *config.Config) error {
		p := config.Proxy{Name: strings.TrimSpace(input.Name), Address: strings.TrimSpace(input.Address), Port: input.Port, Username: input.Username}
		if r.Method == "POST" {
			p.ID = token()[:16]
			if input.Password != nil {
				p.Password = *input.Password
			}
			c.Proxies = append(c.Proxies, p)
			return nil
		}
		for i, old := range c.Proxies {
			if old.ID == r.PathValue("id") {
				p.ID = old.ID
				p.Password = old.Password
				if input.Password != nil {
					p.Password = *input.Password
				}
				c.Proxies[i] = p
				return nil
			}
		}
		return errors.New("代理不存在")
	})
}
func (a *App) deleteProxy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.update(w, r, func(c *config.Config) error {
		for _, h := range c.Hosts {
			if h.ProxyID == id {
				return errors.New("代理正在被主机使用，请先将主机改为直连或选择其他代理")
			}
		}
		for i, p := range c.Proxies {
			if p.ID == id {
				c.Proxies = append(c.Proxies[:i], c.Proxies[i+1:]...)
				return nil
			}
		}
		return errors.New("代理不存在")
	})
}

// Only an explicitly selected proxy is used; a failed proxy never falls back to a direct connection.
// Passing the original hostname to SOCKS5 lets the proxy resolve remote-only DNS names.
func dialHost(ctx context.Context, c config.Config, host config.Host) (net.Conn, error) {
	direct := &net.Dialer{Timeout: 10 * time.Second}
	address := net.JoinHostPort(host.Address, strconv.Itoa(host.Port))
	if host.ProxyID == "" {
		return direct.DialContext(ctx, "tcp", address)
	}
	for _, p := range c.Proxies {
		if p.ID != host.ProxyID {
			continue
		}
		var auth *proxy.Auth
		if p.Username != "" {
			auth = &proxy.Auth{User: p.Username, Password: p.Password}
		}
		dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort(p.Address, strconv.Itoa(p.Port)), auth, direct)
		if err != nil {
			return nil, errors.New("SOCKS5 代理配置无效")
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, errors.New("SOCKS5 代理不支持连接超时")
		}
		return contextDialer.DialContext(ctx, "tcp", address)
	}
	return nil, errors.New("SOCKS5 代理不存在")
}
