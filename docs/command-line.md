[Documentation home](../README.md)

# Command-line reference

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

## Docset helper

```sh
./janeGPT docset -help
./janeGPT docset -root=/path/to/docsets -list
```

| Option | Default | Meaning |
| --- | --- | --- |
| `-root` | Required | Zeal docset storage directory |
| `-list` | `false` | Print installed names as JSON without exporting |
| `-pandoc` | `pandoc` | Converter executable |
| `-max-bytes` | `50331648` | Source HTML plus labels, and each complete export |
| `-context-bytes` | `131072` | Selected reference text before metadata |
| `-timeout` | `9m` | Total export timeout |

The helper reads JSON on stdin and prints JSON on stdout. It does not send email
or call Ollama when invoked directly. See [manual export](zeal.md#command-line-use-without-email).
