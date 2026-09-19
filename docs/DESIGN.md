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
- **Writers are stricter than readers.** Every writer (Go, pwsh, zsh) must produce records
  matching [`schema/record.schema.json`](../schema/record.schema.json). A new key or kind goes
  into the schema first. `internal/contract` runs each shell's writer over the shared cases in
  `testdata/contract/cases.json` and checks the Go reader gets back exactly the same record.

### 4.3 Concurrency

Many shells append at once; one compactor occasionally rewrites.

- Appends are single small writes of a whole line. On Windows the appender opens with a
  short-lived exclusive write share and retries on sharing violation (a few ms at most).
- **Unix caveat (found by CI):** .NET on Unix has no `O_APPEND`. `FileMode.Append` seeks to
  the end at open, and sharing is emulated with advisory `flock` (only `FileShare.None` is
  exclusive). pwsh on Linux therefore appends under `LOCK_EX`. Go and zsh append with
  `O_APPEND`, which is atomic per write but ignores that lock, so a pwsh line could in
  theory overwrite one written concurrently by them. Go writers take the same lock with
  C-004; mixed pwsh + zsh on one Linux file is part of S-014.
- Compaction: take `hit.lock`, write `history.jsonl.new`, then re-read any lines appended to
  the old file during the rewrite, append them, atomically replace. **OPEN (C-004)** — needs a
  proper stress test with many shells hammering it.

### 4.4 Scale

~200 bytes/record → 100k commands ≈ 20–30 MB. The Go side loads the whole file into memory
per invocation (tens of ms) and searches in memory. If that ever gets slow: a binary index
cache beside the file, rebuilt from the JSONL (which remains the source of truth).

## 5. Capture (shell side)

### PowerShell (PSReadLine ≥ 2.2)

Loaded from `$PROFILE` with `Invoke-Expression (& hit init pwsh | Out-String)`. The binary
prints `shell/pwsh/hit.ps1` wrapped in a dynamic module (`New-Module hit … | Import-Module
-Global`) and ends with `Enable-Hit -HistoryPath '<path>'`. The binary resolves the path, so
the path rules exist only once (Go). `Disable-Hit` puts everything back; `Remove-Module hit`
unloads it. This is the only process spawn, and it happens once at shell startup.

- **Record on Enter:** `Set-PSReadLineOption -AddToHistoryHandler` receives the **full
  multi-line command** and appends the `cmd` record in-process.
  The handler **chains** whatever handler was installed before (PSReadLine's default, mcfly,
  …) and returns that handler's verdict unchanged, so PSReadLine's own history, Up arrow and
  predictions work as before, and hit can run side by side with mcfly while being trialled.
- **Sensitive commands:** if the chained handler says `MemoryOnly`/`SkipAdding` (PSReadLine's
  default does for lines that look like they hold a password/token/apikey/secret), hit doesn't
  record the line either. C-014 adds hit's own rules.
