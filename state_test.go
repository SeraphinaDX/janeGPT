package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMessageKeyPrefersMessageID(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	if err := os.WriteFile(first, []byte("From: a@example.org\r\nMessage-ID: <same@example.org>\r\n\r\none"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("From: a@example.org\r\nMessage-ID: <same@example.org>\r\n\r\ntwo"), 0600); err != nil {
		t.Fatal(err)
	}
	firstKey, err := messageKey(first)
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := messageKey(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstKey != "message-id:<same@example.org>" || secondKey != firstKey {
		t.Fatalf("keys: %q and %q", firstKey, secondKey)
	}
}

func TestMessageKeyFallsBackToContentHash(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	if err := os.WriteFile(first, []byte("From: a@example.org\r\n\r\none"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("From: a@example.org\r\n\r\ntwo"), 0600); err != nil {
		t.Fatal(err)
	}
	firstKey, err := messageKey(first)
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := messageKey(second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(firstKey, "sha256:") || firstKey == secondKey {
		t.Fatalf("keys: %q and %q", firstKey, secondKey)
	}
}

func TestDeliveryLedgerRoundTripAndPrune(t *testing.T) {
	dir := t.TempDir()
	ledger, err := openDeliveryLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour).UTC()
	if err := ledger.put("old", messageState{Status: statusDelivered, Attempts: 1, UpdatedAt: old}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.put("retry", messageState{Status: statusRetry, Attempts: 2, NextAttempt: old, LastError: "boom", UpdatedAt: old}); err != nil {
		t.Fatal(err)
	}
	ledger, err = openDeliveryLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	if record, ok := ledger.get("retry"); !ok || record.Attempts != 2 || record.LastError != "boom" {
		t.Fatalf("record: %+v, %v", record, ok)
	}
	if err := ledger.pruneCompleted(time.Now().Add(-24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.get("old"); ok {
		t.Fatal("old delivered record was not pruned")
	}
	if _, ok := ledger.get("retry"); !ok {
		t.Fatal("retry record was pruned")
	}
	info, err := os.Stat(filepath.Join(dir, stateFilename))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("state mode: %o", info.Mode().Perm())
	}
}

func TestDeliveryLedgerRejectsCorruption(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, stateFilename), []byte(`{"version":1,"messages":{},"surprise":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openDeliveryLedger(dir); err == nil {
		t.Fatal("corrupt state was accepted")
	}
}

func TestRetryDelay(t *testing.T) {
	base := 5 * time.Minute
	for attempts, want := range map[int]time.Duration{1: 5 * time.Minute, 2: 10 * time.Minute, 3: 20 * time.Minute, 20: 24 * time.Hour} {
		if got := retryDelay(base, attempts); got != want {
			t.Fatalf("attempt %d: got %s, want %s", attempts, got, want)
		}
	}
}

func TestFindMessagesExcludesArchiveAndFailed(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "Inbox")
	archive := filepath.Join(root, "Archive")
	failed := filepath.Join(root, "Failed")
	for _, dir := range []string{inbox, archive, failed} {
		if err := ensureMaildir(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "new", filepath.Base(dir)), []byte("message"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := findMessages(root, archive, failed)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0] != filepath.Join(inbox, "new", "Inbox") {
		t.Fatalf("messages: %q", messages)
	}
}
