# hit — design

> **hit** = **hi**story **t**ool. A fast, multi-line-aware shell history and directory finder
> for PowerShell (first) and zsh (second), built for people who live in the terminal.

This is a living document. Decisions that are settled are stated plainly; open questions are
marked **OPEN** and tracked in [WISHLIST.md](WISHLIST.md).

---

## 1. Why

Existing tools each get close and then fail in a way that matters every day:

| Tool | What's good | What hurts |
|---|---|---|
| mcfly | Ctrl+R finder, context-aware ranking | Multi-line PowerShell commands are mangled or lost |
| atuin | Beautiful, rich metadata | SQLite backing store; "can't open connection to db" errors; heavy for the job |
| PSReadLine history / predictor | Built in, handles multi-line | Weak search, plain text file, no metadata, no deletion UX |
| z / zoxide | Frecency directory jumping | Useless on network shares (`\\elastic-prod-1\logs`): stats paths, slow or prunes them |

**hit's promise:** *you never lose something you typed, and you never lose somewhere you went.*

## 2. Principles

1. **Ergonomics over theory.** This will be used thousands of times a day. Every keystroke
   and every millisecond is a feature. Decisions are judged by using them, not by arguing them.
2. **Iterate fast.** Go, plain files, thin shell scripts. No daemon, no database, no service.
3. **Verbatim is sacred.** The stored command is exactly what was typed, byte for byte.
   Any prettifying is a *view* or an explicit *action*, never a mutation of the record.
4. **Never block the prompt.** Recording a command must add no perceptible latency.
   Nothing on the hot path touches the network or stats a remote path.
5. **Plain data.** History is a human-readable, append-only JSONL file you can grep, back up,
   sync, or query with other tools (e.g. `sql-cli`).
6. **PowerShell first, portable always.** Built and dogfooded on pwsh/Windows, but nothing
   may be designed in a way that can't port to zsh/Linux (or pwsh on Linux).
7. **Fail safe.** If anything in hit breaks, the shell still works and the command still runs.

## 3. Architecture

```
 ┌────────────── shell process (pwsh / zsh) ──────────────┐
 │  hit.psm1 / hit.zsh  (thin integration)                │
 │   • record hook  ──── append JSONL ───────────────┐    │
 │   • prompt hook  ──── append "end"/"cd" records ──┤    │
 │   • Enter guard  (prod safety rules, in-process)  │    │
 │   • key handlers ──── spawn `hit search` ───┐     │    │
 │   • formatter (native parser)               │     │    │
 └─────────────────────────────────────────────┼─────┼────┘
                                               ▼     ▼
                        ┌──────────── hit (Go binary) ───────────┐
                        │ search TUI · dirs TUI · rm · compact   │
                        │ import · stats · init <shell>          │
                        └──────────────────┬─────────────────────┘
                                           ▼
                                  history.jsonl  (+ config.toml)
```

**Split of responsibilities**

- **Shell side writes.** Appending a record happens in-process (.NET file append in pwsh, a
  backgrounded write in zsh) so there is no process spawn on Enter.
- **Go side reads and maintains.** Search, ranking, TUI, deletion, compaction, import.
- **Shell side parses shell syntax.** Only the shell can parse itself reliably, so formatting
  (§7) and anything syntax-aware lives in the integration script, using the shell's own parser.
- `hit init pwsh` / `hit init zsh` prints the integration script (the atuin/zoxide pattern),
  so the script and the binary version always match: `Invoke-Expression (& hit init pwsh | Out-String)`.

## 4. Storage

### 4.1 Location

