package config

import (
	"errors"
	"strings"
	"testing"
)

func TestProxyValidationAndBackwardCompatibility(t *testing.T) {
	legacy := []byte("version: 1\nweb: {username: admin, password: test}\nkeys: []\nhosts: []\n")
	c, err := decode(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if c.Proxies == nil {
		t.Fatal("legacy proxies must normalize to empty list")
	}
	p := Proxy{ID: "office", Name: "Office", Address: "127.0.0.1", Port: 1080}
	c.Proxies = []Proxy{p}
	c.Hosts = []Host{{ID: "host", Name: "Host", Address: "private.invalid", Port: 22, Username: "root", ProxyID: "office", Auth: Auth{Mode: "password_prompt"}}}
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"dangling reference", func(c *Config) { c.Hosts[0].ProxyID = "missing" }},
		{"duplicate ID", func(c *Config) { c.Proxies = append(c.Proxies, c.Proxies[0]) }},
		{"invalid port", func(c *Config) { c.Proxies[0].Port = 0 }},
		{"proxy URL", func(c *Config) { c.Proxies[0].Address = "http://proxy.invalid" }},
		{"partial credentials", func(c *Config) { c.Proxies[0].Username = "only-user" }},
		{"oversize credentials", func(c *Config) { c.Proxies[0].Username = "user"; c.Proxies[0].Password = strings.Repeat("x", 256) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := clone(c)
			test.mutate(&candidate)
			if Validate(candidate) == nil {
				t.Fatal("invalid proxy accepted")
			}
		})
	}
	if err := Validate(c); err != nil {
		t.Fatal("validation test mutation modified original", err)
	}
}

func TestProxyMutationRollbackDoesNotAliasStore(t *testing.T) {
	s, _ := testStore(t)
	_, revision, _ := s.Snapshot()
	if err := s.Update(revision, func(c *Config) error {
		c.Proxies = []Proxy{{ID: "office", Name: "Office", Address: "localhost", Port: 1080}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, revision, _ = s.Snapshot()
	if err := s.Update(revision, func(c *Config) error { c.Proxies[0].Name = "uncommitted"; return errors.New("reject") }); err == nil {
		t.Fatal("mutation unexpectedly committed")
	}
	c, _, _ := s.Snapshot()
	if c.Proxies[0].Name != "Office" {
		t.Fatal("rejected update mutated stored proxy")
	}
}
