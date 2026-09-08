package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Arguments are separate argv entries: no shell parsing or interpolation occurs.
type commandArgs []string

func (a *commandArgs) String() string { return fmt.Sprint([]string(*a)) }
func (a *commandArgs) Set(value string) error {
	*a = append(*a, value)
	return nil
}

func validateMailCommands(cfg *config) error {
	if (cfg.SyncCommand != "" && strings.TrimSpace(cfg.SyncCommand) == "") || (cfg.SendCommand != "" && strings.TrimSpace(cfg.SendCommand) == "") {
		return errors.New("custom mail executable must not be whitespace")
	}
	if cfg.CommandTimeout <= 0 {
		return errors.New("-command-timeout must be positive")
	}
	if len(cfg.SyncArgs) > 0 && cfg.SyncCommand == "" {
		return errors.New("-sync-arg requires -sync-command (or MAILBOT_SYNC_COMMAND)")
	}
	if len(cfg.SendArgs) > 0 && cfg.SendCommand == "" {
		return errors.New("-send-arg requires -send-command (or MAILBOT_SEND_COMMAND)")
	}
	if cfg.NoSync && (cfg.SyncCommand != "" || len(cfg.SyncArgs) > 0) {
		return errors.New("-no-sync cannot be combined with a custom sync command")
	}
	if !cfg.NoSync && strings.TrimSpace(cfg.SyncCommand) == "" && strings.TrimSpace(cfg.OfflineIMAP) == "" {
		return errors.New("mail receive executable must not be empty")
	}
	if strings.TrimSpace(cfg.SendCommand) == "" && strings.TrimSpace(cfg.MSMTP) == "" {
		return errors.New("mail send executable must not be empty")
	}
	return nil
}

func receiveMail(ctx context.Context, cfg config) error {
	if cfg.NoSync {
		status(cCyan, "SYNC", "scanning existing Maildir; receive command disabled")
		return ctx.Err()
	}
	program, args := cfg.SyncCommand, []string(cfg.SyncArgs)
	if program == "" {
		program = cfg.OfflineIMAP
	}
	status(cBlue, "SYNC", "running %s; waiting for mail receive to finish", program)
	if err := runMailCommand(ctx, cfg.CommandTimeout, program, nil, args...); err != nil {
		return fmt.Errorf("mail receive command %q failed: %w", program, err)
	}
	status(cGreen, "SYNC", "mail receive completed")
	return nil
}

func sendCommand(cfg config, recipient string) (string, []string) {
	if cfg.SendCommand != "" {
		args := append([]string(nil), cfg.SendArgs...)
		for i, arg := range args {
			// Only a whole argument is a placeholder; no shell expansion.
			if arg == "{recipient}" {
				args[i] = recipient
			}
		}
		return cfg.SendCommand, args
	}
	args := []string{}
	if cfg.MSMTPAccount != "" {
		args = append(args, "-a", cfg.MSMTPAccount)
	}
	return cfg.MSMTP, append(args, "--", recipient)
}

func runMailCommand(ctx context.Context, timeout time.Duration, program string, stdin []byte, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := runCommand(ctx, program, stdin, args...)
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", program, ctx.Err())
	}
	return err
}
