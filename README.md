# janeGPT

![Logo](logo.avif)

An Ollama bot made for E-mail. You can ask local models stuff via e-mail.

## Usage

```
./janeGPT --help
```
```
Usage of ./janeGPT:
  -admin string
        only accepted sender address unless -reply-anyone is enabled
  -archive string
        Archive Maildir path, relative to -maildir unless absolute (default "Archive")
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
  -model string
        Ollama model (default "llama3.2")
  -msmtp string
        msmtp executable (default "msmtp")
  -msmtp-account string
        optional msmtp account name
  -offlineimap string
        offlineimap executable (default "offlineimap")
  -ollama-url string
        Ollama base URL (default "http://127.0.0.1:11434")
  -personality string
        system/personality prompt sent to Ollama
  -reply-anyone
        reply to any sender instead of only the configured admin
  -subject string
        static subject for replies (default "Ollama response")

```