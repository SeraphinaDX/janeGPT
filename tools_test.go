package main

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixtureDocset(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "Python.docset", "Contents", "Resources", "Documents")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"pathlib.html": "<html><body><h2>Pathlib</h2><p>Use Path to open files.</p><pre><code>from pathlib import Path\np = Path('example.txt')</code></pre></body></html>",
		"other.html":   "<html><body><h2>Unrelated page</h2><p>FULL_DOCSET_SENTINEL</p></body></html>",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func requirePandoc(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not installed")
	}
}

func TestCompleteDocsetExports(t *testing.T) {
	requirePandoc(t)
	root := fixtureDocset(t)
	result, err := exportDocset(context.Background(), root, "Python", "pathlib", "pandoc", 1<<20, 64)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Attachments) != 2 {
		t.Fatal("missing formats")
	}
	for _, file := range result.Attachments {
		for _, want := range []string{"FULL_DOCSET_SENTINEL", "from pathlib import Path", "pathlib.html", "other.html"} {
			if !strings.Contains(file.Text, want) {
				t.Fatalf("%s missing %s: %s", file.Name, want, file.Text)
			}
		}
	}
	if !strings.Contains(result.Context, "all 2 HTML pages") {
		t.Fatal(result.Context)
	}
	if _, err := exportDocset(context.Background(), root, "../Python", "x", "pandoc", 1<<20, 100); err == nil {
		t.Fatal("accepted path")
	}
	if _, err := exportDocset(context.Background(), root, "Python", "x", "pandoc", 10, 100); err == nil {
		t.Fatal("partial export allowed")
	}
	docs := filepath.Join(root, "Python.docset", "Contents", "Resources", "Documents")
	if err := os.Symlink("/etc/passwd", filepath.Join(docs, "escape.html")); err != nil {
		t.Fatal(err)
	}
	if _, err := exportDocset(context.Background(), root, "Python", "x", "pandoc", 1<<20, 100); err == nil {
		t.Fatal("followed symlink")
	}
}

func TestExternalToolBounds(t *testing.T) {
	cfg, _ := helperConfig(t, "overflow")
	if _, err := captureCommand(context.Background(), cfg.SendCommand, nil, 20); err == nil {
		t.Fatal("accepted excess output")
	}
	cfg, _ = helperConfig(t, "wait")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := captureCommand(ctx, cfg.SendCommand, nil, 20); err == nil {
		t.Fatal("timeout ignored")
	}
}

func TestDocsetEmailEndToEnd(t *testing.T) {
	requirePandoc(t)
	root := fixtureDocset(t)
	helper, _ := helperConfig(t, "docset")
	docCommand := append([]string(helper.SendCommand), root)
	sender, capture := helperConfig(t, "send")
	var mu sync.Mutex
	var requests []ollamaRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		mu.Lock()
		requests = append(requests, req)
		mu.Unlock()
		answer := "Pathlib explanation and examples."
		if req.Format == "json" {
			answer = `{"calls":[{"name":"zeal_docs","arguments":{"docset":"Python","query":"pathlib examples"}}]}`
		}
		json.NewEncoder(w).Encode(ollamaResponse{Response: answer})
	}))
	defer server.Close()
	cfg := testCycleConfig(t, sender.SendCommand, server.URL)
	cfg.Subject = "Old fixed subject"
	cfg.Tools = []externalTool{{Name: "zeal_docs", Description: "Python docs", Inputs: []string{"docset", "query"}, Command: docCommand}}
	if err := validateTools(&cfg); err != nil {
		t.Fatal(err)
	}
	message := "From: admin@example.org\r\nSubject: Python pathlib\r\nMessage-ID: <docs@example.org>\r\n\r\nExplain pathlib with examples and attach the whole Python docset."
	if err := os.WriteFile(filepath.Join(cfg.MaildirRoot, "new", "docs"), []byte(message), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runCycle(context.Background(), cfg); err != nil {
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
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != "Re: Python pathlib" {
		t.Fatalf("subject=%q %v", subject, err)
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	files := map[string]string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(decodeTransferEncoding(part.Header.Get("Content-Transfer-Encoding"), part))
		if err != nil {
			t.Fatal(err)
		}
		files[part.FileName()] = string(content)
	}
	for _, name := range []string{"Python.md", "Python.org"} {
		if !strings.Contains(files[name], "FULL_DOCSET_SENTINEL") {
			t.Fatalf("missing full export %s", name)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || !strings.Contains(requests[1].Prompt, "Pathlib") || !strings.Contains(requests[1].System, toolSystem) {
		t.Fatalf("requests=%+v", requests)
	}
}

func TestUnknownToolNeverRuns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ollamaResponse{Response: `{"calls":[{"name":"shell","arguments":{}}]}`})
	}))
	defer server.Close()
	cfg := defaultConfig()
	cfg.OllamaURL = server.URL
	cfg.Tools = []externalTool{{Name: "reference", Description: "Docs", Command: []string{"unreachable"}}}
	if err := validateTools(&cfg); err != nil {
		t.Fatal(err)
	}
	if _, _, err := collectTools(context.Background(), server.Client(), cfg, "Hi", "Hi"); err == nil || !strings.Contains(err.Error(), "unconfigured") {
		t.Fatal(err)
	}
}

func TestToolTOML(t *testing.T) {
	p := configFile(t, `docsets_dir = "~/docsets"
max_tool_context_bytes = 8192
[[tools]]
name = "reference"
description = "Look up a query"
command = ["~/bin/reference", "--json"]
inputs = ["query"]
timeout = "3m"
max_output_bytes = 4096
`)
	cfg, err := loadConfig([]string{"-config=" + p}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTools(&cfg); err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(cfg.DocsetsDir) || len(cfg.Tools) != 1 || !filepath.IsAbs(cfg.Tools[0].Command[0]) || cfg.Tools[0].Timeout != 3*time.Minute || cfg.Tools[0].MaxOutput != 4096 || cfg.MaxToolContext != 8192 {
		t.Fatalf("config=%+v", cfg)
	}
	cfg.Tools = append(cfg.Tools, cfg.Tools[0])
	if err := validateTools(&cfg); err == nil {
		t.Fatal("duplicate tool accepted")
	}
}
