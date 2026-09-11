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

type replyHeaders struct {
	Subject    string
	InReplyTo  string
	References string
}

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

func cleanHeaderText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