| | Default | Override |
|---|---|---|
| Data | `%LOCALAPPDATA%\hit\` · `~/.local/share/hit/` | `HIT_DATA_DIR` |
| Config | `%APPDATA%\hit\config.toml` · `~/.config/hit/config.toml` | `HIT_CONFIG` |

**OPEN (C-011):** one file per host (`history-<host>.jsonl`) so the directory can be synced
(Syncthing/OneDrive/git) without write conflicts, with the reader merging all files.

### 4.2 Format — JSONL, append-only

One JSON object per line. JSON escapes newlines, so multi-line commands are safe and
round-trip exactly. Short keys keep the file compact.

```jsonc
// command started (written by the record hook, before execution)
{"k":"cmd","id":"01J8Z…","ts":"2026-09-19T10:12:03.412Z","cmd":"Invoke-RestMethod `\n    -Uri …","cwd":"\\\\elastic-prod-1\\logs","sh":"pwsh","host":"box1","sid":"a1b2c3"}
// command finished (written by the prompt hook)
{"k":"end","id":"01J8Z…","exit":0,"ms":412}
// directory visited (written by the prompt hook when cwd changes)
{"k":"cd","ts":"…","dir":"\\\\elastic-prod-1\\logs","sh":"pwsh","host":"box1","sid":"a1b2c3"}
// deletion (written by `hit rm` / Del in the finder)
{"k":"del","id":"01J8Z…","ts":"…"}
```

- `id` is a ULID: sortable, unique across hosts without coordination.
- **Two-phase cmd/end** because pwsh's history hook fires *before* execution. Writing
  immediately means a command is never lost, even if the window is closed mid-run; exit code
  and duration arrive later and are merged by id. A `cmd` without an `end` is simply "unknown".
- **Deletion is a tombstone.** The reader hides deleted ids immediately; `hit compact` rewrites
  the file without them (and without their `end` records). Compaction also runs opportunistically.
- **Reader is tolerant.** Unknown keys are ignored; a torn/corrupt line is skipped, never fatal.
- Unknown `k` values are ignored, so new record kinds can be added without breaking old readers.

### 4.3 Concurrency

Many shells append at once; one compactor occasionally rewrites.

- Appends are single small writes of a whole line. On Windows the appender opens with a
  short-lived exclusive write share and retries on sharing violation (a few ms at most).
- Compaction: take `hit.lock`, write `history.jsonl.new`, then re-read any lines appended to
  the old file during the rewrite, append them, atomically replace. **OPEN (C-004)** — needs a
  proper stress test with many shells hammering it.

### 4.4 Scale

~200 bytes/record → 100k commands ≈ 20–30 MB. The Go side loads the whole file into memory
per invocation (tens of ms) and searches in memory. If that ever gets slow: a binary index
cache beside the file, rebuilt from the JSONL (which remains the source of truth).

## 5. Capture (shell side)

### PowerShell (PSReadLine ≥ 2.2)

- `Set-PSReadLineOption -AddToHistoryHandler` receives the **full multi-line command**.
  Write the `cmd` record there and return `$true` so PSReadLine's own history still works.
- `prompt` function wrapper: writes the `end` record (`$?`, `$LASTEXITCODE`, duration from
  `Get-History -Count 1`) and a `cd` record when `$PWD` changed.
- Record `cwd` as `$PWD.ProviderPath` for FileSystem so UNC paths are stored as
  `\\server\share\…`, not `Microsoft.PowerShell.Core\FileSystem::\\server\…`.
  Non-filesystem providers (`HKLM:`, `Cert:`) are stored as-is with the provider name.
- Leading-space commands are not recorded (opt-out convention, like bash `HISTCONTROL`).

### zsh

- `zshaddhistory` hook gets the full command → `cmd` record.
- `precmd` → `end` record (`$?`, duration via `$EPOCHREALTIME`); `chpwd` → `cd` record.
- Writes are backgrounded (`&!`) so the prompt never waits.
- Distributed as an antidote-compatible plugin.

### Secret redaction (both)

Commands matching configured patterns (API keys, `Authorization:` headers, `-Password`) are
redacted or not recorded at all, before they touch disk. See §9 for how rules reach the shell.

## 6. Search (the finder)

Spawned by a key handler: `hit search --query "<current buffer>" --out <tmpfile>`.
The TUI draws on the console, and the chosen command is written to the temp file; the key handler
reads it and replaces the prompt buffer. (Using a temp file rather than stdout keeps the TUI's
terminal I/O clean on Windows.)

**Default keys (all subject to change by use):**

| Key | Action |
|---|---|
| Ctrl+R (from prompt) | open finder, seeded with current buffer |
| type | fuzzy filter |
| ↑/↓, Ctrl+P/N | move |
| Enter | put command in the prompt (don't run) |
| Tab | put command in the prompt and keep editing |
| Ctrl+E | open in `$EDITOR`, result goes back to the prompt |
| Alt+F | toggle "as typed" / "tidied" view of the selection (§7) |
| Del | delete from history (tombstone), with undo while finder is open |
| Ctrl+G | cycle scope: this directory → this session → this host → everything |
| Ctrl+X | toggle "hide failed commands" |
| Esc | cancel, prompt untouched |

- **Multi-line is first class**: the list shows the first line with a `⏎ +3` marker, and a
  preview pane shows the full command, syntax-highlighted.
- **Ranking:** match quality × recency × frequency, boosted for same directory and
  successful exit. Duplicates collapse into one entry showing a run count and last-run time.

## 7. Formatting ("tidy")

Long single-line commands can be shown or inserted with one argument per line.
**Always a view or an insert option; the stored record is never changed.**

PowerShell, in `hit.psm1`, using PowerShell's own parser:

1. `[System.Management.Automation.Language.Parser]::ParseInput` → tokens + errors.
   Parse errors or already multi-line → return unchanged.
2. Before each **top-level** `Parameter` token (not inside strings, here-strings, `{}`, `@{}`,
   `()`, `$()`), insert `` ` `` + newline + indent. Never leave whitespace after the backtick.
