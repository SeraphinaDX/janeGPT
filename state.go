package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"time"
)

const (
	stateVersion      = 1
	stateFilename     = "state.json"
	statusRetry       = "retry"
	statusDelivered   = "delivered"
	statusQuarantined = "quarantined"
	maxRetryBackoff   = 24 * time.Hour
)

// messageState records the outcome of processing attempts, independently of
// the message’s current Maildir filename or location.
type messageState struct {
	Status      string    `json:"status"`
	Attempts    int       `json:"attempts"`
	NextAttempt time.Time `json:"next_attempt,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// stateData is the versioned on-disk format; incompatible versions stop startup
// rather than silently discarding duplicate-delivery protection.
type stateData struct {
	Version  int                     `json:"version"`
	Messages map[string]messageState `json:"messages"`
}

// deliveryLedger owns one state file. It has no interprocess lock: only one
// bot process may use a state directory at a time.
type deliveryLedger struct {
	path string
	data stateData
}

// openDeliveryLedger starts empty only when the file is absent. Corrupt or
// unsupported state is an error because ignoring it could resend delivered mail.
func openDeliveryLedger(dir string) (*deliveryLedger, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	ledger := &deliveryLedger{
		path: filepath.Join(dir, stateFilename),
		data: stateData{Version: stateVersion, Messages: make(map[string]messageState)},
	}
	f, err := os.Open(ledger.path)
	if errors.Is(err, os.ErrNotExist) {
		return ledger, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open delivery state: %w", err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ledger.data); err != nil {
		return nil, fmt.Errorf("decode delivery state: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return nil, fmt.Errorf("decode delivery state: %w", err)
	}
	if ledger.data.Version != stateVersion {
		return nil, fmt.Errorf("unsupported delivery state version %d", ledger.data.Version)
	}
	if ledger.data.Messages == nil {
		return nil, errors.New("decode delivery state: messages must not be null")
	}
	for key, record := range ledger.data.Messages {
		if key == "" || record.Attempts <= 0 || record.UpdatedAt.IsZero() {
			return nil, fmt.Errorf("decode delivery state: invalid record %q", key)
		}
		switch record.Status {
		case statusRetry:
			if record.Attempts == 0 || record.NextAttempt.IsZero() {
				return nil, fmt.Errorf("decode delivery state: invalid retry record %q", key)
			}
		case statusDelivered, statusQuarantined:
		default:
			return nil, fmt.Errorf("decode delivery state: invalid status %q for %q", record.Status, key)
		}
	}
	return ledger, nil
}

func (l *deliveryLedger) get(key string) (messageState, bool) {
	record, ok := l.data.Messages[key]
	return record, ok
}

// put persists a record before reporting success and restores the in-memory
// value if saving fails, keeping subsequent decisions consistent with disk.
func (l *deliveryLedger) put(key string, record messageState) error {
	previous, existed := l.data.Messages[key]
	l.data.Messages[key] = record
	if err := l.save(); err != nil {
		if existed {
			l.data.Messages[key] = previous
		} else {
			delete(l.data.Messages, key)
		}
		return err
	}
	return nil
}

// pruneCompleted expires only delivered records. Quarantine and pending retry
// records remain so old failed messages are not treated as new work.
func (l *deliveryLedger) pruneCompleted(before time.Time) error {
	removed := make(map[string]messageState)
	for key, record := range l.data.Messages {
		if record.Status == statusDelivered && record.UpdatedAt.Before(before) {
			removed[key] = record
			delete(l.data.Messages, key)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	if err := l.save(); err != nil {
		for key, record := range removed {
			l.data.Messages[key] = record
		}
		return err
	}
	return nil
}

// save writes and syncs a private temporary file in the same directory before
// renaming it over the ledger. Readers see a complete old or new JSON document.
func (l *deliveryLedger) save() error {
	dir := filepath.Dir(l.path)
	f, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary delivery state: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return fmt.Errorf("protect temporary delivery state: %w", err)
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(l.data); err != nil {
		f.Close()
		return fmt.Errorf("encode delivery state: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync delivery state: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close delivery state: %w", err)
	}
	if err := os.Rename(tmp, l.path); err != nil {
		return fmt.Errorf("replace delivery state: %w", err)
	}
	// Best-effort directory sync persists the rename on supporting systems.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// messageKey prefers the first usable Message-ID so Maildir renames do not
// change identity. Mail without one falls back to a hash of the entire file.
func messageKey(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if msg, err := mail.ReadMessage(f); err == nil {
		if ids := messageIDs(msg.Header.Get("Message-ID")); len(ids) > 0 {
			return "message-id:" + ids[0], nil
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

// retryDelay doubles the base delay after each failed attempt, checking before
// multiplication to avoid overflow and capping the result at one day.
func retryDelay(base time.Duration, attempts int) time.Duration {
	delay := base
	for i := 1; i < attempts && delay < maxRetryBackoff; i++ {
		if delay > maxRetryBackoff/2 {
			return maxRetryBackoff
		}
		delay *= 2
	}
	if delay > maxRetryBackoff {
		return maxRetryBackoff
	}
	return delay
}

// stateError bounds stored diagnostics so repeated failures cannot grow each
// ledger record without limit.
func stateError(err error) string {
	const maxBytes = 4096
	text := err.Error()
	if len(text) <= maxBytes {
		return text
	}
	return text[:maxBytes] + "..."
}
