### Zeal docsets and external tools

[#zeal-docsets-and-external-tools](#zeal-docsets-and-external-tools)

Jane can answer email questions by pulling in real documentation instead of
relying only on the model's training data. This works two ways:

1. **Built-in Zeal integration** (`zeal_docs`) — reads docsets you've already
   installed in [Zeal](https://zealdocs.org), converts the matching pages with
   Pandoc, and attaches complete exports to the reply.
2. **Custom tools** — any external program that speaks a small JSON
   protocol (see [Writing your own tool](#writing-your-own-tool) below).

Both go through the same two-step flow: Ollama first decides *whether* a tool
is needed and with *what inputs*, the tool runs, and a second Ollama request
turns the tool's output into the actual reply. Normal conversational email
never triggers a tool call.

#### Prerequisites

[#zeal-prerequisites](#zeal-prerequisites)

- [Zeal](https://zealdocs.org) installed, with at least one docset downloaded
  from inside Zeal (Zeal → Preferences → Docsets → Available).
- [Pandoc](https://pandoc.org) installed and on `PATH` (or point `-pandoc` at
  its location — see [Custom helper limits](#custom-helper-limits)).
- Jane does **not** install, update, or download docsets itself. It only
  reads whatever is already sitting in Zeal's docset storage directory.

Zeal's docset storage directory is shown in Zeal → Preferences → Docsets, at
the bottom of the window. Typical defaults:

| Platform | Typical docset path |
| --- | --- |
| Linux | `~/.local/share/Zeal/Zeal/docsets` |
| macOS | `~/Library/Application Support/Zeal/Zeal/docsets` |
| Windows | `%LOCALAPPDATA%\Zeal\Zeal\docsets` |

Confirm the exact path in Zeal's own preferences pane rather than assuming
one of the above — it varies by install method (package manager vs. AppImage
vs. Flatpak, for example).

#### Enabling it

[#zeal-enabling](#zeal-enabling)

Add the docset directory and a context budget to `janegpt.toml`:

```toml
docsets_dir = "~/.local/share/Zeal/Zeal/docsets"
max_tool_context_bytes = 131072
```

`docsets_dir` left empty (the default) disables the built-in tool entirely —
no docset content is ever read or attached. `max_tool_context_bytes` caps how
much *selected* reference text (from every tool called in that email, not
just `zeal_docs`) is sent to Ollama for the final answer; it does not cap the
size of the attached `.md`/`.org` files, which are governed separately (see
[Limits](#zeal-limits) below).

#### Using it by email

[#zeal-usage](#zeal-usage)

Send Jane a normal email naming the docset and what you want explained.
Jane matches the docset name exactly against what's installed (case-sensitive,
matching the name Zeal shows), so use the same spelling Zeal uses.

**Example 1 — subject only:**

> **Subject:** Python: explain pathlib with examples

**Example 2 — subject + body:**

> **Subject:** Async question
>
> Use the Python docset. Explain how `asyncio.gather` differs from
> `asyncio.wait`, and give two short code examples.

**Example 3 — a different docset:**

> **Subject:** PostgreSQL: window functions
>
> Use the PostgreSQL docset. Explain `ROW_NUMBER()` vs `RANK()` vs
> `DENSE_RANK()` with a worked example over a small table.

**Example 4 — no tool needed:**

> **Subject:** What's a good name for a houseplant?

This last one triggers zero tool calls — Ollama recognizes it as ordinary
conversation and answers directly, with no docset lookup and no attachments.

In every case where a docset *is* used, the reply body contains a short,
example-driven explanation grounded in the selected excerpt, and two
attachments land on the email: `<Docset>.md` and `<Docset>.org`, each a
complete Pandoc conversion of every HTML page in that docset — not just the
pages relevant to your question. Expect these to be large for big docsets;
see [Limits](#zeal-limits).

Only the subject and body are shown to the tool-selection step — Jane never
lets attached files or fetched webpages influence which docset gets chosen.

#### What actually happens (for the curious)

[#zeal-mechanics](#zeal-mechanics)

1. Ollama receives the tool catalog (JSON) plus your subject/body and returns
   a JSON tool plan, e.g.:

   ```json
   {"calls": [{"name": "zeal_docs", "arguments": {"docset": "Python", "query": "pathlib examples"}}]}
   ```

2. Jane validates that `zeal_docs` is a real configured tool and that
   `docset`/`query` are its only declared inputs, then runs the helper:

   ```
   printf '%s' '{"docset":"Python","query":"pathlib examples"}' \
     | ./janeGPT docset -root=/path/to/docsets
   ```

3. The helper walks every `.html`/`.htm`/`.xhtml` page under that docset's
   `Contents/Resources/Documents`, refusing symlinks and non-UTF-8 pages
   outright rather than skipping them silently. It builds two complete Pandoc
   exports (Markdown and Org) of the whole docset, then separately ranks
   pages by keyword overlap with your query to select a smaller excerpt for
   the model context. It returns one JSON object on stdout:

   ```json
   {
     "context": "Docset: Python. Exported all 214 HTML pages to Python.md and Python.org. Selected reference excerpts follow (may be incomplete). Explain the requested topic with practical examples.\n\n<ranked excerpt...>",
     "attachments": [
       {"name": "Python.md", "content_type": "text/markdown", "text": "<full export>"},
       {"name": "Python.org", "content_type": "text/org", "text": "<full export>"}
     ]
   }
   ```

4. That `context` (truncated to `max_tool_context_bytes` if needed, with a
   `[Tool context truncated; exports are complete.]` note appended) is fed
   into a second Ollama request that writes the actual reply. The two
   attachments are passed through untouched.

5. The whole exchange is wrapped in a fixed system instruction telling the
   model that docset content, filenames, and tool output are untrusted
   reference data, not instructions — so text embedded in a docset page can't
   redirect the bot.

Jane never claims the model read every page: only the ranked excerpt reaches
the prompt, while the attachments carry the complete docset text.

#### Command-line use (without email)

[#zeal-cli](#zeal-cli)

The same helper the bot calls internally is a normal subcommand, useful for
testing a docset before wiring it into email:

```
# List installed docsets as a JSON array
./janeGPT docset -root=~/.local/share/Zeal/Zeal/docsets -list
```

```json
["Python", "PostgreSQL", "Go"]
```

```
# Run one export/query manually
printf '%s' '{"docset":"Go","query":"error wrapping"}' \
  | ./janeGPT docset -root=~/.local/share/Zeal/Zeal/docsets
```

This prints the same `{"context": ..., "attachments": [...]}` JSON that Jane
would feed to Ollama — handy for checking that a docset converts cleanly and
that your query terms actually surface useful pages before you rely on it in
production.

#### Custom helper limits

[#zeal-custom-limits](#zeal-custom-limits)

`docsets_dir` alone uses fixed defaults: 48 MiB of source HTML, 48 MiB per
export, and a nine-minute total timeout, wrapped in a ten-minute/128 MiB tool
envelope. To change any of these — e.g. because you run a docset larger than
48 MiB, or Pandoc isn't on `PATH` — leave `docsets_dir` empty and register
`zeal_docs` explicitly as a normal tool instead:

```toml
[[tools]]
name = "zeal_docs"
description = "Read installed Python documentation; docset must be Python. query is the requested topic."
command = ["/home/you/bin/janeGPT", "docset", "-root=/path/to/docsets", "-pandoc=/usr/local/bin/pandoc", "-max-bytes=67108864", "-context-bytes=131072", "-timeout=15m"]
inputs = ["docset", "query"]
timeout = "16m"
max_output_bytes = 201326592
```

Note the description here is doing real work: it's what Ollama sees when
deciding whether to call this tool, so naming the exact docset and what
`query` means measurably improves tool-selection accuracy. If you register
several docset-backed tools this way (see below), give each a distinct,
specific description — a vague one increases the odds Ollama picks the wrong
tool or none at all.

You can register the same helper under several names if you want per-docset
timeouts or size caps — for instance, a large docset like MDN alongside a
small one:

```toml
[[tools]]
name = "mdn_docs"
description = "Read installed MDN Web Docs documentation. docset must be MDN. query is the requested topic."
command = ["/home/you/bin/janeGPT", "docset", "-root=/path/to/docsets", "-max-bytes=209715200", "-timeout=25m"]
inputs = ["docset", "query"]
timeout = "26m"
max_output_bytes = 419430400

[[tools]]
name = "go_docs"
description = "Read installed Go standard library documentation. docset must be Go. query is the requested topic."
command = ["/home/you/bin/janeGPT", "docset", "-root=/path/to/docsets"]
inputs = ["docset", "query"]
```

A tool named `zeal_docs` cannot coexist with a non-empty `docsets_dir` —
janeGPT rejects that configuration at startup, since it would be ambiguous
which one wins.

#### Writing your own tool

[#writing-your-own-tool](#writing-your-own-tool)

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

Emailing the admin with subject `Changelog: what changed in 2.4.0?` now
causes Ollama to select `changelog_lookup` with
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

#### Limits

[#zeal-limits](#zeal-limits)

| Limit | Applies to | Default |
| --- | --- | --- |
| `max-bytes` (helper flag) | Total source HTML read per docset export | 48 MiB |
| Per-export size | Each of `.md`/`.org` | 48 MiB |
| `timeout` (helper flag) | Total docset export time | 9 minutes |
| Built-in `zeal_docs` envelope timeout | Whole tool call, incl. helper | 10 minutes |
| Built-in `zeal_docs` envelope output | JSON result from helper | 128 MiB |
| `max_tool_context_bytes` | Combined selected excerpt sent to Ollama, across all tools called in one email | 131072 bytes (128 KiB) |
| Custom tool `timeout` | Per tool, if you register one yourself | 2 minutes |
| Custom tool `max_output_bytes` | Per tool, if you register one yourself | 32 MiB |

A docset that exceeds `max-bytes` or the timeout fails outright — the helper
never sends a partial export. MIME base64 encoding adds roughly a third to
attachment size on the wire, so a 48 MiB export can become ~64 MiB in the
actual email; check this against your mail server's message size limit
before relying on very large docsets.

#### Troubleshooting

[#zeal-troubleshooting](#zeal-troubleshooting)

- **"docset ... is not installed; available: ..."** — the name in your email
  doesn't exactly match a folder Zeal created (case-sensitive). Run
  `./janeGPT docset -root=... -list` to see the exact names Jane recognizes.
- **"docset contains a symlink or invalid Documents directory"** — Zeal
  docsets are occasionally packaged with symlinked resources; Jane refuses
  these rather than following them outside the docset root. Reinstall the
  docset from Zeal, or file the mismatch with the docset's maintainer.
- **No tool call happens at all** — check that your subject/body actually
  reads as a documentation request; Ollama sees only that text. Being
  explicit ("Use the Python docset.") is more reliable than an implicit
  reference.
- **Reply arrives with no attachments** — attachments are only produced by
  tools that set `attachments` in their JSON result; a custom tool (like the
  changelog example above) is free to return context only.
- **Pandoc errors** — Jane runs Pandoc with `--sandbox`, which disallows
  reading files outside its input; if a docset page relies on external
  includes this can fail. Point `-pandoc` at a working install and confirm
  `pandoc --from=html --to=gfm` succeeds by hand on a sample page.

Format references: [Zeal usage](https://zealdocs.org/usage.html),
[Dash docset structure](https://kapeli.com/docsets), and
[Pandoc's manual](https://pandoc.org/MANUAL.html).
