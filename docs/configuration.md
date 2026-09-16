[Documentation home](../README.md)

# Configuration

- [Mail commands](#mail-commands)
- [Loading and overriding settings](#loading-and-overriding-settings)
- [Retries and delivery state](#retries-and-delivery-state)
- [Zeal setup](zeal.md#enabling-it)
- [External tools](external-tools.md)
- [Complete example TOML](../janegpt.example.toml)

All ordinary settings are top-level TOML keys. Put them **before** any
`[[tools]]` tables: keys after a table header belong to that table.
`docsets_dir` and `max_tool_context_bytes` must remain top-level.

## Mail commands

Copy the example, edit your addresses and commands, then start janeGPT:

```sh
cp janegpt.example.toml janegpt.toml
./janeGPT
```

janeGPT automatically loads `janegpt.toml` from the current directory. Select a
different file with `-config=path/to/file.toml` or the `MAILBOT_CONFIG`
environment variable.

A configuration using MailSalonSync and msmtp is:

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
`"{recipient}"`; janeGPT replaces that exact argument with all reply addresses. A wrapper
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

## Loading and overriding settings

See [janegpt.example.toml](../janegpt.example.toml) for every setting. Durations use strings such as
`"30s"`, `"1m"`, and `"0s"`. Unknown keys, invalid values, and missing explicitly
selected files stop startup with an error. `~` expands in the config filename,
Maildir, archive, failed, state-directory, and executable paths.

Settings are applied in this order: built-in defaults, environment variables,
TOML, then explicit command-line overrides. Switches remain useful for temporary
overrides such as `-interval=0s`. Repeating `-sync-command` or `-send-command`
replaces that entire TOML command, one executable or argument per occurrence.

## Retries and delivery state

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

The ledger lives at `state_dir/state.json`. Retries run on the first scan after
the scheduled time. The default policy is:

```toml
state_dir = "~/.local/state/janeGPT"
failed = "Failed"
max_attempts = 5
retry_backoff = "5m"
completed_retention = "2160h"
```

The Failed Maildir is excluded from scans. Moving a quarantined message back
into the inbox alone does not reset its ledger record. Keep the ledger when
upgrading: deleting it removes duplicate-delivery protection. Completed records
expire after 90 days by default; quarantined records are retained.
