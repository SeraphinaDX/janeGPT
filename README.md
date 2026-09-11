# janeGPT

![Logo](logo.avif)

An Ollama bot made for email. You can ask local models questions by email.

## How it works

janeGPT runs a configured synchronization command, scans a Maildir for messages,
asks Ollama for a response, and passes a complete email to a configured sending
command. It does not know or care which mail programs you choose.

The receive command must finish after updating the configured Maildir. The send
command receives the complete RFC 5322/MIME message on standard input, including
headers and attachments.

## Capabilities

### Email processing

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

### Sender control and loop prevention

- In the default admin-only mode, prompts Ollama only for mail from the configured
  administrator. Replies use `Reply-To` when present, otherwise the admin address.
- `reply_anyone = true` replies directly to each message's sender.
- Honors a valid `Reply-To` header, including multiple reply addresses. The
  configured admin or original sender is used when `Reply-To` is absent.
- When reply-to-anyone mode is enabled and `from` is configured, messages from
  the bot's own address are archived without a reply to prevent a mail loop.
- Unauthorized, self-sent, and empty messages are archived without contacting
  Ollama.

### Ollama responses

- Uses any Ollama model available through the configured Ollama server.
- Sends each email as an independent, non-streaming request to Ollama's generate
  API.
- Supports a configurable system/personality prompt.
- Sends only the model's answer in the reply body; it does not quote the incoming
  email.
- Replies with `Re: ` followed by the decoded incoming subject and avoids adding
  a second reply prefix. Incoming subjects always take precedence; configured
  `subject` is only a fallback for mail without a subject.
- Adds `In-Reply-To` and extends `References` from valid incoming message IDs so
  supporting mail clients keep the exchange in one thread.
- Supports an optional `From` header.

### Webpage context

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

### External mail programs

- Works with any foreground receive command that updates the configured Maildir.
- Works with any send command that accepts a complete RFC 5322/MIME message on
  standard input.
- Passes commands as exact argument arrays without invoking a shell.
- Replaces an exact `{recipient}` send argument with the reply address.
- Can skip the receive command with `sync_command = []` when another service
  already synchronizes the Maildir.
- Applies a configurable timeout to receive and send commands and reports their
  captured output and failures in the status display.

### Scheduling, archiving, and recovery

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

### Configuration and limits

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

## Configuration

### Zeal docsets and external tools

Enable the documentation tool by pointing to the **Docset storage directory**
shown in Zeal's preferences. Install the desired docsets in Zeal first and install
Pandoc on the machine running Jane. Jane reads installed docsets; it does not
download arbitrary docsets requested by email.

```toml
docsets_dir = "~/.local/share/Zeal/Zeal/docsets"
max_tool_context_bytes = 131072
```

Email Jane with a subject such as `Python: explain pathlib with examples` and
a body such as `Use the Python docset. Explain pathlib and give practical examples.
Include the full docset in Markdown and Org.` Subject-only requests also work.

Ollama first selects a tool and its inputs using JSON output on its generate API.
Jane permits only configured tool names and declared string inputs, executes
fixed argument arrays without a shell, and supplies tool results to a second
Ollama request for the final explanation and examples. Tool selection sees only
the email subject and body, not attached documents or fetched webpages.
Normal conversations select no tools. At most three calls run per email;
tools cannot trigger additional tool calls from their output.

The built-in `zeal_docs` tool launches this executable as a separate helper:

```sh
./janeGPT docset -root=/path/to/docsets -list
printf '%s' '{"docset":"Python","query":"pathlib examples"}' | ./janeGPT docset -root=/path/to/docsets
```

The helper reads **every HTML/HTM/XHTML page** under the selected docset's
`Contents/Resources/Documents` directory and uses Pandoc to produce two
complete text exports: `Python.md` and `Python.org`. Both are attached to the
reply, including pages unrelated to the question. Headings identify source
pages; code blocks, tables, and prose are converted by Pandoc. These are text
exports, not copies of image assets, JavaScript, or the search database. Relative
links retain their original targets and may need the original docset to resolve.
Dynamic content requiring JavaScript is not rendered.

Query-matching pages are ranked for the model context, which is bounded separately
from the complete attachments. Jane does not claim that Ollama read every page.
The helper refuses symlinks and fails on unreadable/non-UTF-8 pages, conversion
errors, or size overflow instead of sending a partial export.

The default helper limits are 48 MiB of source HTML (including page labels),
48 MiB per export, and nine minutes total. The built-in tool has a ten-minute
timeout and a 128 MiB JSON-output limit. Large docsets may exceed these limits or
your mail server's message limit; MIME base64 adds roughly a third to attachment
size. Failures follow the configured retry/quarantine policy. To customize helper
limits, leave `docsets_dir` empty and register a tool explicitly:

```toml
[[tools]]
name = "zeal_docs"
description = "Read installed Python documentation; docset must be Python. query is the requested topic."
command = ["/home/you/bin/janeGPT", "docset", "-root=/path/to/docsets", "-max-bytes=67108864", "-context-bytes=131072", "-timeout=15m"]
inputs = ["docset", "query"]
timeout = "16m"
max_output_bytes = 201326592
```

Any external program can implement this protocol: receive a JSON object of
declared string inputs on stdin; write one JSON result on stdout; write diagnostics
to stderr; exit nonzero on failure. For example:

```json
{"context":"Reference text for the model","attachments":[{"name":"reference.md","content_type":"text/markdown","text":"# Full reference"}]}
```

`attachments` is optional. Only UTF-8 `.md` and `.org` output attachments are
accepted, with plain filenames; Jane assigns their MIME types from the extension.
Commands and executable paths come only from TOML; model inputs are never shell
commands. Only configure tools you intend email requests to invoke. Tools run
with Jane's account permissions and can run again on retry, so read-only or
idempotent commands are appropriate.

