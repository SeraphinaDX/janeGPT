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
- Excludes the configured Archive Maildir and creates its `cur`, `new`, and
  `tmp` directories when necessary.
- Accepts plain-text and HTML message bodies, including nested multipart email.
- Prefers plain text when a multipart message contains both plain-text and HTML
  versions. HTML-only messages are converted to readable plain text.
- Decodes base64 and quoted-printable bodies, UTF-8 text, and ISO-8859-1 text.
- Ignores MIME file attachments and embedded `message/rfc822` parts when building
  the model prompt.
- Removes standard signature delimiters and common GrapheneOS, iPhone, iPad,
  Android, and Outlook mobile signatures.
- Rejects malformed messages, invalid sender addresses, excessive MIME nesting,
  and messages or decoded bodies larger than the configured limits.

### Sender control and loop prevention

- In the default admin-only mode, prompts Ollama only for mail from the configured
  administrator and replies to that address.
- `reply_anyone = true` replies directly to each message's sender.
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
- Supports a configurable reply subject and optional `From` header.

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
- Archives a processed message only after its reply command succeeds, allowing
  model and delivery failures to be retried on a later scan.
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

Current scope: janeGPT does not give Ollama the sender's file attachments, retain
conversation history between emails, preserve the incoming subject or threading
headers, or fetch non-HTML URL content.

## Configuration

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
admin = "you@example.org"
from = "bot@example.org"
model = "llama3.2"
interval = "1m"

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
Maildir and archive paths, and executable paths.

Settings are applied in this order: built-in defaults, environment variables,
TOML, then explicit command-line overrides. Switches remain useful for temporary
overrides such as `-interval=0s`. Repeating `-sync-command` or `-send-command`
replaces that entire TOML command, one executable or argument per occurrence.

Mail commands inherit janeGPT's environment. Keep credentials in the mail
programs' own configuration or environment. Both commands must run in the
foreground and return a nonzero status on failure. A receive failure stops the
scan. A send failure leaves the incoming message unarchived for retry. Each
command has a ten-minute timeout by default, configurable with
`command_timeout = "20m"`.

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
  -config string
        TOML configuration file (default: janegpt.toml if present)
  -from string
        optional From header
  -interval duration
        scan interval; 0 means run once (default 1m0s)
  -maildir string
        Maildir root
  -max-body-bytes int
        maximum decoded prompt body size (default 2097152)
  -max-message-bytes int
        maximum incoming message file size (default 10485760)
  -max-page-bytes int
        maximum downloaded HTML page size (default 10485760)
  -max-web-context-bytes int
        maximum Org-mode webpage text included in the Ollama prompt (default 131072)
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
  -send-command value
        send command element (repeat); reads message on stdin; {recipient} expands in arguments
  -subject string
        static subject for replies (default "Ollama response")
  -sync-command value
        receive command element (repeat for executable and each argument)
```