3. **Check:** re-parse the result and compare the token streams, ignoring whitespace and
   line-continuation tokens. Any difference → return the original. Worst case is "not tidied",
   never "broken".

Pipelines: break before each `|` with a four-space indent (no backtick needed after a trailing `|`).

zsh does the same thing with `${(z)cmd}` (zsh's own lexer), inserting `\` + newline.

**Later (F-011):** convert to splatting (`$p = @{…}; Cmd @p`) for commands that are longer still.

Because formatting needs the shell's parser, the finder asks the shell to do it: on Alt+F
the TUI returns `{action: "format", cmd}` and the key handler formats in-process and
re-opens the finder at the same position. **OPEN (F-006):** or pre-compute tidied forms
in the record hook and store them as `fmt`. Try both and keep whichever feels better.

## 8. Directory navigation

The same data powers "where have I been". Every prompt that changes directory writes a `cd` record, and
every `cmd` carries its `cwd`, so directory frecency comes for free.

**Network shares, where z/zoxide fall down:**

- **Never stat on the query path.** Ranking and display use recorded data only. Showing
  `\\elastic-prod-1\logs` costs nothing even if the server is down.
- Existence is checked only for the *selected* entry at jump time, with a short timeout, and
  a missing path is reported, never silently pruned. Pruning is manual or explicit.
- Windows paths are matched case-insensitively and normalised (trailing `\`, `/` vs `\`).
- Mapped drive letters and UNC paths are both kept as the user typed them. **OPEN (C-012):**
  also resolve `Z:\` → `\\server\share` so both find each other.

**UX:**

- `Alt+C` → directory finder (fuzzy over path segments, frecency ranked); Enter = `Set-Location`.
- `hit cd <terms>` / short alias (e.g. `j elastic logs`) → best match, no UI.
- Pivot: from a directory in the finder, show *commands run there*, and from a command, jump to
  the directory it was run in. What you typed and where you typed it are the same dataset.

## 9. Safety guards (prod failsafes)

Avoid sending the wrong thing to prod on a Monday morning.

Rules live in `config.toml`:

```toml
[[guard]]
name    = "prod elastic writes"
match   = 'elastic-prod-\d+'                       # regex over the whole command
and     = '(?i)-Method\s+(Post|Put|Delete|Patch)'   # optional second condition
action  = "confirm"        # warn | confirm | block
confirm = "prod"           # word you must type to proceed
```

- **PowerShell:** Enter is re-bound to a handler that checks the buffer against the rules
  *in-process* before calling `AcceptLine`. `warn` shows a banner and accepts;
  `confirm` asks for the confirmation word; `block` refuses.
- **zsh:** wrap the `accept-line` widget the same way.
- Recalled commands that match a guard are flagged in the finder (red marker) *before* you
  insert them.
- Rules are compiled to shell-native code by `hit init <shell>`. Checking runs on every Enter,
  so it can't spawn a process. **OPEN (C-013):** reload on config change without restarting the shell.
- A guard failing to evaluate (bad regex) must **not** block the command. It warns instead
  (principle 7).

## 10. Import

`hit import <source>`, idempotent (re-running never duplicates):

- PSReadLine `ConsoleHost_history.txt` (multi-line entries end lines with a trailing `` ` ``)
- mcfly (SQLite; a pure-Go reader is needed for import only)
- zsh `EXTENDED_HISTORY` (`: <ts>:<dur>;cmd`, `\`-continued lines)
- atuin (SQLite)

Imported records carry `"src":"psreadline"` etc. and no exit code.

## 11. Non-goals (for now)

- A sync server or accounts. The data is a file, so use any file sync.
- Bash/fish/nushell. Contributions welcome once the pwsh + zsh shape is settled.
- Replacing PSReadLine. hit plugs into it.

## 12. Tech choices

- **Go**: fast iteration, single static binary, trivial cross-compile, ~10–20 ms startup.
- **TUI:** bubbletea + lipgloss (charmbracelet). Works on Windows Terminal and conhost via VT.
- **Fuzzy:** start with `sahilm/fuzzy` or a simple in-house scorer, and tune it by use.
- **Config:** TOML (`BurntSushi/toml` or `pelletier/go-toml/v2`).
- **Optional later:** a small C# `ICommandPredictor` assembly (S-020) that reads the same file,
  for PSReadLine's inline/list predictions.
