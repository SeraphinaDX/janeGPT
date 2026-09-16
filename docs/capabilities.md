[Documentation home](../README.md)

# Capabilities

## Email processing

- Scans both `new` and `cur` in every valid Maildir below the configured root.
- Excludes the configured Archive and Failed Maildirs and creates their `cur`,
  `new`, and `tmp` directories when necessary.
- Accepts plain-text and HTML message bodies, including nested multipart email.
- Prefers plain text when a multipart message contains both plain-text and HTML
  versions. HTML-only messages are converted to readable plain text.
- Decodes base64 and quoted-printable bodies, UTF-8 text, and ISO-8859-1 text.
- Reads incoming `.txt`, `.md`, and `.org` attachments as reference material,
  labeled by filename. Supports base64, quoted-printable, UTF-8, and Latin-1.
- Applies per-file and combined UTF-8 text limits; files that exceed either limit
  are skipped whole. Unsupported, empty, or unreadable files appear in terminal
  status and a deterministic attachment report in the reply.
- Treats attachment text and filenames as untrusted reference data in the system
  prompt. URLs inside attachments are not fetched.
- Summarizes readable attachments when an email has no body. Incoming files are
  not reattached to the reply. Embedded `message/rfc822` parts remain ignored.
- Removes standard signature delimiters and common GrapheneOS, iPhone, iPad,
  Android, and Outlook mobile signatures.
- Rejects malformed messages, invalid sender addresses, excessive MIME nesting,
  and messages or decoded bodies larger than the configured limits.

## Sender control and loop prevention

- In the default admin-only mode, prompts Ollama only for mail from the configured
  administrator. Replies use `Reply-To` when present, otherwise the admin address.
- `reply_anyone = true` replies directly to each message's sender.
- Honors a valid `Reply-To` header, including multiple reply addresses. The
  configured admin or original sender is used when `Reply-To` is absent.
- When reply-to-anyone mode is enabled and `from` is configured, messages from
  the bot's own address are archived without a reply to prevent a mail loop.
- Unauthorized and self-sent messages are archived without contacting Ollama.
  Messages with no subject, body, or attachment information are also archived.

## Ollama responses

- Uses any Ollama model available through the configured Ollama server.
- Processes each email independently using Ollama's non-streaming generate API.
  With tools enabled, a selection request precedes the final response request.
- Supports a configurable system/personality prompt.
- Replies with the model's answer without automatically quoting the incoming
  email. A report of skipped incoming attachments is appended when needed.
- Replies with `Re: ` followed by the decoded incoming subject and avoids adding
  a second reply prefix. Incoming subjects always take precedence; configured
  `subject` is only a fallback for mail without a subject.
- Adds `In-Reply-To` and extends `References` from valid incoming message IDs so
  supporting mail clients keep the exchange in one thread.
- Supports an optional `From` header.

## Webpage context

- Finds unique HTTP and HTTPS URLs in the email body and downloads their pages.
- Accepts HTML and XHTML pages, follows up to five validated redirects, and
  rejects URLs that resolve to loopback, private, link-local, multicast, or
  unspecified addresses.
- Converts fetched pages to Org mode while retaining the page title, source URL,
  headings, links, lists, and preformatted text.
- Includes the converted text in the Ollama prompt up to the configured combined
  context limit.
- Marks webpage content as untrusted reference material in the system prompt so
  the model is told not to follow instructions found on a page.
- Attaches each converted `.org` page to the outgoing reply.
- Applies configurable per-page download size and timeout limits. If any URL
  cannot be fetched or validated, that message remains unprocessed for retry.

## External mail programs

- Works with any foreground receive command that updates the configured Maildir.
- Works with any send command that accepts a complete RFC 5322/MIME message on
  standard input.
- Passes commands as exact argument arrays without invoking a shell.
- Replaces an exact `{recipient}` send argument with all reply addresses.
- Can skip the receive command with `sync_command = []` when another service
  already synchronizes the Maildir.
- Applies a configurable timeout to receive and send commands and reports their
  captured output and failures in the status display.

## Documentation and other tools

- Selects configured tools from the email subject and body, with at most three
  calls per email. Tool results cannot trigger further calls.
- Reads installed Zeal docsets through the `janeGPT docset` helper and Pandoc.
- Attaches full Markdown and Org text exports of every HTML/HTM/XHTML page.
- Uses bounded, query-ranked excerpts for Ollama's explanation and examples.
- Runs configured external commands with JSON inputs, timeouts, and output limits.
- See [Zeal examples](zeal.md#using-it-by-email) and the
  [external tool protocol](external-tools.md).

## Scheduling, archiving, and recovery

- Polls at a configurable interval or runs one scan with `interval = "0s"`.
- Processes discovered messages in sorted path order and continues to other
  messages when one fails.
- Identifies a message by its valid `Message-ID`, or by a SHA-256 content hash
  when no usable ID is present.
- Stores retry and delivery records in an atomically replaced, private JSON
  ledger below the configured state directory.
- Records successful delivery before archiving. If janeGPT restarts after the
  send succeeds but before archival, it archives the message without resending.
- Retries model, webpage, parsing, and mail-command failures with exponential
  backoff, capped at 24 hours, while showing the attempt count and next retry.
- Moves a message to the configured Failed Maildir after the maximum attempt
  count and retains the last error as its quarantine reason.
- Prunes delivered-message records after a configurable retention period.
- Avoids overwriting an existing archived message with the same filename.
- Can archive across filesystems by copying and then removing the source when a
  direct rename is unavailable.
- Handles interrupt and termination signals and stops after the active operation
  returns or is cancelled.
- Prints color-coded status for synchronization, message discovery, reading,
  webpage fetching, Ollama, sending, archiving, and errors.

## Configuration and limits

- Loads all settings from TOML and automatically discovers `janegpt.toml` in the
  current directory.
- Supports an explicit config path, environment-variable defaults, and temporary
  command-line overrides.
- Rejects unknown TOML keys, malformed values, invalid email addresses, an
  invalid Ollama URL, and non-positive size or timeout limits at startup.
- Expands `~` in config, Maildir, Archive, and executable paths.
- Provides separate limits for incoming message size, decoded body size, fetched
  page size, and total webpage context sent to Ollama.

Current scope: janeGPT does not read PDF, office, image, or archive attachments,
retain conversation history between emails, or fetch non-HTML URL content.

Incoming attachment limits default to 128 KiB per file and 256 KiB combined:

```toml
max_attachment_bytes = 131072
max_attachment_context_bytes = 262144
```

These limits count decoded UTF-8 document text; filenames and JSON framing add
some prompt overhead. The existing incoming message size limit still applies.
Environment defaults are `MAILBOT_MAX_ATTACHMENT_BYTES` and
`MAILBOT_MAX_ATTACHMENT_CONTEXT_BYTES`.

