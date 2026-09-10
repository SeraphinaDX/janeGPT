package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

func defaultConfig() config {
	return config{
		MaildirRoot:    envOr("MAILBOT_MAILDIR", ""),
		ArchivePath:    envOr("MAILBOT_ARCHIVE", "Archive"),
		AdminEmail:     envOr("MAILBOT_ADMIN", ""),
		ReplyAnyone:    false,
		Model:          envOr("OLLAMA_MODEL", "llama3.2"),
		Personality:    "",
		OllamaURL:      envOr("OLLAMA_URL", "http://127.0.0.1:11434"),
		Interval:       envDuration("MAILBOT_INTERVAL", time.Minute),
		From:           envOr("MAILBOT_FROM", ""),
		Subject:        envOr("MAILBOT_SUBJECT", "Ollama response"),
		MaxMessageSize: envInt64("MAILBOT_MAX_MESSAGE_BYTES", 10<<20),
		MaxBodySize:    envInt64("MAILBOT_MAX_BODY_BYTES", 2<<20),
		PageTimeout:    envDuration("MAILBOT_PAGE_TIMEOUT", 30*time.Second),
		MaxPageSize:    envInt64("MAILBOT_MAX_PAGE_BYTES", 10<<20),
		MaxWebContext:  envInt64("MAILBOT_MAX_WEB_CONTEXT_BYTES", 128<<10),
		CommandTimeout: 10 * time.Minute,
	}
}

func configFlags(cfg *config, configPath *string, output io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("janeGPT", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(configPath, "config", *configPath, "TOML configuration file (default: janegpt.toml if present)")
	fs.StringVar(&cfg.MaildirRoot, "maildir", cfg.MaildirRoot, "Maildir root")
	fs.StringVar(&cfg.ArchivePath, "archive", cfg.ArchivePath, "Archive Maildir path, relative to -maildir unless absolute")
	fs.StringVar(&cfg.AdminEmail, "admin", cfg.AdminEmail, "only accepted sender address unless -reply-anyone is enabled")
	fs.BoolVar(&cfg.ReplyAnyone, "reply-anyone", cfg.ReplyAnyone, "reply to any sender instead of only the configured admin")
	fs.StringVar(&cfg.Model, "model", cfg.Model, "Ollama model")
	fs.StringVar(&cfg.Personality, "personality", cfg.Personality, "system/personality prompt sent to Ollama")
	fs.StringVar(&cfg.OllamaURL, "ollama-url", cfg.OllamaURL, "Ollama base URL")
	fs.DurationVar(&cfg.Interval, "interval", cfg.Interval, "scan interval; 0 means run once")
	fs.StringVar(&cfg.From, "from", cfg.From, "optional From header")
	fs.StringVar(&cfg.Subject, "subject", cfg.Subject, "static subject for replies")
	fs.Int64Var(&cfg.MaxMessageSize, "max-message-bytes", cfg.MaxMessageSize, "maximum incoming message file size")
	fs.Int64Var(&cfg.MaxBodySize, "max-body-bytes", cfg.MaxBodySize, "maximum decoded prompt body size")
	fs.DurationVar(&cfg.PageTimeout, "page-timeout", cfg.PageTimeout, "timeout for fetching each URL")
	fs.Int64Var(&cfg.MaxPageSize, "max-page-bytes", cfg.MaxPageSize, "maximum downloaded HTML page size")
	fs.Int64Var(&cfg.MaxWebContext, "max-web-context-bytes", cfg.MaxWebContext, "maximum Org-mode webpage text included in the Ollama prompt")
	fs.Var(&cfg.SyncCommand, "sync-command", "receive command element (repeat for executable and each argument)")
	fs.Var(&cfg.SendCommand, "send-command", "send command element (repeat); reads message on stdin; {recipient} expands in arguments")
	fs.DurationVar(&cfg.CommandTimeout, "command-timeout", cfg.CommandTimeout, "timeout for each mail receive/send command")
	return fs
}

// Precedence: built-in defaults < environment < TOML < explicit flags.
func loadConfig(args []string, output io.Writer) (config, error) {
	cfg := defaultConfig()
	configPath := os.Getenv("MAILBOT_CONFIG")
	fs := configFlags(&cfg, &configPath, output)
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	explicitConfig := configPath != ""
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			explicitConfig = true
		}
	})
	if explicitConfig && configPath == "" {
		return config{}, errors.New("-config must not be empty")
	}
	if !explicitConfig {
		if _, err := os.Stat("janegpt.toml"); err == nil {
			configPath = "janegpt.toml"
		} else if !errors.Is(err, os.ErrNotExist) {
			return config{}, err
		}
	}
	if configPath != "" {
		expanded, err := expandHome(configPath)
		if err != nil {
			return config{}, err
		}
		cfg = defaultConfig()
		metadata, err := toml.DecodeFile(expanded, &cfg)
		if err != nil {
			return config{}, fmt.Errorf("config %s: %w", configPath, err)
		}
		if unknown := metadata.Undecoded(); len(unknown) > 0 {
			return config{}, fmt.Errorf("config %s: unknown setting %s", configPath, unknown[0])
		}
		// CLI command elements replace the file command, rather than appending to it.
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "sync-command" {
				cfg.SyncCommand = nil
			}
			if f.Name == "send-command" {
				cfg.SendCommand = nil
			}
		})
		if err := configFlags(&cfg, &configPath, output).Parse(args); err != nil {
			return config{}, err
		}
	}
	for _, path := range []*string{&cfg.MaildirRoot, &cfg.ArchivePath} {
		expanded, err := expandHome(*path)
		if err != nil {
			return config{}, err
		}
		*path = expanded
	}
	for _, command := range []*commandArgs{&cfg.SyncCommand, &cfg.SendCommand} {
		if len(*command) == 0 {
			continue
		}
		expanded, err := expandHome((*command)[0])
		if err != nil {
			return config{}, err
		}
		(*command)[0] = expanded
	}
	return cfg, nil
}

func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand home directory: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}
