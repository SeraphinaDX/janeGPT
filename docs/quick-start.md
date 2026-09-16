[Documentation home](../README.md)

# Quick start

## Requirements

- Go 1.22 or newer to build Jane.
- A running Ollama server with your configured model available.
- A Maildir and working external sync/send programs.
- For Zeal: installed docsets and Pandoc on Jane's machine.

## Build and configure

From the repository root:

```sh
go build -o janeGPT .
cp janegpt.example.toml janegpt.toml
```

Edit `janegpt.toml`. This compact example uses MailSalonSync for both directions.
Replace the addresses and `bot-jmap` with your configured account:

```toml
maildir = "~/Maildir-bot"
admin = "you@example.org"
from = "bot@example.org"
model = "gemma4"
personality = "Be friendly and explain things clearly with practical examples."
interval = "1m"
sync_command = ["MailSalonSync", "-plain", "sync"]
send_command = ["MailSalonSync", "jmap-send", "-account", "bot-jmap"]
```

The sync account must use the same Maildir. Credentials belong in the mail
program's own configuration or environment. `admin` is the permitted sender;
`from` is the bot address. See [configuration](configuration.md) for other programs.

## Run and send an email

```sh
./janeGPT -config=janegpt.toml
```

Send from the configured administrator to the bot:

```text
Subject: Go slices

Explain the difference between a slice and an array, with a short Go example.
```

Jane should reply with `Re: Go slices`, then archive the incoming message.
For a single scan:

```sh
./janeGPT -config=janegpt.toml -interval=0s
```

A single scan processes mail available after synchronization; it does not wait
for a future message. Read status output if no response arrives.

## Enable documentation lookup

Follow [Using Zeal](zeal.md) to install docsets, set `docsets_dir`, and request
explanations with full Markdown and Org exports.
