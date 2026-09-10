package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
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
	command := append(commandArgs{executable}, args...)
	return config{SyncCommand: command, SendCommand: command, CommandTimeout: 5 * time.Second}, capture
}

func TestReceiveCommandArgumentsAndCompletion(t *testing.T) {
	cfg, capture := helperConfig(t, "receive")
	want := []string{"sync", "-config=/a folder/config.toml", "$(touch unwanted)", "", "--plain"}
	cfg.SyncCommand = append(cfg.SyncCommand, want...)
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
	if err := receiveMail(context.Background(), config{}); err != nil {
		t.Fatal(err)
	}
}

func TestSendCommandRecipient(t *testing.T) {
	cfg := config{SendCommand: commandArgs{"sender", "send", "{recipient}"}}
	program, args := sendCommand(cfg, []string{"b@example.org", "c@example.org"})
	if program != "sender" || !reflect.DeepEqual(args, []string{"send", "b@example.org", "c@example.org"}) {
		t.Fatalf("%s %q", program, args)
	}
	if cfg.SendCommand[2] != "{recipient}" {
		t.Fatal("configuration mutated")
	}
}

func TestCustomSenderReceivesMIME(t *testing.T) {
	cfg, capture := helperConfig(t, "send")
	cfg.From = "bot@example.org"
	reply := replyHeaders{Subject: "Re: Café", InReplyTo: "<parent@example.org>", References: "<root@example.org> <parent@example.org>"}
	if err := sendMail(context.Background(), cfg, []string{"a@example.org", "b@example.org"}, reply, "Model answer", nil); err != nil {
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
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("To") != "a@example.org, b@example.org" || msg.Header.Get("From") != cfg.From || subject != reply.Subject || msg.Header.Get("In-Reply-To") != reply.InReplyTo || msg.Header.Get("References") != reply.References || strings.TrimSpace(string(body)) != "Model answer" {
		t.Fatalf("unexpected message: %s", data)
	}
}

func TestValidateMailCommands(t *testing.T) {
	base := config{SendCommand: commandArgs{"sender"}, CommandTimeout: time.Minute}
	if err := validateMailCommands(&base); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*config){
		func(c *config) { c.CommandTimeout = 0 },
		func(c *config) { c.SyncCommand = commandArgs{""} },
		func(c *config) { c.SendCommand = nil },
		func(c *config) { c.SendCommand = commandArgs{"  "} },
	} {
		cfg := base
		mutate(&cfg)
		if err := validateMailCommands(&cfg); err == nil {
			t.Fatalf("accepted invalid config: %+v", cfg)
		}
	}
}
