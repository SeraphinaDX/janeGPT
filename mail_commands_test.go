package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Run the test binary as an external mail program, without requiring mail tools.
func TestMailCommandHelper(t *testing.T) {
	if os.Getenv("JANEGPT_COMMAND_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	args = args[1:]
	switch args[0] {
	case "fail":
		os.Exit(7)
	case "wait":
		time.Sleep(time.Minute)
	case "receive":
		data, _ := json.Marshal(args[1:])
		if err := os.WriteFile(os.Getenv("JANEGPT_CAPTURE"), data, 0600); err != nil {
			os.Exit(8)
		}
	case "send":
		data, _ := io.ReadAll(os.Stdin)
		if err := os.WriteFile(os.Getenv("JANEGPT_CAPTURE"), data, 0600); err != nil {
			os.Exit(8)
		}
	}
	os.Exit(0)
}

func helperConfig(t *testing.T, mode string) (config, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "captured")
	t.Setenv("JANEGPT_COMMAND_HELPER", "1")
	t.Setenv("JANEGPT_CAPTURE", capture)
	args := commandArgs{"-test.run=^TestMailCommandHelper$", "--", mode}
	return config{SyncCommand: executable, SyncArgs: args, SendCommand: executable, SendArgs: args, CommandTimeout: 5 * time.Second}, capture
}

func TestReceiveCommandArgumentsAndCompletion(t *testing.T) {
	cfg, capture := helperConfig(t, "receive")
	want := []string{"sync", "-config=/a folder/config.toml", "$(touch unwanted)", "", "--plain"}
	cfg.SyncArgs = append(cfg.SyncArgs, want...)
	if err := receiveMail(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal("command did not finish before receive returned:", err)
	}
	var got []string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments: got %q, want %q", got, want)
	}
}

func TestReceiveFailureStopsCycle(t *testing.T) {
	cfg, _ := helperConfig(t, "fail")
	cfg.MaildirRoot = filepath.Join(t.TempDir(), "does-not-exist")
	err := runCycle(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "mail receive command") || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("got %v", err)
	}
}

func TestMailCommandTimeoutAndCancellation(t *testing.T) {
	cfg, _ := helperConfig(t, "wait")
	cfg.CommandTimeout = 100 * time.Millisecond
	if err := receiveMail(context.Background(), cfg); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := receiveMail(ctx, cfg); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestNoSync(t *testing.T) {
	if err := receiveMail(context.Background(), config{NoSync: true, OfflineIMAP: "missing-command"}); err != nil {
		t.Fatal(err)
	}
}

func TestSendCommandDefaultsAndRecipient(t *testing.T) {
	program, args := sendCommand(config{MSMTP: "custom-msmtp", MSMTPAccount: "personal"}, "a@example.org")
	if program != "custom-msmtp" || !reflect.DeepEqual(args, []string{"-a", "personal", "--", "a@example.org"}) {
		t.Fatalf("%s %q", program, args)
	}
	cfg := config{SendCommand: "sender", SendArgs: commandArgs{"send", "{recipient}"}, MSMTPAccount: "ignored"}
	program, args = sendCommand(cfg, "b@example.org")
	if program != "sender" || !reflect.DeepEqual(args, []string{"send", "b@example.org"}) {
		t.Fatalf("%s %q", program, args)
	}
	if cfg.SendArgs[1] != "{recipient}" {
		t.Fatal("configuration mutated")
	}
}

func TestCustomSenderReceivesMIME(t *testing.T) {
	cfg, capture := helperConfig(t, "send")
	cfg.From, cfg.Subject = "bot@example.org", "Reply"
	if err := sendMail(context.Background(), cfg, "a@example.org", "Model answer", nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(msg.Body)
	if msg.Header.Get("To") != "a@example.org" || msg.Header.Get("From") != cfg.From || strings.TrimSpace(string(body)) != "Model answer" {
		t.Fatalf("unexpected message: %s", data)
	}
}

func TestValidateMailCommands(t *testing.T) {
	base := config{OfflineIMAP: "offlineimap", MSMTP: "msmtp", CommandTimeout: time.Minute}
	if err := validateMailCommands(&base); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*config){
		func(c *config) { c.CommandTimeout = 0 },
		func(c *config) { c.SyncArgs = commandArgs{"sync"} },
		func(c *config) { c.SendArgs = commandArgs{"send"} },
		func(c *config) { c.NoSync = true; c.SyncCommand = "sync" },
		func(c *config) { c.OfflineIMAP = "" },
		func(c *config) { c.MSMTP = "" },
	} {
		cfg := base
		mutate(&cfg)
		if err := validateMailCommands(&cfg); err == nil {
			t.Fatalf("accepted invalid config: %+v", cfg)
		}
	}
}
