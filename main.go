package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"net/textproto"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	cReset  = "\x1b[0m"
	cRed    = "\x1b[31m"
	cGreen  = "\x1b[32m"
	cYellow = "\x1b[33m"
	cBlue   = "\x1b[34m"
	cCyan   = "\x1b[36m"
	cBold   = "\x1b[1m"
)

type config struct {
	MaildirRoot    string
	ArchivePath    string
	AdminEmail     string
	ReplyAnyone    bool
	Model          string
	Personality    string
	OllamaURL      string
	Interval       time.Duration
	OfflineIMAP    string
	MSMTP          string
	MSMTPAccount   string
	From           string
	Subject        string
	MaxMessageSize int64
	MaxBodySize    int64
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	System string `json:"system,omitempty"`
	Stream bool   `json:"stream"`
}

type ollamaResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

var (
	breakTagRE = regexp.MustCompile(`(?i)<\s*(br\s*/?|/p|/div|/li|/tr|/h[1-6])\s*>`)
	tagRE      = regexp.MustCompile(`(?s)<[^>]*>`)
	scriptRE   = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`)
	styleRE    = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style\s*>`)
	multiNLRE  = regexp.MustCompile(`\n{3,}`)
)

func main() {
	cfg := parseFlags()
	if err := validateConfig(&cfg); err != nil {
		status(cRed, "CONFIG", "%v", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	status(cCyan, "START", "mailbot using model %q; maildir=%s", cfg.Model, cfg.MaildirRoot)
	if cfg.Personality != "" {
		status(cCyan, "PERSONA", "custom personality enabled")
	}
	if cfg.ReplyAnyone {
		status(cYellow, "MODE", "reply-to-anyone is enabled")
	} else {
		status(cGreen, "MODE", "admin-only mode")
	}
	if cfg.Interval > 0 {
		status(cBlue, "LOOP", "checking mail every %s", cfg.Interval)
	} else {
		status(cBlue, "ONCE", "single scan mode")
	}

	for {
		if err := runCycle(ctx, cfg); err != nil && !errors.Is(err, context.Canceled) {
			status(cRed, "ERROR", "%v", err)
		}
		if cfg.Interval <= 0 || ctx.Err() != nil {
			break
		}

		timer := time.NewTimer(cfg.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}

	status(cYellow, "STOP", "mailbot stopped")
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.MaildirRoot, "maildir", envOr("MAILBOT_MAILDIR", ""), "Maildir root")
	flag.StringVar(&cfg.ArchivePath, "archive", envOr("MAILBOT_ARCHIVE", "Archive"), "Archive Maildir path, relative to -maildir unless absolute")
	flag.StringVar(&cfg.AdminEmail, "admin", envOr("MAILBOT_ADMIN", ""), "only accepted sender address unless -reply-anyone is enabled")
	flag.BoolVar(&cfg.ReplyAnyone, "reply-anyone", false, "reply to any sender instead of only the configured admin")
	flag.StringVar(&cfg.Model, "model", envOr("OLLAMA_MODEL", "llama3.2"), "Ollama model")
	flag.StringVar(&cfg.Personality, "personality", "", "system/personality prompt sent to Ollama")
	flag.StringVar(&cfg.OllamaURL, "ollama-url", envOr("OLLAMA_URL", "http://127.0.0.1:11434"), "Ollama base URL")
	flag.DurationVar(&cfg.Interval, "interval", envDuration("MAILBOT_INTERVAL", time.Minute), "scan interval; 0 means run once")
	flag.StringVar(&cfg.OfflineIMAP, "offlineimap", envOr("OFFLINEIMAP_BIN", "offlineimap"), "offlineimap executable")
	flag.StringVar(&cfg.MSMTP, "msmtp", envOr("MSMTP_BIN", "msmtp"), "msmtp executable")
	flag.StringVar(&cfg.MSMTPAccount, "msmtp-account", envOr("MSMTP_ACCOUNT", ""), "optional msmtp account name")
	flag.StringVar(&cfg.From, "from", envOr("MAILBOT_FROM", ""), "optional From header; msmtp config may add it instead")
	flag.StringVar(&cfg.Subject, "subject", envOr("MAILBOT_SUBJECT", "Ollama response"), "static subject for replies")
	flag.Int64Var(&cfg.MaxMessageSize, "max-message-bytes", envInt64("MAILBOT_MAX_MESSAGE_BYTES", 10<<20), "maximum incoming message file size")
	flag.Int64Var(&cfg.MaxBodySize, "max-body-bytes", envInt64("MAILBOT_MAX_BODY_BYTES", 2<<20), "maximum decoded prompt body size")
	flag.Parse()
	return cfg
}

func validateConfig(cfg *config) error {
	if cfg.MaildirRoot == "" {
		return errors.New("-maildir (or MAILBOT_MAILDIR) is required")
	}
	root, err := filepath.Abs(cfg.MaildirRoot)
	if err != nil {
		return err
	}
	cfg.MaildirRoot = filepath.Clean(root)

	if cfg.ArchivePath == "" {
		return errors.New("-archive must not be empty")
	}
	if !filepath.IsAbs(cfg.ArchivePath) {
		cfg.ArchivePath = filepath.Join(cfg.MaildirRoot, cfg.ArchivePath)
	}
	archive, err := filepath.Abs(cfg.ArchivePath)
	if err != nil {
		return err
	}
	cfg.ArchivePath = filepath.Clean(archive)

	if cfg.AdminEmail == "" {
		if !cfg.ReplyAnyone {
			return errors.New("-admin (or MAILBOT_ADMIN) is required unless -reply-anyone is enabled")
		}
	} else {
		admin, err := mail.ParseAddress(cfg.AdminEmail)
		if err != nil {
			return fmt.Errorf("invalid admin address: %w", err)
		}
		cfg.AdminEmail = strings.ToLower(admin.Address)
	}

	if cfg.From != "" {
		from, err := mail.ParseAddress(cfg.From)
		if err != nil {
			return fmt.Errorf("invalid -from address: %w", err)
		}
		cfg.From = from.Address
	}
	if strings.ContainsAny(cfg.Subject, "\r\n") {
		return errors.New("-subject must not contain CR or LF")
	}
	if cfg.Model == "" {
		return errors.New("-model must not be empty")
	}
	if cfg.MaxBodySize <= 0 || cfg.MaxMessageSize <= 0 {
		return errors.New("message/body size limits must be positive")
	}
	u, err := url.Parse(cfg.OllamaURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid -ollama-url %q", cfg.OllamaURL)
	}
	cfg.OllamaURL = strings.TrimRight(cfg.OllamaURL, "/")
	return ensureMaildir(cfg.ArchivePath)
}

func runCycle(ctx context.Context, cfg config) error {
	status(cBlue, "SYNC", "running %s", cfg.OfflineIMAP)
	if err := runCommand(ctx, cfg.OfflineIMAP, nil); err != nil {
		return fmt.Errorf("offlineimap failed: %w", err)
	}
	status(cGreen, "SYNC", "offlineimap completed")

	messages, err := findMessages(cfg.MaildirRoot, cfg.ArchivePath)
	if err != nil {
		return fmt.Errorf("scan maildir: %w", err)
	}
	if len(messages) == 0 {
		status(cCyan, "MAIL", "no messages found outside Archive")
		return nil
	}
	status(cCyan, "MAIL", "found %d message(s)", len(messages))

	client := &http.Client{Timeout: 10 * time.Minute}
	for _, path := range messages {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := processMessage(ctx, client, cfg, path); err != nil {
			status(cRed, "FAIL", "%s: %v", path, err)
		}
	}
	return nil
}

func processMessage(ctx context.Context, client *http.Client, cfg config, path string) error {
	status(cBlue, "READ", "%s", path)

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() > cfg.MaxMessageSize {
		return fmt.Errorf("message is %d bytes, over limit %d", info.Size(), cfg.MaxMessageSize)
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	msg, err := mail.ReadMessage(f)
	if err != nil {
		f.Close()
		return fmt.Errorf("parse message: %w", err)
	}

	fromHeader := msg.Header.Get("From")
	from, err := mail.ParseAddress(fromHeader)
	if err != nil {
		f.Close()
		return fmt.Errorf("invalid From header: %w", err)
	}

	if cfg.ReplyAnyone && cfg.From != "" && strings.EqualFold(from.Address, cfg.From) {
		f.Close()
		status(cYellow, "SKIP", "sender %q matches the bot From address; archiving to avoid a mail loop", from.Address)
		if err := archiveMessage(path, cfg.ArchivePath); err != nil {
			return fmt.Errorf("archive self-sent message: %w", err)
		}
		status(cGreen, "ARCHIVE", "%s", filepath.Base(path))
		return nil
	}

	if !cfg.ReplyAnyone && !strings.EqualFold(from.Address, cfg.AdminEmail) {
		f.Close()
		status(cYellow, "SKIP", "sender %q is not the configured admin; archiving without prompting Ollama", from.Address)
		if err := archiveMessage(path, cfg.ArchivePath); err != nil {
			return fmt.Errorf("archive unauthorized message: %w", err)
		}
		status(cGreen, "ARCHIVE", "%s", filepath.Base(path))
		return nil
	}

	recipient := cfg.AdminEmail
	if cfg.ReplyAnyone {
		recipient = from.Address
	}

	body, err := extractBody(textproto.MIMEHeader(msg.Header), msg.Body, cfg.MaxBodySize)
	f.Close()
	if err != nil {
		return fmt.Errorf("extract body: %w", err)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		status(cYellow, "SKIP", "message has an empty text body; archiving")
		if err := archiveMessage(path, cfg.ArchivePath); err != nil {
			return fmt.Errorf("archive empty message: %w", err)
		}
		return nil
	}

	status(cCyan, "OLLAMA", "prompting model %q with %d body bytes", cfg.Model, len(body))
	response, err := askOllama(ctx, client, cfg, body)
	if err != nil {
		return err
	}
	response = strings.TrimSpace(response)
	if response == "" {
		return errors.New("Ollama returned an empty response")
	}
	status(cGreen, "OLLAMA", "received %d response bytes", len(response))

	// Important: only the model response is placed in the outgoing message body.
	// The incoming prompt/body is never appended or quoted here.
	status(cBlue, "SEND", "sending model response to %s using msmtp", recipient)
	if err := sendWithMSMTP(ctx, cfg, recipient, response); err != nil {
		return fmt.Errorf("msmtp failed: %w", err)
	}
	status(cGreen, "SEND", "response sent")

	// Archive only after successful delivery, so temporary Ollama/msmtp failures can retry.
	if err := archiveMessage(path, cfg.ArchivePath); err != nil {
		return fmt.Errorf("archive processed message: %w", err)
	}
	status(cGreen, "ARCHIVE", "%s", filepath.Base(path))
	return nil
}

func askOllama(ctx context.Context, client *http.Client, cfg config, prompt string) (string, error) {
	payload, err := json.Marshal(ollamaRequest{Model: cfg.Model, Prompt: prompt, System: cfg.Personality, Stream: false})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.OllamaURL+"/api/generate", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call Ollama: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Ollama HTTP %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}

	var out ollamaResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("decode Ollama response: %w", err)
	}
	if out.Error != "" {
		return "", errors.New(out.Error)
	}
	return out.Response, nil
}

func sendWithMSMTP(ctx context.Context, cfg config, recipient, modelResponse string) error {
	var msg bytes.Buffer
	msg.WriteString("To: " + recipient + "\r\n")
	if cfg.From != "" {
		msg.WriteString("From: " + cfg.From + "\r\n")
	}
	msg.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	msg.WriteString("Subject: " + cfg.Subject + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(modelResponse)
	if !strings.HasSuffix(modelResponse, "\n") {
		msg.WriteString("\r\n")
	}

	args := make([]string, 0, 4)
	if cfg.MSMTPAccount != "" {
		args = append(args, "-a", cfg.MSMTPAccount)
	}
	args = append(args, "--", recipient)
	return runCommand(ctx, cfg.MSMTP, msg.Bytes(), args...)
}

func runCommand(ctx context.Context, program string, stdin []byte, args ...string) error {
	cmd := exec.CommandContext(ctx, program, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(output.String())
		if text != "" {
			return fmt.Errorf("%w: %s", err, text)
		}
		return err
	}
	if text := strings.TrimSpace(output.String()); text != "" {
		status(cCyan, "CMD", "%s", text)
	}
	return nil
}

func findMessages(root, archive string) ([]string, error) {
	archive = filepath.Clean(archive)
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		clean := filepath.Clean(path)
		if d.IsDir() && clean == archive {
			return filepath.SkipDir
		}
		if !d.IsDir() || (d.Name() != "new" && d.Name() != "cur") {
			return nil
		}
		if !looksLikeMaildir(filepath.Dir(path)) {
			return nil
		}

		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			out = append(out, filepath.Join(path, entry.Name()))
		}
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func looksLikeMaildir(dir string) bool {
	for _, name := range []string{"new", "cur"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !st.IsDir() {
			return false
		}
	}
	return true
}

func ensureMaildir(dir string) error {
	for _, name := range []string{"cur", "new", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0700); err != nil {
			return fmt.Errorf("create Archive maildir: %w", err)
		}
	}
	return nil
}

func archiveMessage(src, archive string) error {
	dstDir := filepath.Join(archive, "cur")
	if err := os.MkdirAll(dstDir, 0700); err != nil {
		return err
	}
	base := filepath.Base(src)
	dst := filepath.Join(dstDir, base)
	if _, err := os.Stat(dst); err == nil {
		dst = filepath.Join(dstDir, fmt.Sprintf("%d.%s", time.Now().UnixNano(), base))
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Fallback for an unusual cross-filesystem archive path.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(dst)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(dst)
		return closeErr
	}
	return os.Remove(src)
}

func extractBody(h textproto.MIMEHeader, r io.Reader, maxBytes int64) (string, error) {
	return extractPart(h, r, maxBytes, 0)
}

func extractPart(h textproto.MIMEHeader, r io.Reader, maxBytes int64, depth int) (string, error) {
	if depth > 20 {
		return "", errors.New("MIME nesting too deep")
	}

	if disp := h.Get("Content-Disposition"); disp != "" {
		disposition, params, err := mime.ParseMediaType(disp)
		if err == nil && (strings.EqualFold(disposition, "attachment") || params["filename"] != "") {
			return "", nil
		}
	}

	mediaType := "text/plain"
	params := map[string]string{}
	if raw := h.Get("Content-Type"); raw != "" {
		mt, p, err := mime.ParseMediaType(raw)
		if err != nil {
			return "", fmt.Errorf("parse Content-Type: %w", err)
		}
		mediaType, params = strings.ToLower(mt), p
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", errors.New("multipart message has no boundary")
		}
		mr := multipart.NewReader(r, boundary)
		var plainParts, htmlParts []string
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", err
			}
			partType := strings.ToLower(part.Header.Get("Content-Type"))
			text, err := extractPart(part.Header, part, maxBytes, depth+1)
			part.Close()
			if err != nil {
				return "", err
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			if strings.HasPrefix(partType, "text/html") {
				htmlParts = append(htmlParts, text)
			} else {
				plainParts = append(plainParts, text)
			}
		}
		if len(plainParts) > 0 {
			return enforceBodyLimit(strings.Join(plainParts, "\n\n"), maxBytes)
		}
		return enforceBodyLimit(strings.Join(htmlParts, "\n\n"), maxBytes)
	}

	if mediaType == "message/rfc822" || (!strings.HasPrefix(mediaType, "text/plain") && !strings.HasPrefix(mediaType, "text/html")) {
		return "", nil
	}

	decoded := decodeTransferEncoding(h.Get("Content-Transfer-Encoding"), r)
	data, err := readLimited(decoded, maxBytes)
	if err != nil {
		return "", err
	}
	text := decodeCharset(data, params["charset"])
	if strings.HasPrefix(mediaType, "text/html") {
		text = htmlToText(text)
	}
	return enforceBodyLimit(text, maxBytes)
}

