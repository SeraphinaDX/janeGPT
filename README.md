# janeGPT

![Logo](logo.avif)

Ask local Ollama models questions by email. Jane reads a Maildir, uses configured
tools and reference material, and replies through your chosen mail program.

## Start here

1. [Build and configure Jane](docs/quick-start.md).
2. [Enable Zeal and try documentation requests](docs/zeal.md).
3. [Adjust mail commands, limits, and retries](docs/configuration.md).

## Documentation

| Guide | Contents |
| --- | --- |
| [Quick start](docs/quick-start.md) | Requirements, build, MailSalonSync setup, first email |
| [Capabilities](docs/capabilities.md) | Full feature list and current limitations |
| [Configuration](docs/configuration.md) | TOML, mail commands, overrides, delivery state and retries |
| [Using Zeal](docs/zeal.md) | Docset setup, example emails, exports, troubleshooting |
| [External tools](docs/external-tools.md) | Registration, JSON protocol, worked tool example |
| [Command-line reference](docs/command-line.md) | Bot flags and docset helper options |
| [Example configuration](janegpt.example.toml) | All settings in a copyable TOML file |

## How it works

Jane runs your sync command, scans incoming mail, gathers requested reference
material, asks Ollama for an answer, and passes the reply to your send command.
It preserves the incoming subject with `Re:` and maintains reply threading.

Incoming text, Markdown, and Org attachments can supply context. Zeal requests
can return complete Markdown and Org text exports of an installed docset,
alongside an explanation and examples. Other external tools can be configured
through TOML.

The send command receives the complete RFC 5322/MIME email on stdin. Durable
delivery records, bounded retries, and a Failed Maildir support recovery.

See [Capabilities](docs/capabilities.md) for details and [LICENSE](LICENSE)
for licensing.
