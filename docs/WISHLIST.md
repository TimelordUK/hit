# hit — wishlist

Every idea goes here, however wild. We refine them by using the tool.

**IDs:** `C-` core (Go binary, storage, search engine) · `S-` shell integration (pwsh, zsh) ·
`F-` user-facing features and UX · `T-` the finder's own layout, keys and chrome.
IDs are permanent. Don't renumber; mark items dropped instead.

`T-` was split out of `F-` once the finder grew enough to have its own questions (what a row
shows, where the preview sits, how you move around it). Finder items already filed under `F-`
stay there — IDs never move — so look in both when hunting for prior art.

**Milestones:** `M1` daily-drivable in pwsh · `M2` navigation + guards · `M3` zsh · `later` · `?` undecided

**Status:** `idea` → `planned` → `doing` → `done` · `dropped`

---

## Core

| ID | Item | MS | Status |
|---|---|---|---|
| C-001 | JSONL store: tolerant reader, record kinds `cmd`/`end`/`cd`/`del`, merge by id | M1 | done |
| C-002 | ULID ids; data/config dir resolution with env overrides | M1 | done |
| C-003 | Tombstone deletion (Del in the finder done; `hit rm <id>` still to do), hidden immediately | M1 | doing |
| C-004 | `hit compact`: lock, rewrite, catch late appends, atomic swap; many-shell stress test | M1 | planned |
| C-005 | Fuzzy match + ranking (match × recency × frequency × same-dir × success) | M1 | done |
| C-006 | Duplicate collapse with run count / last run | M1 | done |
| C-007 | `hit import psreadline` (backtick-continued multi-line entries) | M1 | planned |
| C-008 | `hit import mcfly` (SQLite, import-only dependency). mcfly redirects PSReadLine's history file to a temp file, so its DB holds the recent history | M1 | planned |
| C-009 | `hit import zsh` (EXTENDED_HISTORY) and `hit import atuin` | M3 | idea |
| C-010 | `hit init <shell>` emits the integration script matching the binary version (pwsh done) | M1 | doing |
| C-011 | Per-host/per-env history files (Windows, WSL, msys2, other machines); each appends only to its own, reader merges all | M3 | planned |
| C-012 | Resolve mapped drives ↔ UNC so `Z:\logs` and `\\srv\share\logs` find each other | M2 | idea |
| C-013 | Guard rules in `config.toml`, compiled into shell-native checks; reload on change | M2 | idea |
| C-014 | Secret redaction patterns (don't record / mask) applied before writing | M1 | idea |
| C-015 | Binary index cache beside the JSONL if load time ever becomes noticeable | later | idea |
| C-016 | `hit stats`: most-used commands, dirs, failure rates | later | idea |
| C-017 | Export / query-friendly view for `sql-cli` (flattened cmd+end rows as CSV/JSONL) | later | idea |
| C-018 | Test hooks: `HIT_DATA_DIR`/`HIT_CONFIG` isolation, injected clock + id source, `HIT_NOW` | M1 | done |
| C-019 | `hit search --filter … --print` (non-interactive, same ranking) | M1 | done |
| C-020 | Fuzz tests: reader tolerance + byte-exact round-trip of any command string | M1 | done |
| C-021 | Golden tests for search/ranking under `testdata/` with `-update` | M1 | done |
| C-022 | JSON Schema for records + result handoff in `schema/`; shared fixtures for all writers | M1 | doing |
| C-023 | CI: GitHub Actions windows + ubuntu: go vet/test/short fuzz, Pester, zsh tests, e2e smoke | M1 | doing |
| C-024 | Multi-process append + compact stress test | M1 | planned |
| C-025 | `hit import zoxide` (`zoxide query --list --score`) so directory ranking carries over | M2 | idea |
| C-026 | Learn from finder picks: record what was chosen (new record kind, schema first) and weight it in ranking, so the commands you actually reach for float up | ? | idea |
| C-027 | `hit path data\|history\|config`: print resolved paths for scripts and `hit doctor` | M1 | done |
| C-028 | Release workflow: tag `v*` cross-compiles windows/linux/darwin (amd64+arm64), publishes archives + SHA256SUMS, so hit installs where `proxy.golang.org` is blocked | M1 | done |
| C-029 | `HIT_TIMING` phase timings (DESIGN §15): elapsed ms for spawn, init, read, build, rank and first paint, shown in the finder and logged, so a slow Ctrl+R is attributed to a phase instead of guessed at. `--started-at` lets the binary measure its own process creation. Measured 2026-09-21: a day's history (70 commands / 380 lines) costs ~1 ms of an ~8 ms total, and 24.6k lines costs 37 ms — so recall latency is not history size | M1 | done |
| C-030 | Literal match mode beside the subsequence one. **Done:** a leading `'` matches literally, fzf-style; `^`/`$`/`!` and per-term splitting are not built. Matching is a pure subsequence, so `hit` matches `Get-History`, `Get-ChildItem` (h/i in "Child", t in "Item"), `Push-Location` and `$h = Invoke-RestMethod -Uri https://…` — 6 of 8 commands in a sample. Ranking floats the true hits to the top, which is why it has been bearable, but the tail fills the list once there is real history. Prior art worth copying rather than inventing: fzf's per-term syntax (`'exact`, `^prefix`, `suffix$`, `!negate`), which beats a global toggle because one query often wants both kinds of term. Smart-case already works this way and should stay | M1 | done |

## Shell

| ID | Item | MS | Status |
|---|---|---|---|
| S-001 | pwsh: record hook via `AddToHistoryHandler`, in-process append, no process spawn | M1 | done |
| S-002 | pwsh: prompt wrapper writes `end` (exit, duration) and `cd` records | M1 | done |
| S-003 | pwsh: Ctrl+R handler → `hit search` → replace buffer with the (multi-line) result. Must work in vi insert mode (owner uses `-EditMode vi`) | M1 | done |
| S-004 | pwsh: `Format-HitCommand` tidy via the PowerShell parser, with a token-equality check | M1 | done |
| S-005 | pwsh: store `$PWD.ProviderPath` (clean UNC), handle non-FS providers | M1 | done |
| S-006 | pwsh: leading-space = don't record | M1 | done |
| S-007 | pwsh: Enter guard (re-bind AcceptLine) for prod rules | M2 | idea |
| S-008 | pwsh: Alt+C directory finder → `Set-Location`; `j <terms>` jump | M2 | planned |
| S-009 | pwsh: Up/Down stepping backed by hit (prefix-filtered, multi-line aware) instead of PSReadLine's | ? | idea |
| S-010 | zsh: `zshaddhistory` / `precmd` / `chpwd` hooks, backgrounded writes | M3 | planned |
| S-011 | zsh: Ctrl+R / Alt+C widgets, `${(z)}` tidy formatter | M3 | planned |
| S-012 | zsh: antidote-installable plugin layout | M3 | planned |
| S-013 | zsh: `accept-line` guard wrapper | M3 | idea |
| S-014 | pwsh on Linux: verify everything works as on Windows | M3 | idea |
| S-015 | pwsh: Pester 5 suite; line-editor adapter (Get/Replace/Accept) so handlers run against a fake | M1 | doing |
| S-016 | pwsh: formatter corpus `tests/corpus/pwsh/` (expected output + token-equality) | M1 | done |
| S-017 | e2e smoke via pty/ConPTY: real shell + module, type multi-line, assert record; Ctrl+R recall | M1 | planned |
| S-018 | zsh: test harness (`zsh -f`, fake BUFFER) | M3 | planned |
| S-019 | `Invoke-HitSelfTest`: run formatter/round-trip checks over the owner's real history, locally only | M1 | idea |
| S-020 | C# `ICommandPredictor` plugin reading hit's history for PSReadLine predictions | later | idea |
| S-021 | `h` / `hit` command opens the finder as a no-key fallback | M1 | planned |
| S-023 | pwsh: `Join-HitCommand` + Alt+M toggle: split a one-liner at top-level parameters/pipes, or bring a continued command back onto one line for editing | M1 | done |
| S-026 | If `spawn` turns out to own the recall time on a managed machine: keep one warm `hit` process per shell session rather than spawning per Ctrl+R. Weigh against principle 2 (no daemon) — a per-session child is not a service, but it is close enough to need a decision | M2 | idea |
| S-022 | `scripts/install.ps1`: build + install with version stamped; `-Clear` moves the current history into `backup\` (dogfooding: start clean each reinstall) | M1 | done |
| S-024 | `HIT_TIMING` phase timings in `Invoke-HitFinder` (DESIGN §15): temp-file create, buffer read, the run, read-back and redraw, plus `--started-at` so the binary can report process creation. Ctrl+R felt like 2.5–3 s on an AV-heavy work machine while the Go side is ~8 ms — this says which phase owns it | M1 | done |
| S-027 | Alt+M: let `Join-HitCommand` collapse statements by inserting `;`, so `& {` blocks and other multi-statement commands can come back onto one line. Today it refuses them ("it is more than one statement") because DESIGN §7 classes adding a separator as a rewrite. Needs the token-equality check extended to accept a `Semi` standing exactly where a `NewLine` was, and a corpus case per context where the two are *not* equivalent — this is the "spend real time on the parser" item | M1 | idea |
| S-028 | Alt+M round-trip corpus: for every command in `tests/corpus/pwsh/`, assert split-then-join returns the original text, so the two directions cannot drift apart | M1 | idea |
| S-025 | Stop using `[System.IO.Path]::GetTempFileName()` for the finder handoff: it creates a file in `%TEMP%`, which corporate AV scans on every Ctrl+R, and the Win32 call linear-probes for a free name as `%TEMP%` fills over a day. Use one fixed per-session path (or a pipe / stdout handoff) instead. Blocked on S-024 confirming it matters: read the `temp` phase | M1 | idea |

## Features / UX

| ID | Item | MS | Status |
|---|---|---|---|
| F-001 | Finder TUI: fuzzy list, multi-line preview pane with `⏎ +N` markers | M1 | done |
| F-002 | Del in finder = delete (with undo while open) | M1 | done |
| F-003 | Ctrl+E: edit selection in `$EDITOR`, return to prompt | M1 | planned |
| F-004 | Ctrl+R (again, inside the finder) scope cycle: dir → session → host → all | M1 | done |
| F-005 | Hide failed toggle (done); exit code / duration / cwd in preview (done); richer filters to come | M1 | doing |
| F-006 | Ctrl+F toggle "as typed" / "tidied"; decide format-on-demand vs pre-computed `fmt` | M1 | idea |
| F-007 | Syntax highlighting in the preview (pwsh and zsh) | later | idea |
| F-008 | Directory finder: frecency, never stats remote paths, explicit existence check on jump | M2 | planned |
| F-009 | Pivot: dir → commands run there; command → jump to its dir | M2 | idea |
| F-010 | Prod guards: warn / confirm-word / block; red marker on matching entries in the finder | M2 | idea |
| F-011 | Convert to splatting as a tidy mode | later | idea |
| F-012 | Bulk delete: select many / delete all matching the current filter (junk-paste cleanup) | M1 | idea |
| F-013 | Pin / favourite commands and directories; pinned float to top | later | idea |
| F-014 | Tags or notes on a command ("reindex logs-2026") that are searchable | later | idea |
| F-015 | Find files under previously visited dirs (search inside where you've been) | later | idea |
| F-016 | Time filter ("last week", "Monday") in the finder | later | idea |
| F-017 | Guard conditions on time/day (extra-strict before 10:00 on a Monday) | later | idea |
| F-018 | Keymap from `config.toml`, no hard-coded keys | M1 | planned |
| F-019 | `hit doctor`: detect Zellij/tmux/WT/WSL/msys2, report key clashes and setup problems | M2 | idea |
| F-026 | Finder rows show the command flattened onto one line, not just its first line: a block starting `& {` rendered as `& {` and nothing else, so every command of that shape looked identical. Match highlighting maps back to the original offsets, so matches on later lines now show too | M1 | done |
| F-027 | Finder ignores Alt chords instead of typing them into the filter: Alt+M in the finder was silently narrowing the list to whatever matched "m", so the key looked dead and quietly did the wrong thing | M1 | done |
| F-020 | Inline vs full-screen mode (still full-screen); layout adapts to pane size (done) | M1 | doing |
| F-021 | Shell-family filter by default; WSL/msys2/Windows path translation in dir finder | M3 | idea |
| F-022 | Leader key inside the finder, only if clashes pile up | ? | idea |
| F-023 | Drop-in `cd` like `zoxide --cmd cd`: a real path → Set-Location, otherwise jump to the best frecency match; `cdi` interactive. Replaces zoxide (and ZLocation) | M2 | idea |
| F-024 | Retrospective lock: mark a command in the finder as guarded, so sending it (or a match) again asks for confirmation first. Per-command guards built on F-010/C-013 | M2 | idea |
| F-025 | navi-style expansion: a short alias expands in place at the prompt into a stored (multi-line) command, with placeholders to fill in. Aliases in config or as tagged history entries (F-014) | later | idea |

## TUI (the finder)

| ID | Item | MS | Status |
|---|---|---|---|
| T-001 | Show each command's time in the list itself, not only in the preview, so a run can be placed against the working day without moving the cursor onto it. **Done:** fixed six-wide column on the left, dimmed; clock for today, `02 Jan` otherwise, blank when the record has no timestamp; dropped below a 40-column pane rather than squeezing the command | M1 | done |
| T-002 | Time filter in the finder: today / this week / since a date. Pairs with T-001 — once the time is visible, wanting to narrow by it follows. Decide whether it is a cycling toggle like `^x`, a scope-style cycle, or typed into the query (`>today`). Now that T-001 shows the time, this is the natural next step. Prior art in `mless`: `Pane.parseTimeInput` takes `15:04`, `15:04:05` and full dates, and anchors a bare time to the document's day with a midnight-rollover guard. hit would anchor to *now* instead and wants relative forms (`2h`, `today`) that mless has no use for — copy the shape, not the file | M1 | idea |
| T-003 | Preview placement: expand the selected row **inline** rather than in a fixed pane at the bottom. With a full list the eye has to travel to the foot of the screen and back to read a command it is already sitting on. Undecided — inline costs a stable layout (rows move as you scroll) and complicates the `⏎ +N` marker. Prototype both and pick by using them (F-020 covers the inline/full-screen frame; this is about where the *command body* goes) | M1 | idea |
| T-004 | Vim-style movement in the finder: `gg` / `G` for top and bottom, and whatever else earns its place (`d`/`u` half-page?). The owner runs PSReadLine in vi edit mode, so the muscle memory is already there. Blocked on T-006: without a mode, every one of these keys is filter text. Keymap should match `mless` key for key where the two overlap — the muscle memory is the point | M1 | idea |
| T-005 | The finder's half of C-030. **Done:** the header names `literal` next to the scope, so the mode is not left to be inferred from a quote that is easy to miss. Still open: a whole-query toggle for when quoting every term gets tiresome, once per-term syntax exists | M1 | doing |
| T-006 | A mode split in the finder, so navigation keys can exist at all. Today every printable key is filter text, which is why T-004 has nowhere to put `gg`. `mless` gets single-letter motions for free because nothing is accepting text in its normal mode — hit's query box is always live, so it has to choose. Options: Esc switches filter → normal (Esc again, or `q`, then cancels) which fits vi edit mode at the prompt and is the only one that scales to counts and marks; or the Alt namespace, free since F-027, which needs no mode but is not vim and stops at a handful of chords. Take `mless`'s shape — a `Mode` field dispatched at the top of `Update`, plus a `countPrefix` accumulated from digits and consumed by the next command — but copy the pattern, not the code, and do not copy its trick of overloading `countPrefix = -1` as a "waiting for a mark" sentinel | M1 | idea |
| T-007 | Do **not** lift `mless`'s `Viewport`. It is the obvious-looking candidate and it is the wrong shape: it scrolls a window over a document and its highlighted line is optional (`-1` by default), whereas the finder moves a selection that always exists and lets the window follow. `clampCursor` is already the right twenty lines for that job. Recorded so the idea does not get re-proposed every few months | — | dropped |
