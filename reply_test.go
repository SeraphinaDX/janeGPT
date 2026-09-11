package main

import (
	"net/mail"
	"reflect"
	"strings"
	"testing"
)

func TestReplyRecipients(t *testing.T) {
	header := mail.Header{"Reply-To": {`Jane <jane@example.org>, Other <other@example.org>, JANE@example.org`}}
	got, err := replyRecipients(header, "fallback@example.org")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"jane@example.org", "other@example.org"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}

	got, err = replyRecipients(mail.Header{}, "fallback@example.org")
	if err != nil || !reflect.DeepEqual(got, []string{"fallback@example.org"}) {
		t.Fatalf("fallback: %q, %v", got, err)
	}

	if _, err := replyRecipients(mail.Header{"Reply-To": {"not an address"}}, "fallback@example.org"); err == nil {
		t.Fatal("accepted invalid Reply-To")
	}
}

func TestBuildReplyHeaders(t *testing.T) {
	header := mail.Header{
		"Subject":    {"=?UTF-8?Q?Project_=E2=9C=A8?="},
		"Message-Id": {"<child@example.org>\r\ninvalid"},
		"References": {"<root@example.org> <root@example.org> <older@example.org>"},
	}
	got := buildReplyHeaders(header, "")
	if got.Subject != "Re: Project ✨" {
		t.Fatalf("subject: %q", got.Subject)
	}
	if got.InReplyTo != "<child@example.org>" {
		t.Fatalf("In-Reply-To: %q", got.InReplyTo)
	}
	if got.References != "<root@example.org> <older@example.org> <child@example.org>" {
		t.Fatalf("References: %q", got.References)
	}

	got = buildReplyHeaders(mail.Header{"Subject": {"Re: Existing"}}, "")
	if got.Subject != "Re: Existing" {
		t.Fatalf("duplicated reply prefix: %q", got.Subject)
	}
	got = buildReplyHeaders(mail.Header{"Subject": {"Ignored"}}, "Custom\r\nInjected")
	if got.Subject != "Re: Ignored" {
		t.Fatalf("incoming subject must win: %q", got.Subject)
	}
	if got := replySubject("", "Custom\r\nInjected"); got != "Custom Injected" {
		t.Fatalf("fallback subject: %q", got)
	}
}

func TestReferenceLimit(t *testing.T) {
	var ids []string
	for i := 0; i < maxReferenceIDs+5; i++ {
		ids = append(ids, "<"+strings.Repeat("x", i+1)+"@example.org>")
	}
	got := buildReplyHeaders(mail.Header{"References": {strings.Join(ids, " ")}}, "")
	if len(messageIDs(got.References)) != maxReferenceIDs {
		t.Fatalf("kept %d references", len(messageIDs(got.References)))
	}
	oversized := "<" + strings.Repeat("x", maxMessageIDBytes) + "@example.org>"
	if got := messageIDs(oversized); len(got) != 0 {
		t.Fatalf("accepted oversized Message-ID: %q", got)
	}
}
