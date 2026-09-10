package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func configFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.toml")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTOMLConfigAndOverrides(t *testing.T) {
	t.Setenv("MAILBOT_CONFIG", "")
	t.Setenv("OLLAMA_MODEL", "environment-model")
	p := configFile(t, `maildir = "~/Maildir"
admin = "you@example.org"
model = "file-model"
interval = "0s"
page_timeout = "2s"
command_timeout = "3m"
failed = "Failed-mail"
state_dir = "~/state/janeGPT"
max_attempts = 7
retry_backoff = "7m"
completed_retention = "168h"
reply_anyone = true
personality = """First line.
Second line."""
sync_command = ["receiver", "sync", "a path with spaces"]
send_command = ["sender", "{recipient}"]
`)
	cfg, err := loadConfig([]string{"-config=" + p}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "file-model" || cfg.Interval != 0 || cfg.PageTimeout != 2*time.Second || cfg.CommandTimeout != 3*time.Minute || cfg.MaxAttempts != 7 || cfg.RetryBackoff != 7*time.Minute || cfg.CompletedRetention != 168*time.Hour || !cfg.ReplyAnyone || !strings.Contains(cfg.Personality, "\n") {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if !filepath.IsAbs(cfg.MaildirRoot) || !filepath.IsAbs(cfg.StateDir) || cfg.FailedPath != "Failed-mail" || !reflect.DeepEqual(cfg.SyncCommand, commandArgs{"receiver", "sync", "a path with spaces"}) {
		t.Fatalf("paths/args: %+v", cfg)
	}
	cfg, err = loadConfig([]string{"-model=cli-model", "-config", p, "-reply-anyone=false", "-sync-command=new", "-sync-command=", "-send-command=other", "-send-command=-t"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "cli-model" || cfg.ReplyAnyone || !reflect.DeepEqual(cfg.SyncCommand, commandArgs{"new", ""}) || !reflect.DeepEqual(cfg.SendCommand, commandArgs{"other", "-t"}) {
		t.Fatalf("overrides: %+v", cfg)
	}
}

func TestTOMLConfigErrors(t *testing.T) {
	for _, body := range []string{`modle = "typo"`, `interval = "later"`, `sync_command = "sync"`, `reply_anyone = "yes"`, `model = "a"
model = "b"`, `model = [`} {
		p := configFile(t, body)
		if _, err := loadConfig([]string{"-config=" + p}, io.Discard); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
	for _, args := range [][]string{{"-config="}, {"-config=/no/such/file.toml"}, {"-unknown"}, {"unexpected"}} {
		if _, err := loadConfig(args, io.Discard); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
	if _, err := loadConfig([]string{"-help", "-config=/missing"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help: %v", err)
	}
}

func TestExampleConfig(t *testing.T) {
	cfg, err := loadConfig([]string{"-config=janegpt.example.toml"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaildirRoot = t.TempDir()
	cfg.StateDir = t.TempDir()
	if err := validateConfig(&cfg); err != nil {
		t.Fatal(err)
	}
}

func TestHomeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{"~": home, "~/Maildir": filepath.Join(home, "Maildir"), "relative": "relative", "$HOME/file": "$HOME/file"} {
		got, err := expandHome(in)
		if err != nil || got != want {
			t.Fatalf("%q: %q, %v", in, got, err)
		}
	}
}

func TestConfigDiscoveryAndEnvironment(t *testing.T) {
	// No parallel tests: the current directory is process-wide.
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	t.Setenv("MAILBOT_CONFIG", "")
	t.Setenv("OLLAMA_MODEL", "environment-model")
	cfg, err := loadConfig(nil, io.Discard)
	if err != nil || cfg.Model != "environment-model" {
		t.Fatalf("legacy defaults: %+v %v", cfg, err)
	}
	if err := os.WriteFile("janegpt.toml", []byte(`model = "discovered"`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfig(nil, io.Discard)
	if err != nil || cfg.Model != "discovered" {
		t.Fatalf("discovery: %+v %v", cfg, err)
	}
	p := configFile(t, `model = "explicit"`)
	t.Setenv("MAILBOT_CONFIG", p)
	cfg, err = loadConfig(nil, io.Discard)
	if err != nil || cfg.Model != "explicit" {
		t.Fatalf("environment config path: %+v %v", cfg, err)
	}
	cfg, err = loadConfig([]string{"-config=janegpt.toml"}, io.Discard)
	if err != nil || cfg.Model != "discovered" {
		t.Fatalf("explicit path precedence: %+v %v", cfg, err)
	}
}

func TestValidateRetryConfig(t *testing.T) {
	valid := func() config {
		cfg := defaultConfig()
		cfg.MaildirRoot = filepath.Join(t.TempDir(), "Maildir")
		cfg.ArchivePath = "Archive"
		cfg.FailedPath = "Failed"
		cfg.StateDir = filepath.Join(t.TempDir(), "state")
		cfg.AdminEmail = "admin@example.org"
		cfg.SendCommand = commandArgs{"sender"}
		return cfg
	}
	for name, mutate := range map[string]func(*config){
		"empty failed":   func(c *config) { c.FailedPath = "" },
		"same maildir":   func(c *config) { c.FailedPath = c.ArchivePath },
		"empty state":    func(c *config) { c.StateDir = "" },
		"zero attempts":  func(c *config) { c.MaxAttempts = 0 },
		"zero backoff":   func(c *config) { c.RetryBackoff = 0 },
		"zero retention": func(c *config) { c.CompletedRetention = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := valid()
			mutate(&cfg)
			if err := validateConfig(&cfg); err == nil {
				t.Fatalf("accepted invalid config: %+v", cfg)
			}
		})
	}
}
