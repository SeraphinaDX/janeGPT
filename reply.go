package main

import (
	"fmt"
	"mime"
	"net/mail"
	"regexp"
	"strings"
)

const (
	maxMessageIDBytes  = 255
	maxReferenceIDs    = 20
	maxReferencesBytes = 900
)

var (
	messageIDRE   = regexp.MustCompile(`<[^<>\x00-\x20\x7f]+>`)
	replyPrefixRE = regexp.MustCompile(`(?i)^\s*re\s*:`)
)

// replyHeaders contains normalized outgoing subject and threading metadata.
// Threading links messages in mail clients; it does not provide model memory.
type replyHeaders struct {
	Subject    string
	InReplyTo  string
	References string
}

// replyRecipients honors Reply-To after the caller has checked the sender.
// An invalid explicit Reply-To is an error rather than a silent fallback.
func replyRecipients(header mail.Header, fallback string) ([]string, error) {
	raw := strings.TrimSpace(header.Get("Reply-To"))
	if raw == "" {
		return []string{fallback}, nil
	}
	addresses, err := mail.ParseAddressList(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid Reply-To header: %w", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("Reply-To header contains no addresses")
	}
	recipients := make([]string, 0, len(addresses))
	seen := make(map[string]bool, len(addresses))
	for _, address := range addresses {
		key := strings.ToLower(address.Address)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		recipients = append(recipients, address.Address)
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("Reply-To header contains no usable addresses")
	}
	return recipients, nil
}

// buildReplyHeaders retains the newest bounded References chain and appends
// the parent Message-ID when it is not already present.
func buildReplyHeaders(header mail.Header, subjectOverride string) replyHeaders {
	parentIDs := messageIDs(header.Get("Message-ID"))
	var parent string
	if len(parentIDs) > 0 {
		parent = parentIDs[0]
	}

	references := messageIDs(header.Get("References"))
	if parent != "" && !containsString(references, parent) {
		references = append(references, parent)
	}
	if len(references) > maxReferenceIDs {
		references = references[len(references)-maxReferenceIDs:]
	}
	for len(references) > 1 && len(strings.Join(references, " ")) > maxReferencesBytes {
		references = references[1:]
	}

	return replyHeaders{
		Subject:    replySubject(header.Get("Subject"), subjectOverride),
		InReplyTo:  parent,
		References: strings.Join(references, " "),
	}
}

// replySubject preserves an incoming subject, decoding MIME words and adding
// Re: only when needed. The configured override is solely an empty-subject fallback.
func replySubject(incoming, override string) string {
	if strings.TrimSpace(incoming) == "" && strings.TrimSpace(override) != "" {
		return cleanHeaderText(override)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(incoming)
	if err != nil {
		subject = incoming
	}
	subject = cleanHeaderText(subject)
	if subject == "" {
		return "Ollama response"
	}
	if replyPrefixRE.MatchString(subject) {
		return subject
	}
	return "Re: " + subject
}

// messageIDs extracts bounded, unique angle-bracket tokens suitable for the
// threading headers. This is conservative token filtering, not full RFC validation.
func messageIDs(value string) []string {
	matches := messageIDRE.FindAllString(value, -1)
	out := make([]string, 0, len(matches))
	for _, id := range matches {
		if len(id) > maxMessageIDBytes {
			continue
		}
		if !containsString(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// cleanHeaderText collapses whitespace, including CR/LF, into a single line
// before text is reused in an outgoing header or filename label.
func cleanHeaderText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
