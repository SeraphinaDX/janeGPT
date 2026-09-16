[Documentation home](../README.md)

# External tools

For Zeal setup and email examples, start with [Using Zeal](zeal.md).

## Register a tool

Place tool tables at the end of `janegpt.toml`, after all top-level keys:

```toml
[[tools]]
name = "local_reference"
description = "Search local reference notes using query."
command = ["/home/you/bin/local-reference", "--json"]
inputs = ["query"]
timeout = "2m"
max_output_bytes = 33554432
```

This requires your own `local-reference` program. The description tells the
model when to use it. Inputs are JSON strings sent on stdin, not shell arguments.
Names start with a letter and contain letters, digits, or underscores, up to
64 characters. Tool names must be unique; each tool's input names must be unique.

## Protocol and worked example

Any external program can back a tool, not just the docset helper. The
protocol is deliberately small:

- Jane invokes your `command` array exactly as written (no shell), passing a
  JSON object of your declared `inputs` on stdin, e.g. `{"query": "..."}`.
- Your program writes **one** JSON object to stdout:
  `{"context": "...", "attachments": [...]}`. `attachments` is optional.
- Diagnostics go to stderr; a nonzero exit is treated as failure (its stderr
  is included in the error Jane reports and retries against, up to
  `max_attempts`).
- Only UTF-8 `.md` or `.org` attachments with plain filenames (no path
  separators) are accepted; Jane sets their MIME type from the extension and
  rejects anything else.

**Worked example — a "changelog lookup" tool** backed by a small shell
script that greps a local `CHANGELOG.md`:

```toml
[[tools]]
name = "changelog_lookup"
description = "Search this project's CHANGELOG.md for a version or keyword. version_or_keyword is what to look for."
command = ["/home/you/bin/changelog-tool.sh"]
inputs = ["version_or_keyword"]
timeout = "30s"
max_output_bytes = 65536
```

```bash
#!/bin/sh
# changelog-tool.sh — reads {"version_or_keyword": "..."} on stdin,
# writes {"context": "..."} on stdout.
set -eu
term=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["version_or_keyword"])')
match=$(grep -A5 -i -F -- "$term" /srv/myproject/CHANGELOG.md || true)
if [ -z "$match" ]; then
  match="No changelog entry found for '$term'."
fi
python3 -c 'import json,sys; print(json.dumps({"context": sys.argv[1]}))' "$match"
```

Emailing Jane from an allowed sender with subject `Changelog: what changed in 2.4.0?` asks Ollama to select `changelog_lookup` with
`{"version_or_keyword": "2.4.0"}`, run the script, and fold the matched
`CHANGELOG.md` section into the reply — with no attachment, since this tool
never sets `attachments`.

A few things worth internalizing before writing your own:

- **Tools cannot chain.** A tool's output never triggers another tool call —
  the model only gets one tool-selection pass (up to three calls) per email.
- **Inputs are always strings**, and every declared input is required on
  every call — there's no optional-argument concept in the protocol.
- **Treat tool output as untrusted** in your own thinking the same way Jane
  does: the fixed system prompt tells Ollama not to follow instructions
  found inside tool context, but your script should still avoid echoing
  attacker-controlled email content back verbatim into a shell command
  without quoting it, since `command` arrays run without a shell but your
  own script might still shell out internally.
- **Read-only and idempotent is safest.** Tools run with Jane's own account
  permissions and can run again automatically on retry, so a tool with side
  effects (e.g. one that writes files or calls a paid API) could fire more
  than once for the same email if a later step in the pipeline fails and
  triggers a retry.
- Tool defaults if you omit them: `timeout = "2m"`, `max_output_bytes = 33554432` (32 MiB).

## Execution limits

Jane makes one selection request followed by a final answer request. Selection
sees the subject and body, not attached documents or fetched webpages. At most
three tool calls run; output cannot trigger more tool calls.

`max_tool_context_bytes` limits combined model reference text, not attachments.
`max_output_bytes` limits each command's entire JSON result, including attachments.
Output overflow terminates the command; stderr is limited to 16 KiB. Failures
enter [retry and quarantine handling](configuration.md#retries-and-delivery-state).

There is no interactive confirmation step. Configure only commands appropriate
for your allowed senders. Tool output is marked as untrusted reference material
in the system prompt; this instruction is not a guarantee against prompt injection.