- **Prompt wrapper:** wraps the current global `prompt` (starship, zoxide's wrapper, …) and
  writes the `end` record (exit code, duration measured from Enter) and a `cd` record when the
  location changed. It captures `$?` first and restores it (`Write-Error -ErrorAction Ignore`)
  before calling the wrapped prompt, so starship's status indicator still sees the real value.
  Exit code: 0 if `$?`; otherwise `$LASTEXITCODE` if non-zero, else 1 (`$LASTEXITCODE` is
  stale after cmdlets).
- **Self-healing:** init scripts that run later can replace the prompt or the handler.
  If a recorded command never reaches hit's prompt hook, the next Enter wraps the prompt
  again. If the handler isn't hit's at prompt time, hit re-registers and chains the newcomer.
  Each wrapper closes over its own predecessor, and the handler has a re-entry guard, so
  mutual wrapping can't loop. The hooks are idempotent, so being reached twice is harmless.
- Record `cwd` as `$PWD.ProviderPath` for FileSystem, so UNC paths are stored as
  `\\server\share\…`, not `Microsoft.PowerShell.Core\FileSystem::\\server\…`.
  Non-filesystem providers (`HKLM:`, `Cert:`) store the PowerShell path (`HKLM:\SOFTWARE`).
- Leading-space commands are not recorded (opt-out convention, like bash `HISTCONTROL`).
- Cost: ~0.8 ms per command (Enter + prompt, two appends) on the owner's machine.

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

**Default keys** (all configurable, all subject to change by use; chosen to avoid the Zellij,
tmux, Windows Terminal and PSReadLine defaults, see §14):

| Key | Action |
|---|---|
| Ctrl+R (from prompt) | open finder, seeded with current buffer |
| Ctrl+R (in finder) | cycle scope: this directory → this session → this host → everything |
| type | fuzzy filter |
| ↑/↓ | move (Ctrl+P/N are **not** defaults: Zellij owns them) |
| Enter | put command in the prompt (don't run) |
| Tab | put command in the prompt and keep editing |
| Ctrl+E | open in `$EDITOR`, result goes back to the prompt |
| Ctrl+F | toggle "as typed" / "tidied" view of the selection (§7) |
| Del | delete from history (tombstone), with undo while finder is open |
| Ctrl+X | toggle "hide failed commands" |
| F1 / `?` on an empty query | key help overlay |
| Esc | cancel, prompt untouched |

Zellij takes most Ctrl/Alt letters, so every default here is provisional until `hit doctor`
(F-019) checks it against the real environment.

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

Because formatting needs the shell's parser, the finder asks the shell to do it: on the tidy key
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

## 13. Testing

The owner uses hit all day and reports what should change. **Changes must never quietly undo
behaviour we've already agreed on.** Every layer is testable from day one, and CI runs all of it on
Windows and Linux.

### 13.1 Build it to be testable

These hooks exist for tests first. Most turn out to be useful features too:

- **Isolation:** `HIT_DATA_DIR` / `HIT_CONFIG` point everything at a temp dir. No test ever
  touches real history.
- **Deterministic time and ids:** the Go core takes a clock and an id source as dependencies.
  `HIT_NOW` (tests only) pins time for shell-side record writing.
- **Non-interactive search:** `hit search --filter <q> --scope dir --print` runs the same
  ranking with no TUI. Tests use it, and so can scripts and fzf users.
- **Logic separate from glue** in the shell scripts: pure functions (`Format-HitCommand`,
  `New-HitRecord`, `Test-HitGuard`, `ConvertFrom-HitSearchResult`) are kept apart from the thin
  PSReadLine/zle bindings. The bindings talk to the line editor through a small adapter
  (`GetBuffer` / `ReplaceBuffer` / `Accept`) that tests replace with a fake.
- **TUI as a pure model:** bubbletea's `Update(msg) → model` is fed key messages in tests with no
  terminal. Rendering is checked separately.

### 13.2 Layers

| Layer | Tool | What it proves |
|---|---|---|
| Go unit | `go test`, table-driven | store merge/tombstones, ranking, dedupe, path normalisation, import parsers |
| Go fuzz | `go test -fuzz` | reader never panics on torn/garbage lines; any command string round-trips byte-exact |
| Go golden | `testdata/*.jsonl` → expected output files | ranking/search results don't drift unintentionally (`-update` flag to re-bless) |
| Go stress | subprocess test | N processes appending while `compact` runs → no lost or duplicated records |
| TUI | model tests + `teatest` golden views | key → behaviour, including narrow-pane layouts |
| **Contract** | JSON Schema in `schema/` + shared fixtures | pwsh and zsh writers produce records the Go reader accepts, and the search result handoff parses on both sides |
| pwsh | **Pester 5** (pwsh 7 on Windows *and* Linux) | record building, UNC/provider paths, guards, handlers against a fake line editor |
| Formatter corpus | Pester, `tests/corpus/pwsh/*.ps1` | each case: expected tidied output **and** token-equality with the original |
| zsh | zsh test scripts run with `zsh -f` (zunit if it earns its place) | hooks, widgets against a fake `BUFFER`, `${(z)}` formatter |
| End-to-end smoke | Go + pty (ConPTY on Windows via `ActiveState/termtest`, creack/pty on Linux) | a real shell with the real module: type a 4-line command, Enter, record lands verbatim; Ctrl+R returns it |

### 13.3 Regression workflow

1. The owner reports something from daily use ("this Elastic command came back mangled").
2. **First, a failing test or fixture.** The exact command goes into the corpus or golden files.
3. Fix, then commit with the wishlist/bug ID.
4. The corpus only grows, so daily use gradually becomes the test suite.

**Private corpus:** `hit` can run the formatter/round-trip checks over the owner's *real*
history locally (`Invoke-HitSelfTest`). It's never committed, but it catches the odd real-world
commands a hand-written corpus misses.

### 13.4 CI

GitHub Actions matrix `windows-latest` + `ubuntu-latest` (macOS later):
`go vet` + `go test` (+ short fuzz run) · Pester on pwsh 7 · zsh tests (Linux) · e2e smoke.
Every push and PR, no merge on red.

## 14. Environments, multiplexers and keys

Typical setup: Windows Terminal → Zellij → pwsh / WSL zsh / msys2 bash in different panes.
hit must feel the same everywhere and must not fight other tools for keys.

### 14.1 Keys

- **Few global bindings.** At the prompt hit claims only two keys: **Ctrl+R** (history)
  and **Alt+C** (directories). Everything else lives *inside* the finder, where only the terminal and
  multiplexer can still take a key first.
- **Known key owners** (the defaults avoid these):
  - Zellij (normal mode): Ctrl+G/P/T/N/H/S/O/Q, Alt+N, Alt+F, Alt+H/J/K/L/arrows, Alt+[/], Alt+I/O, Alt+=/-
  - tmux: prefix Ctrl+B
  - Windows Terminal: Alt+Enter, Ctrl+Shift+*, Alt+Shift+*
  - PSReadLine (Windows mode): many Ctrl/Alt defaults, re-bound only for our two keys
- **Keymap is data** (F-018): all keys come from `config.toml`, so a clash is a config
  change, not a code change.
- **`hit doctor`** (F-019) detects the environment (`$ZELLIJ`, `$TMUX`, `$WT_SESSION`,
  `$TERM_PROGRAM`, WSL, msys2) and reports clashes between hit's keymap and the known
  defaults of whatever it's running under.
- **Leader key: not yet.** A tmux-style leader inside the finder (e.g. Ctrl+Space then a letter) is
  kept as an option (F-022). The keymap design lets us add it without a rewrite, and we'll only add it if
  real clashes pile up.
- **No-key fallback:** typing `h` (or `hit`) always opens the finder, for when some layer eats
  the key.

### 14.2 Several shells, several environments

- **Shell family filter:** by default the finder shows commands from the *current* shell family
  (pwsh vs POSIX shells), and a scope toggle includes everything. Pasting a bash one-liner into pwsh
  is rarely what you want.
- **WSL / msys2 / Windows sharing one history** needs C-011 (per-host/per-env files). Each
  environment appends only to its own file (`history-<host>-<env>.jsonl`) in a shared dir and
  reads all of them. There's no cross-boundary file locking, which matters because locking over WSL's
  `/mnt/c` is unreliable.
- **Path translation:** `/mnt/c/dev`, `/c/dev` (msys2) and `C:\dev` are the same directory.
  The directory finder shows each path in the current environment's form (F-021).

### 14.3 Terminal differences

- Works in Windows Terminal, conhost, Zellij, tmux, over SSH. No mouse needed.
- Respects `NO_COLOR`; falls back to 16 colours.
- **Inline mode** (draw N lines under the prompt) vs **full-screen** (alternate screen) is
  configurable. In a small Zellij pane, inline is usually nicer (F-020).
- Layout adapts to pane size: preview beside the list when wide, below when narrow, hidden when tiny.