func decodeTransferEncoding(enc string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	default:
		return r
	}
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("decoded body exceeds %d bytes", max)
	}
	return data, nil
}

func enforceBodyLimit(s string, max int64) (string, error) {
	if int64(len(s)) > max {
		return "", fmt.Errorf("decoded body exceeds %d bytes", max)
	}
	return s, nil
}

func decodeCharset(data []byte, charset string) string {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "iso-8859-1", "latin1", "latin-1":
		runes := make([]rune, len(data))
		for i, b := range data {
			runes[i] = rune(b)
		}
		return string(runes)
	default:
		if utf8.Valid(data) {
			return string(data)
		}
		return strings.ToValidUTF8(string(data), "�")
	}
}

func htmlToText(s string) string {
	s = scriptRE.ReplaceAllString(s, "")
	s = styleRE.ReplaceAllString(s, "")
	s = breakTagRE.ReplaceAllString(s, "\n")
	s = tagRE.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = multiNLRE.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func status(color, label, format string, args ...any) {
	stamp := time.Now().Format("15:04:05")
	fmt.Printf("%s%s[%s] %-8s%s %s\n", cBold, color, stamp, label, cReset, fmt.Sprintf(format, args...))
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func envDuration(name string, fallback time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func envInt64(name string, fallback int64) int64 {
	if v := os.Getenv(name); v != "" {
		var n int64
		if _, err := fmt.Sscan(v, &n); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