Tool defaults: `timeout = "2m"`, `max_output_bytes = 33554432`.
Tool results are explicitly marked as untrusted reference material. Existing
sync/send configuration and Reply-To/threading headers continue to work.

Format references: [Zeal usage](https://zealdocs.org/usage.html),
[Dash docset structure](https://kapeli.com/docsets), and
[Pandoc's manual](https://pandoc.org/MANUAL.html).

### Mail configuration

Copy the example, edit your addresses and commands, then start janeGPT:

```sh
cp janegpt.example.toml janegpt.toml
./janeGPT
```

janeGPT automatically loads `janegpt.toml` from the current directory. Select a
different file with `-config=path/to/file.toml` or the `MAILBOT_CONFIG`
environment variable.

A minimal configuration using MailSalonSync and msmtp is:

```toml
maildir = "~/Maildir"
archive = "Archive"
failed = "Failed"
state_dir = "~/.local/state/janeGPT"
admin = "you@example.org"
from = "bot@example.org"
model = "llama3.2"
interval = "1m"
max_attempts = 5
retry_backoff = "5m"
completed_retention = "2160h"

sync_command = ["MailSalonSync", "-plain", "sync"]
send_command = ["msmtp", "--", "{recipient}"]
```

The two command settings are program-agnostic arrays. The first item is the
executable and the remaining items are its arguments. janeGPT executes the array
directly without a shell, so spaces remain part of one argument and shell syntax
is not expanded. `~` is expanded in an executable path, but command arguments are
literal. Use an absolute path in an argument when needed.

Set `sync_command = []` if a service already synchronizes the Maildir. The send
command is required. If it needs the reply address as an argument, add
`"{recipient}"`; janeGPT replaces that exact argument with the address. A wrapper
script can adapt a program that does not accept a complete email on standard
input.

For mbsync:

```toml
sync_command = ["mbsync", "-a"]
```

For offlineimap:

```toml
sync_command = ["offlineimap", "-c", "/home/you/.config/offlineimap/config"]
```

For a sendmail-compatible sender that reads recipients from message headers:

```toml
send_command = ["/usr/sbin/sendmail", "-t", "-i"]
```

See `janegpt.example.toml` for every setting. Durations use strings such as
`"30s"`, `"1m"`, and `"0s"`. Unknown keys, invalid values, and missing explicitly
selected files stop startup with an error. `~` expands in the config filename,
Maildir, archive, failed, state-directory, and executable paths.

Settings are applied in this order: built-in defaults, environment variables,
TOML, then explicit command-line overrides. Switches remain useful for temporary
overrides such as `-interval=0s`. Repeating `-sync-command` or `-send-command`
replaces that entire TOML command, one executable or argument per occurrence.

Mail commands inherit janeGPT's environment. Keep credentials in the mail
programs' own configuration or environment. Both commands must run in the
foreground and return a nonzero status on failure. A receive failure stops the
scan. A send failure leaves the incoming message in place and schedules a retry.
After `max_attempts`, janeGPT moves it into the Failed Maildir. Each command has
a ten-minute timeout by default, configurable with `command_timeout = "20m"`.

Run only one janeGPT process against a state directory at a time. The delivery
ledger prevents resending after a confirmed successful send, but no mail client
can guarantee exactly-once delivery when a send program accepts a message and
then incorrectly exits with an error.

## Command-line options

```sh
./janeGPT --help
```

```text
Usage of janeGPT:
  -admin string
        only accepted sender address unless -reply-anyone is enabled
  -archive string
        Archive Maildir path, relative to -maildir unless absolute (default "Archive")
  -command-timeout duration
        timeout for each mail receive/send command (default 10m0s)
  -completed-retention duration
        how long to retain delivered-message records (default 2160h0m0s)
  -config string
        TOML configuration file (default: janegpt.toml if present)
  -docsets-dir string
        Zeal docset storage directory; empty disables built-in documentation tool
  -failed string
        Failed Maildir path, relative to -maildir unless absolute (default "Failed")
  -from string
        optional From header
  -interval duration
        scan interval; 0 means run once (default 1m0s)
  -maildir string
        Maildir root
  -max-attempts int
        maximum processing attempts before quarantine (default 5)
  -max-attachment-bytes int
        maximum decoded text attachment size (default 131072)
  -max-attachment-context-bytes int
        maximum combined attachment text sent to Ollama (default 262144)
  -max-body-bytes int
        maximum decoded prompt body size (default 2097152)
  -max-message-bytes int
        maximum incoming message file size (default 10485760)
  -max-page-bytes int
        maximum downloaded HTML page size (default 10485760)
  -max-web-context-bytes int
        maximum Org-mode webpage text included in the Ollama prompt (default 131072)
  -max-tool-context-bytes int
        maximum combined tool reference text sent to Ollama (default 131072)
  -model string
        Ollama model (default "llama3.2")
  -ollama-url string
        Ollama base URL (default "http://127.0.0.1:11434")
  -page-timeout duration
        timeout for fetching each URL (default 30s)
  -personality string
        system/personality prompt sent to Ollama
  -reply-anyone
        reply to any sender instead of only the configured admin
  -retry-backoff duration
        initial retry delay; doubles after each failure (default 5m0s)
  -send-command value
        send command element (repeat); reads message on stdin; {recipient} expands in arguments
  -state-dir string
        directory for the durable delivery ledger (default "~/.local/state/janeGPT")
  -subject string
        fallback subject for mail with no subject; incoming subjects always take precedence
  -sync-command value
        receive command element (repeat for executable and each argument)
```
