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
	if cfg.CommandTimeout <= 0 {
		return errors.New("command_timeout must be positive")
	}
	if len(cfg.SyncCommand) > 0 && strings.TrimSpace(cfg.SyncCommand[0]) == "" {
		return errors.New("sync_command executable must not be empty")
	}
	if len(cfg.SendCommand) == 0 || strings.TrimSpace(cfg.SendCommand[0]) == "" {
		return errors.New("send_command must contain an executable")
	}
	return nil
}

func receiveMail(ctx context.Context, cfg config) error {
	if len(cfg.SyncCommand) == 0 {
		status(cCyan, "SYNC", "scanning existing Maildir; receive command disabled")
		return ctx.Err()
	}
	program, args := cfg.SyncCommand[0], cfg.SyncCommand[1:]
	status(cBlue, "SYNC", "running %s; waiting for mail receive to finish", program)
	if err := runMailCommand(ctx, cfg.CommandTimeout, program, nil, args...); err != nil {
		return fmt.Errorf("mail receive command %q failed: %w", program, err)
	}
	status(cGreen, "SYNC", "mail receive completed")
	return nil
}

func sendCommand(cfg config, recipients []string) (string, []string) {
	args := make([]string, 0, len(cfg.SendCommand)-1+len(recipients))
	for _, arg := range cfg.SendCommand[1:] {
		// Only a whole argument is a placeholder; no shell expansion.
		if arg == "{recipient}" {
			args = append(args, recipients...)
			continue
		}
		args = append(args, arg)
	}
	return cfg.SendCommand[0], args
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
