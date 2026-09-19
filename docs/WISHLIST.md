# hit — wishlist

Every idea goes here, however wild. We refine them by using the tool.

**IDs:** `C-` core (Go binary, storage, search engine) · `S-` shell integration (pwsh, zsh) ·
`F-` user-facing features and UX. IDs are permanent. Don't renumber; mark items dropped instead.

**Milestones:** `M1` daily-drivable in pwsh · `M2` navigation + guards · `M3` zsh · `later` · `?` undecided

**Status:** `idea` → `planned` → `doing` → `done` · `dropped`

---

## Core

| ID | Item | MS | Status |
|---|---|---|---|
| C-001 | JSONL store: tolerant reader, record kinds `cmd`/`end`/`cd`/`del`, merge by id | M1 | done |
| C-002 | ULID ids; data/config dir resolution with env overrides | M1 | done |
| C-003 | Tombstone deletion (`hit rm <id>`), hidden immediately | M1 | planned |
| C-004 | `hit compact`: lock, rewrite, catch late appends, atomic swap; many-shell stress test | M1 | planned |
| C-005 | Fuzzy match + ranking (match × recency × frequency × same-dir × success) | M1 | planned |
| C-006 | Duplicate collapse with run count / last run | M1 | planned |
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
| C-019 | `hit search --filter … --print` (non-interactive, same ranking) | M1 | planned |
| C-020 | Fuzz tests: reader tolerance + byte-exact round-trip of any command string | M1 | done |
| C-021 | Golden tests for search/ranking under `testdata/` with `-update` | M1 | planned |
| C-022 | JSON Schema for records + result handoff in `schema/`; shared fixtures for all writers | M1 | doing |
| C-023 | CI: GitHub Actions windows + ubuntu: go vet/test/short fuzz, Pester, zsh tests, e2e smoke | M1 | doing |
| C-024 | Multi-process append + compact stress test | M1 | planned |
| C-025 | `hit import zoxide` (`zoxide query --list --score`) so directory ranking carries over | M2 | idea |

## Shell

| ID | Item | MS | Status |
|---|---|---|---|
| S-001 | pwsh: record hook via `AddToHistoryHandler`, in-process append, no process spawn | M1 | done |
| S-002 | pwsh: prompt wrapper writes `end` (exit, duration) and `cd` records | M1 | done |
| S-003 | pwsh: Ctrl+R handler → `hit search` → replace buffer with the (multi-line) result. Must work in vi insert mode (owner uses `-EditMode vi`) | M1 | planned |
| S-004 | pwsh: `Format-HitCommand` tidy via the PowerShell parser, with a token-equality check | M1 | planned |
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
| S-016 | pwsh: formatter corpus `tests/corpus/pwsh/` (expected output + token-equality) | M1 | planned |
| S-017 | e2e smoke via pty/ConPTY: real shell + module, type multi-line, assert record; Ctrl+R recall | M1 | planned |
| S-018 | zsh: test harness (`zsh -f`, fake BUFFER) | M3 | planned |
| S-019 | `Invoke-HitSelfTest`: run formatter/round-trip checks over the owner's real history, locally only | M1 | idea |
| S-020 | C# `ICommandPredictor` plugin reading hit's history for PSReadLine predictions | later | idea |
| S-021 | `h` / `hit` command opens the finder as a no-key fallback | M1 | planned |

## Features / UX

| ID | Item | MS | Status |
|---|---|---|---|
| F-001 | Finder TUI: fuzzy list, multi-line preview pane with `⏎ +N` markers | M1 | planned |
| F-002 | Del in finder = delete (with undo while open) | M1 | planned |
| F-003 | Ctrl+E: edit selection in `$EDITOR`, return to prompt | M1 | planned |
| F-004 | Ctrl+R (again, inside the finder) scope cycle: dir → session → host → all | M1 | planned |
| F-005 | Hide failed commands toggle; show exit code / duration / cwd in preview | M1 | idea |
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
| F-020 | Inline vs full-screen mode; layout adapts to pane size (side/below/hidden preview) | M1 | idea |
| F-021 | Shell-family filter by default; WSL/msys2/Windows path translation in dir finder | M3 | idea |
| F-022 | Leader key inside the finder, only if clashes pile up | ? | idea |
| F-023 | Drop-in `cd` like `zoxide --cmd cd`: a real path → Set-Location, otherwise jump to the best frecency match; `cdi` interactive. Replaces zoxide (and ZLocation) | M2 | idea |
