# janeGPT

![Logo](logo.avif)

An Ollama bot made for E-mail. You can ask local models stuff via e-mail.

## How

It works by scanning ~/Maildir to find new messages. Normally you give it its own account.

If it finds a request, it sends the model response through a configurable mail command.

By default, janeGPT uses **offlineimap** to receive mail and **msmtp** to send it.
You can substitute other programs using `-sync-command` and `-send-command`.
Configure your chosen programs separately; the receiver must deliver mail to the
Maildir passed to `-maildir`.

## Usage

### Choose your mail programs

`-sync-command` takes an executable name or path. Supply each argument separately
with `-sync-arg`; use the same pattern for sending with `-send-command` and
`-send-arg`. Commands run directly, without a shell. Paths containing spaces are
supported when quoted, but shell pipelines, `$VARIABLE` expansion inside argument
values, and `~` expansion inside quoted paths are not performed by janeGPT.
Use an absolute path or a command on `PATH`. A wrapper script can adapt programs
that require a different interface.

For **MailSalonSync** receiving and the existing msmtp sender:

```sh
./janeGPT -maildir="$HOME/Maildir" -admin=you@example.org \
  -sync-command=MailSalonSync -sync-arg=-plain -sync-arg=sync
```

For **mbsync** receiving:

```sh
./janeGPT -maildir="$HOME/Maildir" -admin=you@example.org \
  -sync-command=mbsync -sync-arg=-a
```

For offlineimap with a particular configuration:

```sh
./janeGPT -maildir="$HOME/Maildir" -admin=you@example.org \
  -sync-command=offlineimap -sync-arg=-c \
  -sync-arg="$HOME/.config/offlineimap/config"
```

If another service already synchronizes the Maildir, use `-no-sync`. janeGPT will
only scan the local messages on each cycle.

A custom sender receives the complete RFC 5322/MIME message on standard input,
including headers and attachments. For a sendmail-compatible sender that reads
recipients from headers:

```sh
./janeGPT -maildir="$HOME/Maildir" -admin=you@example.org \
  -no-sync -from=bot@example.org \
  -send-command=/usr/sbin/sendmail -send-arg=-t -send-arg=-i
```

If the sender needs an explicit recipient argument, set `-send-arg='{recipient}'`.
Only that exact, whole argument is replaced with the reply address. No msmtp
arguments are automatically added to a custom sender. Use a wrapper if your sender
does not accept a complete message on stdin. Set `-from` when the sender requires
a From header.

The executable names can also be set through `MAILBOT_SYNC_COMMAND` and
`MAILBOT_SEND_COMMAND`. These variables contain only the executable, not a command
line. The existing `-offlineimap`, `-msmtp`, `-msmtp-account`, and their environment
variables still work when no custom command is selected. `-sync-arg` and
`-send-arg` require their corresponding custom command. `-no-sync` cannot be
combined with a custom sync command; unset `MAILBOT_SYNC_COMMAND` if necessary.

janeGPT waits for the receiver to exit successfully before scanning. Both
commands must run in the foreground and return a nonzero exit status on failure.
Each invocation has a ten-minute timeout, configurable with
`-command-timeout=20m`. A receive failure stops that scan; a send failure leaves
the incoming message unarchived for retry. As with most command-based mail
delivery, an ambiguous send failure can lead to a duplicate reply on retry.
Commands inherit janeGPT's environment, so credentials can be provided through
the environment or the mail program's own configuration.

### All options

```
./janeGPT --help
```
```text
Usage of ./janeGPT:
  -admin string
        only accepted sender address unless -reply-anyone is enabled
  -archive string
        Archive Maildir path, relative to -maildir unless absolute (default "Archive")
  -command-timeout duration
        timeout for each mail receive/send command (default 10m0s)
  -from string
        optional From header; msmtp config may add it instead
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
  -msmtp string
        msmtp executable (default "msmtp")
  -msmtp-account string
        optional msmtp account name
  -no-sync
        scan Maildir without running a receive command
  -offlineimap string
        offlineimap executable (default "offlineimap")
  -ollama-url string
        Ollama base URL (default "http://127.0.0.1:11434")
  -page-timeout duration
        timeout for fetching each URL (default 30s)
  -personality string
        system/personality prompt sent to Ollama
  -reply-anyone
        reply to any sender instead of only the configured admin
  -send-arg value
        argument for -send-command (repeat); {recipient} expands to reply address
  -send-command string
        mail send executable; reads complete message on stdin; overrides -msmtp
  -subject string
        static subject for replies (default "Ollama response")
  -sync-arg value
        argument for -sync-command (repeat for each argument)
  -sync-command string
        mail receive executable; overrides -offlineimap
```
