package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
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
	case "docset":
		if err := docsetCommand([]string{"-root", args[1]}, os.Stdin, os.Stdout); err != nil {
			os.Exit(9)
		}
	case "tool":
		io.WriteString(os.Stdout, `{"context":"Useful tool result","attachments":[{"name":"reference.md","text":"# Reference"}]}`)
	case "overflow":
		io.WriteString(os.Stdout, strings.Repeat("x", 100000))
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

func testCycleConfig(t *testing.T, send commandArgs, ollamaURL string) config {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Maildir")
	if err := ensureMaildir(root); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.MaildirRoot = root
	cfg.ArchivePath = "Archive"
	cfg.FailedPath = "Failed"
	cfg.StateDir = filepath.Join(t.TempDir(), "state")
	cfg.AdminEmail = "admin@example.org"
	cfg.OllamaURL = ollamaURL
	cfg.SyncCommand = nil
	cfg.SendCommand = send
	cfg.Interval = 0
	cfg.MaxAttempts = 2
	cfg.RetryBackoff = time.Hour
	cfg.CompletedRetention = 24 * time.Hour
	if err := validateConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func writeTestMessage(t *testing.T, cfg config, name string) string {
	t.Helper()
	path := filepath.Join(cfg.MaildirRoot, "new", name)
	message := "From: Admin <admin@example.org>\r\nReply-To: admin@example.org\r\nMessage-ID: <durable@example.org>\r\nSubject: Durable test\r\n\r\nHello Jane\r\n"
	if err := os.WriteFile(path, []byte(message), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testOllamaServer(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"response":"Hi from Jane"}`)
	}))
}

func TestDeliveredMessageIsNotSentTwice(t *testing.T) {
	var calls atomic.Int32
	server := testOllamaServer(t, &calls)
	defer server.Close()
	helper, _ := helperConfig(t, "send")
	cfg := testCycleConfig(t, helper.SendCommand, server.URL)
	writeTestMessage(t, cfg, "first")
	if err := runCycle(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	writeTestMessage(t, cfg, "duplicate")
	if err := runCycle(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("Ollama calls: got %d, want 1", calls.Load())
	}
	entries, err := os.ReadDir(filepath.Join(cfg.ArchivePath, "cur"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("archived messages: got %d, want 2", len(entries))
	}
}

func TestDeliveryRecordPreventsResendAfterArchiveFailure(t *testing.T) {
	var calls atomic.Int32
	server := testOllamaServer(t, &calls)
	defer server.Close()
	helper, _ := helperConfig(t, "send")
	cfg := testCycleConfig(t, helper.SendCommand, server.URL)
	path := writeTestMessage(t, cfg, "archive-recovery")
	if err := os.RemoveAll(cfg.ArchivePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.ArchivePath, []byte("blocks archive directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runCycle(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("message should remain after archive failure:", err)
	}
	key, err := messageKey(path)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := openDeliveryLedger(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if record, ok := ledger.get(key); !ok || record.Status != statusDelivered {
		t.Fatalf("delivery was not recorded before archival: %+v, %v", record, ok)
	}
	if err := os.Remove(cfg.ArchivePath); err != nil {
		t.Fatal(err)
	}
	if err := ensureMaildir(cfg.ArchivePath); err != nil {
		t.Fatal(err)
	}
	if err := runCycle(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("Ollama calls: got %d, want 1", calls.Load())
	}
	if _, err := os.Stat(filepath.Join(cfg.ArchivePath, "cur", "archive-recovery")); err != nil {
		t.Fatal("delivered message was not recovered into archive:", err)
	}
}

func TestFailureBackoffAndQuarantine(t *testing.T) {
	var calls atomic.Int32
	server := testOllamaServer(t, &calls)
	defer server.Close()
	helper, _ := helperConfig(t, "fail")
	cfg := testCycleConfig(t, helper.SendCommand, server.URL)
	path := writeTestMessage(t, cfg, "failure")
	key, err := messageKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := runCycle(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := runCycle(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("backoff did not suppress retry; Ollama calls=%d", calls.Load())
	}
	ledger, err := openDeliveryLedger(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	record, ok := ledger.get(key)
	if !ok || record.Status != statusRetry || record.Attempts != 1 {
		t.Fatalf("retry record: %+v, %v", record, ok)
	}
	record.NextAttempt = time.Now().Add(-time.Minute)
	if err := ledger.put(key, record); err != nil {
		t.Fatal(err)
	}
	if err := runCycle(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("Ollama calls after due retry: got %d, want 2", calls.Load())
	}
	if _, err := os.Stat(filepath.Join(cfg.FailedPath, "cur", "failure")); err != nil {
		t.Fatal("message was not quarantined:", err)
	}
	ledger, err = openDeliveryLedger(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	record, ok = ledger.get(key)
	if !ok || record.Status != statusQuarantined || record.Attempts != 2 || record.LastError == "" {
		t.Fatalf("quarantine record: %+v, %v", record, ok)
	}
}

func TestAttachmentContextAndReportReachReply(t *testing.T) {
	var request ollamaRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		io.WriteString(w, `{"response":"Document summary"}`)
	}))
	defer server.Close()
	helper, capture := helperConfig(t, "send")
	cfg := testCycleConfig(t, helper.SendCommand, server.URL)
	message := "From: admin@example.org\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n" +
		"--b\r\nContent-Disposition: attachment; filename=notes.org\r\n\r\n* Useful notes\r\n" +
		"--b\r\nContent-Disposition: attachment; filename=photo.png\r\n\r\nimage\r\n--b--\r\n"
	if err := os.WriteFile(filepath.Join(cfg.MaildirRoot, "new", "attachments"), []byte(message), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runCycle(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(request.Prompt, "Useful notes") || !strings.Contains(request.System, attachmentSystem) {
		t.Fatalf("missing document context or system instruction: %+v", request)
	}
	if !strings.Contains(string(data), "photo.png") || strings.Contains(string(data), "Useful notes") {
		t.Fatalf("reply should report skipped file without quoting incoming document: %s", data)
	}
}
