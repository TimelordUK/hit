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

Spawned by the Ctrl+R key handler:
`hit search --query "<buffer>" --out <tmpfile> --scope <scope> --cwd <pwd> --session <sid>
--host <host> --shell pwsh`. The TUI draws on the alternate screen **on stderr**, and the
choice is written to the temp file as JSON, so the shell's capture of stdout never disturbs
the display:

```jsonc
{"action":"insert","cmd":"Invoke-RestMethod `\n  -Uri …","id":"01J8Z…","deleted":["01J8Y…"]}
```

`action` is `insert`, `edit` or `cancel`; `deleted` lists ids the finder tombstoned. The key
handler reads the file through a line-editor adapter (`GetBuffer`/`SetBuffer`/`Redraw`) that
tests replace with a fake, so handlers run with no keyboard (DESIGN §13.1).

`hit search --print` runs the same ranking with no TUI and prints one JSON object per result
(C-019): for tests, scripts, and piping into fzf. `--sort rank|recent` reaches C-032 from
there too; every flag has a matching field on the `hit serve` request, so the cold and warm
routes cannot drift apart.

**Keys as built** (all still provisional, to be moved into `config.toml` by F-018):

| Key | Action |
|---|---|
| Ctrl+R (prompt) | open the finder, seeded with the current buffer; bound in both vi modes |
| Ctrl+R (finder) | cycle scope, narrowing a step at a time: all → host → session → dir |
| Alt+D | this directory only ⇄ the scope it was on before |
| Alt+S | sort: rank ⇄ recent |
| type / Backspace / Ctrl+U | filter, delete a character, clear the filter |
| ↑ / ↓ / PgUp / PgDn / Home / End | move (Ctrl+P/N are **not** bound: Zellij owns them) |
| Enter | put the command in the prompt, don't run it |
| Tab | put it in the prompt (`edit`) |
| Ctrl+X | hide/show commands that failed |
| Del / Ctrl+Z | tombstone the selection / undo, while the finder is open |
| Esc / Ctrl+C | cancel, prompt untouched |

**Scope: a cycle and a toggle.** The cycle used to run narrowest-first, and that made it
feel broken. From `all` a single press landed on `dir`, and in a directory with nothing
recorded in it the list went empty at once; getting out again took three more presses,
through `session` and `host`, which in that directory were usually empty too. Two changes,
both from the same report:

- **The cycle narrows one step at a time**: all → host → session → dir. Each press makes
  the list smaller, so you stop as soon as it is small enough, and the first press lands
  somewhere that has rows. The order lives in `search.Scopes` and is pinned by a test.
- **Alt+D is one key in and the same key out**, restoring whatever scope it found (T-008),
  because "what do I run in this folder" is the question asked constantly and it should
  not cost a journey round a cycle.

**Order is a mode, not a better formula (C-032).** Ranking answers *what do I most likely
want next*, and is right nearly always. It is wrong in one specific case: walking back
through the last few things you ran, where frecency actively fights you, because a command
run two hundred times outranks the one you ran once, five minutes ago. **Alt+S** switches
between `rank` and `recent`; matching is untouched, so the same commands match and only
their order changes. The two orders are two different questions, so neither is a default
the other should be tuned into.

**The header names the mode, always — default or not.** An indicator that appears only when
it is switched on teaches nothing about the key that switches it off, and the question the
header answers (*why am I not seeing what I expected?*) is asked precisely when you have
forgotten which mode you are in. Scope and sort are shown permanently, and a non-default
one is drawn as a badge rather than as another grey word. In the directory scope the header
names the *folder*, because in a filter showing one directory's commands, which directory
is the whole of the information.

**An empty list says why it is empty**, and which key widens it. Without that it reads as a
finder that has stopped working — which is exactly how the directory scope was first
reported.

**Matching modes.** A bare query is a smart-case subsequence. A query opening with a single
quote is matched *literally* — `'hit` wants those three runes adjacent. Subsequence matching
is loose by design, and on a real history that shows: `hit` also finds `Get-History`,
`Get-ChildItem` and `Push-Location`, each carrying h, i and t in that order. The quote is
fzf's, where `'wild` is an exact-substring term, so the habit transfers; a quote anywhere
else in the query is an ordinary character, which matters because commands are full of them.
Both modes hand their matched positions to one scoring function, so neither is quietly
favoured in the ranking (C-030). The header names the mode, rather than leaving it to be
inferred from a character that is easy to miss.

**Ranking** (`internal/search`, golden-tested): match quality × frequency (log, so a command
run 500 times doesn't bury one run twice) × recency (halves every 7 days, with a floor) ×
bonuses for this directory (1.5), this session (1.2) and a penalty for a failed exit (0.7).
Matching is smart-case subsequence, scored on contiguity, word boundaries, span and how
early it starts. Duplicates collapse into one result with a run count; the directory and
session bonuses look at every run of that command, not just the latest.

- **Each row carries its time** in a fixed-width column on the left, dimmed so it reads as
  context rather than as part of the command. Today shows the clock, because the job is to
  place a run against the working day — *did I run this before or after the deploy?* — which
  a relative age answers worse the longer the day goes on. Any other day shows the date: the
  minute has stopped mattering and the day has started to. An entry with no usable timestamp
  gets a blank of the same width, so the commands stay in one column. Below a narrow pane the
  time is dropped rather than squeezing the command, which is the thing you came for (T-001).

- **The selected row is a bar across the pane** (T-010). A row is drawn from several styles
  — the time column, the matched runes, the `⏎ +N` marker — and each of them ends with a
  full SGR reset, so wrapping the finished line in a "selected" style painted the highlight
  only as far as the first nested style: in practice the `▸ ` prefix and nothing else. The
  one row you need to find with your eye was the one row with no highlight on it. The
  selected row is therefore drawn from *its own set of styles*, each already carrying the
  highlight, with nothing wrapped round the outside, and it is padded to the full width so
  the selection reads as a band rather than as a highlight that stops wherever the command
  happens to end. Styles on that bar are all lifted a step, because a foreground chosen to
  read as "dim" against the terminal's own background disappears on a coloured one.

- **Multi-line is first class**: the list shows the command flattened onto one line with a
  `⏎ +3` marker, and a preview pane shows it in full.

  The row used to show the *first line only*, which failed on exactly the commands worth
  recalling: a block starting `& {` or `foreach ($x in $y) {` renders as that opener and
  nothing else, so every command of that shape looks identical when you glance down the
  list. Flattening collapses each run of whitespace to one space and keeps a map back to
  the original rune offsets, so match highlighting still lands correctly — and matches on
  later lines, which the old row could never show, now appear. Single-line commands are
  left byte for byte: the view may reformat (principle 3), but not gratuitously.

- **Alt chords are not typed into the filter.** They belong to the shell (Alt+M is the
  prompt's one-line/many-lines toggle) and arrive as ordinary runes, so the finder must
  drop them explicitly or the key silently filters instead of doing nothing.
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

Pipelines: break **after** each top-level `|` (a trailing pipe continues the line on its own,
so no backtick is needed), with each stage's parameters indented one level deeper than the
stage.

**Both directions, on one key (S-023).** `Join-HitCommand` is the reverse: it drops line
continuations and newlines between top-level tokens to bring a command back onto one line
for editing. It refuses when a here-string or any other token owns a newline, because
joining would change what the command means. **Alt+M** at the prompt toggles whichever way
the buffer isn't: split a one-liner, join a continued one. Alt+M is claimed by neither
Zellij, Windows Terminal nor PSReadLine (§14.1). Both directions are checked by re-parsing
and comparing token streams, and a corpus of real commands must survive the round trip
byte-identical (`tests/pwsh/Format.Tests.ps1`).

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

### 9.1 Danger is a command in a context (F-029)

The owner administers many Elastic environments. The mistake they fear most is not a bad
command but a good one in the wrong place: `Set-ElasticEnv PROD` (their own function, which
sets environment variables), then later a command that mutates the stack, sent without
remembering which environment the shell is pointed at. The same line is routine against a
test stack. So danger is not a category (§17, which is static): it is **a mutating command
while the shell's state says prod**, checked when Enter is pressed.

```toml
[[guard]]
name    = "mutating elastic call while pointed at prod"
when    = { env = "ELASTIC_ENV", match = '(?i)^prod' }     # the shell's state, read now
match   = '(?i)-Method\s+(Post|Put|Delete|Patch)|curl\b.*-X\s*(POST|PUT|DELETE|PATCH)'
unless  = '/_(search|count|msearch|mapping|cat)\b'           # POSTs that only read
action  = "confirm"
confirm = "prod"
```

- **`when` reads the live environment in-process** as Enter is pressed: a variable lookup,
  no process, no network, so it is allowed on the hot path. The variable name is the
  owner's to choose: whatever `Set-ElasticEnv` sets.
- **Mutating is the hard half, and false alarms are the real enemy.** A guard that fires on
  every `_search` is soon confirmed without being read, which is worse than no guard at
  all. Hence `unless`: Elastic reads by POST (`_search`, `_count`, `_msearch`), and those
  must pass silently. For the owner's own functions, PowerShell's verbs do the work:
  `Get-`/`Test-`/`Find-` read, while `Set-`/`Remove-`/`New-`/`Update-`/`Clear-`/`Start-`/
  `Stop-`/`Restart-` change things. So a rule can say "an Elastic function with a mutating
  verb" without listing every function.
- **The guard sees only the line typed.** `.\reindex.ps1` looks the same whether the script
  reads or writes, and hit cannot see inside it. Scripts that mutate should defend
  themselves (refuse to run against prod without `-Confirm`); a guard can still catch the
  names of known mutating scripts, but it cannot be the only defence.
- **The finder shows the same verdict before you get to Enter.** A recalled row that would
  trip a guard *in the current state* is marked red when it is drawn — evaluated against
  the shell as it is now, not as it was when the command was recorded. So the same history
  row is red in a prod shell and plain in a test one. That needs the shell to pass the
  guarded variables with the request, which the pipe already has room for.
- **Being in prod should be visible all the time, not only at the guard.** The guard is the
  last line of defence; the first is a prompt that says PROD in red. That belongs in the
  owner's prompt, not in hit, but hit could offer a `Get-HitGuardState` for a prompt to
  call.

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

## 15. Measuring recall latency

Recall is the one interaction that must feel instant (principle 1), and when it doesn't,
the cause is usually not in hit. The finder spans two processes, so neither side can see
the whole of it: the shell knows when it called `Process.Start` but not when the binary
began running, and the binary knows when it began but not when it was asked to.

`HIT_TIMING=1` turns on a phase report on both sides, written to `$TEMP\hit-debug.log`
(the same file as `HIT_DEBUG`, under its own switch so one number doesn't arrive buried
in every key press). The finder also draws its report in the status area, because the
number you want is usually the one for the recall you just did.

The shell stamps the clock as it spawns the binary and passes it as `--started-at`
(unix ms). The binary compares that with its own package initialisation, so the first
phase it reports is **process creation** — the loader, the Go runtime, and on a managed
machine whatever inspects the binary before it is allowed to run. That phase belongs to
neither side's code and is invisible without the handoff.

Phases, in order:

| Side | Phase | What it covers |
|---|---|---|
| pwsh | `temp` | the handoff temp file being created |
| pwsh | `buffer` | reading the prompt buffer out of PSReadLine |
| go | `spawn` | `Process.Start` → the binary's package init (process creation) |
| go | `init` | flag parse and path resolution |
| go | `read` | reading the JSONL history off disk |
| go | `build` | merging records into entries |
| go | `rank` | the first search |
| go | `paint` | the first frame |
| pwsh | `run` | the whole finder session — *includes* your time looking at it |
| pwsh | `readback` | reading and parsing the chosen command |
| pwsh | `redraw` | putting the prompt back |

`hit search --print --started-at <unix-ms>` reports the same phases with no TUI at all,
which isolates process creation from anything terminal-related.

Timing is off unless `HIT_TIMING` is set, and off means a nil timeline on the Go side and
a `$null` one in pwsh — both no-ops, so the hot path carries no checks and no cost.

**Measured 2026-09-21** (Ryzen 7950X, no endpoint security): a day's history — 70 commands,
380 lines, 46 KB — costs ~1 ms of an ~8 ms total, and 24.6k lines costs 37 ms. `tui.New`
plus the first `View` is 38 µs. History size is therefore not a plausible cause of a slow
recall at any size the owner will reach this decade (§4.4). A cold `spawn` measured 206 ms
against 8 ms warm on that machine, which is the phase worth suspecting first elsewhere.

## 16. The resident finder

Measured on a managed work machine (S-029): launching a file the endpoint agent has never
seen costs **~3.5 s**, and ~58 ms once it is known. Ordinary process creation is barely
taxed (50–124 ms against 11–44 on an unmanaged box). So the cost is per *file*, it is
cached, and the cache is evicted through the day — which is exactly what Ctrl+R felt like:
usually fine, sometimes three seconds, worst right after an update. Our own release cadence
feeds it, because every version shipped is a new unknown file. Excluding the install
directory would have fixed it outright, but the site allows no exclusions outside system
paths.

`hit serve` therefore pays that cost once per shell instead of once per recall.

**It is not a service, and the distinction is the point.** No registration, no elevation,
no autostart, no persistence. It is a child of the shell that started it, on an endpoint
only that account can open, and it exits when the shell goes away or after sitting idle
(30 minutes by default). MSBuild's node reuse, VBCSCompiler and gopls are all the same
shape. Anyone auditing the machine should be able to see that from the source, which is
why the pipe's DACL is built explicitly from the current user's SID rather than left to a
default.

**It draws on the console it inherited.** The shell starts it with no redirection, exactly
as it starts a one-shot finder, so the terminal handling is the same code that has always
run — only the control channel is new. That channel is a named pipe on Windows (invisible
to anything enumerating sockets, and PowerShell speaks it natively through
`NamedPipeClientStream`) and a unix socket elsewhere. No listening port is involved.

Requests are served one at a time. There is one console, so two finders could not both
draw on it in any case.

**Every failure falls back to spawning.** No server, a server that will not answer, a
malformed reply, an error in the response: each one drops through to the path that has
always worked, and the shell starts a server for next time. A ping is bounded so a wedged
server can never hold the prompt; the draw itself is not, because that takes as long as
you look at it — which is equally true of the one-shot finder.

**It must not outlive its shell, and a process id is not enough to know that.** The server
watches the shell that started it and exits when it goes. That watch was a process id
checked on a timer, which asks the wrong question: Windows recycles ids briskly, so the
shell can exit, its id be handed to something else, and the check keep answering yes about
a stranger. Found from daily use on 2026-09-24 — a server whose shell had been killed was
still resident, watching a `dotnet` process that had inherited its shell's id. The parent
is now pinned with an **open handle taken at startup**, which both asks about that exact
process and stops the id being reused while the handle is held (C-033). Unix has no
equivalent handle; the id is kept with the caveat recorded, and a pidfd closes it properly
when the server ships beyond Windows.

**A new install stops the running servers.** A resident finder keeps running the binary it
was started from, so after an install the old code would go on answering Ctrl+R in every
shell that already had one — testing a change against the version it replaced.
`install.ps1` stops them unless `-KeepServers` says otherwise; safe by construction,
because every failure on this path falls back to spawning and a later recall starts a
fresh server. That last clause was not true at first: the shell tried to start a server
once per session, so a shell that had already started one fell back to spawning on every
Ctrl+R after an install until `Enable-HitServer` was run by hand (S-033, found at work
2026-09-25). A recall that finds no server now starts one if the last attempt was more
than 30 s ago. The wait is there because a new binary's first launch can take seconds on
the work machine: a second start inside that window would only lose the race for the
pipe, and a server that cannot start at all must not add a failed launch to every recall.

**What is resident has to be answerable without guessing** (C-034, S-032). Both of the
problems above were found by noticing a process and not being able to say what it was, and
on a managed machine that question may come from someone else. The ping reply therefore
carries the server's own account of itself — pid, parent and whether the parent is still
alive (from the pinned handle, not a fresh lookup), endpoint, exe, start time, idle limit,
and how often and how recently it drew a finder. A ping resets the idle clock like any
caller, so "last used" counts finder requests only: otherwise every status check would
report an abandoned server as just used. `hit status` walks the pipe namespace and asks
**every** `hit-*` endpoint rather than only this shell's, because the interesting cases —
an orphan, a server from before the last install — are exactly the ones a per-session
question misses. It marks the server whose parent is the shell it ran from, and says in
words when a server is on another version or its shell has gone. Each ask is bounded at a
second: a server drawing a finder in another shell cannot answer until that closes, and
status reports it as busy rather than waiting. `Get-HitStatus` is the same question from
inside the shell, for this session only and with no process started, and adds what only
the shell knows: whether server mode is on at all.

It is **opt-in** (`$env:HIT_SERVER`, or `Enable-HitServer` at any prompt) and
`Disable-HitServer` abandons it with no restart. Something on the Ctrl+R path has to be
abandonable the moment it misbehaves.

With `HIT_TIMING` on, the finder's report says which route it came by — `via pipe` or
`via spawn` — because a served run simply has no `spawn` phase, and an absence reads too
easily as a fast one.

The history file is re-read on every request rather than cached: commands have been
appended since the last one, and a finder that could not see what you just ran would be
worse than a slow one.

## 17. Categories (planned, C-036)

A category is a broad label over history, defined in `config.toml` and applied when the
history is read. Nothing is stored, so the stored command is never touched, editing a rule
reclassifies everything already recorded, and deleting the config loses nothing.

**Few, or they are pointless.** A category earns its place by answering "I know roughly
what kind of thing it was, but not the command". Anything better found by typing part of it
into Ctrl+R — `jq` pipelines, one-off chains — gets no category. The owner's working set,
2026-09-26, and already suspected of being one too many:

| Category | What | Why it exists |
|---|---|---|
| `git` | simple commands starting `git` or `gh` | daily, and easy to scope to |
| `navigation` | typed `cd`, `Set-Location`, `Push-Location`, … and the `cd` records the prompt writes | the bucket the directory finder / `z` replacement looks at (T-011) |
| `content` | fetching from the web, expanding or making archives | mutative, and the syntax is always forgotten |
| `environment` | setting `$env:` variables, `Get-Credential` into a variable, and the like | the variable a script needs is exactly what gets forgotten |
| `devops` | bespoke scripts, remoting, elastic operations (curl or script alike) | placeholder: grows heuristically, one rule at a time |

There is no `danger` category: danger depends on the shell's state as well as the command,
so it is a guard (§9.1), and red is reserved for it.

**Simple commands, not chains.** A rule matches on the command's **first word**, and only
when the command is a single statement. `git log -5` is `git`, and so is
`git log | Select-String fix` — a pipeline that *starts* with git is still git. A `;` or
`&&` chain with git somewhere in it, or a script block (`& { … }`), is not: those are too
bespoke to fold in (owner, 2026-09-26). A list of first words is the plain form; a regex is
there for what a word list cannot say (`$env:X = …`).

Telling a pipeline from a chain needs the shell's parser, and the Go side has none. But
the shell already parses every command it records (PSReadLine hands over the AST), so the
capture hook can store the command's **shape** — `simple`, `pipeline`, `chain` or `block` —
beside it (S-034). That is a fact about the command, like its exit code, not a category, so
storing it breaks nothing; categories stay a read-time view over it. Records made before
the field existed fall back to a cheap heuristic in Go (an unquoted top-level `;` or `&&`
means chain).

**A fuzzy jump is not navigation.** `z platform` is a query, not a place; the `cd` record
it produces is what lands in `navigation`. The jump itself stays unlabelled, and resolves
with the exact-name rule (C-035): `gdt-platform` always takes `gdt-platform`, and only
otherwise does frecency choose between it and `gdt-platform-launch`.

**First matching rule wins the marker**, so order is priority. `devops` comes before
`content` so that a `curl` to an elastic host is devops, not a download.

A draft — the shape to build to, not a promise of field names:

```toml
[[category]]
name     = "devops"
match    = '(?i)elastic|^(Invoke-Command|Enter-PSSession)\b'
cwd      = '~\scripts\**'     # anything run from the scripts tree, whatever it is called

[[category]]
name     = "git"
commands = ["git", "gh"]

[[category]]
name     = "navigation"
kind     = "cd"                # the recorded directory changes, or
commands = ["cd", "sl", "Set-Location", "Push-Location", "pushd", "Pop-Location", "popd"]

[[category]]
name     = "content"
commands = ["Invoke-WebRequest", "iwr", "curl", "wget",
            "Expand-Archive", "Compress-Archive", "tar", "7z", "unzip"]

[[category]]
name     = "environment"
match    = '(?i)^\$env:\w+\s*=|=\s*Get-Credential\b|SetEnvironmentVariable'
```

Within one rule, `commands`, `match`, `cwd` and `kind` are alternatives: any one matching
is enough. Rules that need two conditions at once ("directories, but only on a share") are
left open until one is actually wanted. `navigation` is the exception: it
is predefined because the directory finder depends on it, and it can still be overridden
here.

**`environment` brings secrets closer to the surface.** `$x = Get-Credential` records
nothing secret, but `$env:TOKEN = 'abc…'` has already stored the token in plain text, and a
category that gathers such lines makes them easier to find — for anyone reading the file.
Secret redaction (C-014) becomes more pressing, not less, once this exists.
