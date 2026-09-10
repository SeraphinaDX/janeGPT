package main

import (
	"encoding/base64"
	"net/textproto"
	"strings"
	"testing"
)

func TestIncomingMultipartAttachments(t *testing.T) {
	body := "--boundary\r\nContent-Type: text/plain\r\n\r\nExplain these files\r\n" +
		"--boundary\r\nContent-Type: application/octet-stream; name=notes.md\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString([]byte("# Notes\nA useful fact")) + "\r\n" +
		"--boundary\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=report.pdf\r\n\r\nPDF\r\n--boundary--\r\n"
	h := textproto.MIMEHeader{"Content-Type": {"multipart/mixed; boundary=boundary"}}
	a := &incomingAttachments{perFile: 100, remaining: 200}
	text, err := extractPartWithAttachments(h, strings.NewReader(body), 1000, 0, a)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Explain these files" || len(a.files) != 1 || a.files[0].Text != "# Notes\nA useful fact" || len(a.notes) != 1 {
		t.Fatalf("body=%q files=%+v notes=%v", text, a.files, a.notes)
	}
	if !strings.Contains(a.prompt(text), "notes.md") || !strings.Contains(a.notes[0], "report.pdf") {
		t.Fatal("missing filename labels")
	}
}

func TestAttachmentLimitsAndFailures(t *testing.T) {
	a := &incomingAttachments{perFile: 5, remaining: 7}
	a.read("one.txt", "", "", strings.NewReader("12345"))
	a.read("two.org", "", "", strings.NewReader("123"))
	a.read("three.md", "", "", strings.NewReader("12"))
	a.read("four.txt", "", "", strings.NewReader("1"))
	if len(a.files) != 2 || len(a.notes) != 2 || a.remaining != 0 {
		t.Fatalf("%+v", a)
	}
	for _, tc := range []struct{ name, encoding, data string }{
		{"bad.txt", "base64", "!!!"},
		{"binary.md", "", "\x00"},
		{"bad.org", "", "\xff"},
		{"empty.txt", "", ""},
		{"large.md", "", "123456"},
	} {
		a := &incomingAttachments{perFile: 5, remaining: 20}
		a.read(tc.name, "", tc.encoding, strings.NewReader(tc.data))
		if len(a.files) != 0 || len(a.notes) != 1 {
			t.Fatalf("%s: %+v", tc.name, a)
		}
	}
}

func TestAttachmentOnlyAndQuotedPrintable(t *testing.T) {
	h := textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Disposition":       {"attachment; filename*=UTF-8''caf%C3%A9.txt"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	}
	a := &incomingAttachments{perFile: 100, remaining: 100}
	body, err := extractPartWithAttachments(h, strings.NewReader("caf=C3=A9"), 100, 0, a)
	if err != nil || body != "" || len(a.files) != 1 || a.files[0].Name != "café.txt" || a.files[0].Text != "café" {
		t.Fatalf("body=%q files=%+v err=%v", body, a.files, err)
	}
	if !strings.HasPrefix(a.prompt(body), "Summarize") {
		t.Fatal("missing attachment-only instruction")
	}
}
