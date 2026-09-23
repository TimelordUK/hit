# hit

**hi**story **t**ool: a fast, multi-line-aware shell history and directory finder for
PowerShell and zsh.

> You never lose something you typed, and you never lose somewhere you went.

**Status:** M1, usable daily on PowerShell 7 (recording, Ctrl+R finder, Alt+M reflow).
Directory jumping and prod guards are next; zsh comes after that.

## Why another history tool?

- **Multi-line commands are first class.** A 4-line `Invoke-RestMethod` to Elasticsearch
  comes back exactly as you typed it, byte for byte.
- **No database.** History is a plain append-only JSONL file you can grep, sync, and query.
  There's no "can't open connection to db".
- **Network shares are first class.** Directory jumping never stats remote paths, so
  `\\server\share` entries stay fast and don't get pruned.
- **Nothing on the hot path.** Recording a command costs ~0.8 ms in-process: no process is
  started, nothing touches the network.
- **PowerShell first, zsh second.** Windows and Linux.

## Install (PowerShell 7)

Needs [Go](https://go.dev/dl/) 1.25+ and PowerShell 7 with PSReadLine 2.2+ (`$PSVersionTable`,
`Get-Module PSReadLine -ListAvailable`).

```powershell
go install github.com/TimelordUK/hit/cmd/hit@latest
```

That puts `hit.exe` in `~\go\bin` (`go env GOPATH`\bin). Make sure that's on your `PATH`:

```powershell
if (-not (Get-Command hit -ErrorAction SilentlyContinue)) {
    $env:PATH += ';' + (Join-Path (go env GOPATH) 'bin')   # and add it permanently
}
```

Then add **one line at the end of your profile** (`notepad $PROFILE`):

```powershell
Invoke-Expression (& hit init pwsh | Out-String)
```

Put it **last**, after anything else that hooks the prompt or PSReadLine (starship,
oh-my-posh, zoxide, PSFzf, mcfly). hit chains whatever is already there rather than
replacing it, and re-hooks itself if something later takes over, but last is simplest.

Open a new terminal. hit records from then on; `hit path history` shows where.

### Without Go (locked-down network)

If `go install` can't reach `proxy.golang.org`, download a prebuilt binary from
[Releases](https://github.com/TimelordUK/hit/releases) instead — the integration script is
embedded in it, so that one file is everything:

```powershell
# pick the newest release, Windows x64
$tmp = New-TemporaryFile
Invoke-WebRequest -Uri (
    (Invoke-RestMethod https://api.github.com/repos/TimelordUK/hit/releases/latest).assets |
        Where-Object name -like '*windows_amd64.zip' | Select-Object -ExpandProperty browser_download_url
) -OutFile "$tmp.zip"
$dest = "$env:LOCALAPPDATA\Programs\hit"
Expand-Archive "$tmp.zip" -DestinationPath $dest -Force
Unblock-File "$dest\hit.exe"
# add $dest to your PATH (once), then the profile line above
```

Or just download the `.zip` in a browser, unpack it somewhere on your `PATH`, and add the
same profile line. Checksums are in `SHA256SUMS.txt` on the release.

A clone alone isn't enough behind such a proxy: building still needs the modules. If your
network allows direct git access to the dependencies, `$env:GOPROXY='direct'` and
`$env:GOFLAGS='-mod=mod'` sometimes gets through; otherwise use the release binary.

### From a clone instead

```powershell
git clone https://github.com/TimelordUK/hit.git; cd hit
./scripts/install.ps1            # go install with the git version stamped in
./scripts/install.ps1 -Clear     # same, but start from an empty history
```

`-Clear` moves the current history file into `<data dir>\backup\` rather than deleting it.

## Using it

| Key | What it does |
|---|---|
| **Ctrl+R** | open the finder, seeded with whatever you've typed |
| **Alt+M** | split a long command at its parameters and pipes, or join it back onto one line |

Both work in vi and Windows edit modes.

**In the finder:** type to filter (fuzzy, in order; a capital letter makes that letter
case-sensitive). `↑`/`↓`, `PgUp`/`PgDn`, `Home`/`End` move. `Enter` puts the command in your
prompt without running it, `Tab` the same for further editing, `Esc` cancels.
`Ctrl+R` again cycles the scope: this directory → this session → this machine → everything.
`Ctrl+X` hides commands that failed. `Del` deletes an entry, `Ctrl+Z` undoes that while the
finder is open. Multi-line commands show a `⏎ +N` marker, with the whole command, its
directory, exit code and duration in the preview.

**Alt+M** uses PowerShell's own parser and re-parses its own output: if the token stream
isn't identical, you get your command back untouched. It won't merge separate statements or
touch newlines that belong to a string or here-string, and it tells you when it leaves
something alone. Your stored history is never rewritten; this only changes the prompt buffer.

### Commands

```powershell
hit search --scope all           # the finder, standalone
hit search --print --limit 20    # same ranking, JSON lines, no TUI (for scripts and fzf)
hit path history|data|config     # where things live
hit init pwsh                    # print the integration script
hit version
```

### Turning it off

`Disable-Hit` stops recording in the current session and gives Ctrl+R back to PSReadLine.
Remove the profile line to stop it permanently. Your history file stays where it is.

### If something misbehaves

```powershell
$env:HIT_DEBUG = 1               # then reproduce, and read:
Get-Content $env:TEMP\hit-debug.log
```

The finder runs inside a key handler where errors are swallowed on purpose (a broken hit must
never break your prompt), so that log is how it reports for duty.

### If Ctrl+R feels slow

```powershell
$env:HIT_TIMING = 1              # then hit Ctrl+R; the report is in the finder
Get-Content $env:TEMP\hit-debug.log
```

You get a phase breakdown from both sides, e.g.

```
timing (go): spawn 2841 · init 0 · read 2 · build 0 · rank 1 · paint 4 · total 2848 ms
timing (pwsh): temp 11 · buffer 0 · run 3204 · readback 6 · redraw 2 · total 3223 ms
```

The first field says how the finder was reached: `via spawn` for a fresh process, or
`via pipe` when a resident `hit serve` drew it (see below).

`spawn` is process creation — the loader, the Go runtime, and on a managed machine whatever
scans the binary before it may run. It is measured across both processes, so it is the one
number neither side could report alone. `run` spans the whole finder session, so it includes
your own time looking at it.

History size is almost never the answer: 24k lines of history costs ~37 ms to load and rank.
See [DESIGN §15](docs/DESIGN.md) for what each phase covers.

### If it is still slow: the resident finder

On a managed machine the cost is often not hit at all, but launching *any* unfamiliar
binary. Measured on one: a file the endpoint agent has not seen costs ~3.5 s on first
launch and ~58 ms once known, and that verdict is evicted through the day. If your IT can
exclude the install folder, that fixes it outright. If they can't:

```powershell
$env:HIT_SERVER = 1              # then open a new terminal
```

hit then keeps one process per shell and asks it to draw, so the launch cost is paid once
a session instead of on every Ctrl+R. `Enable-HitServer` / `Disable-HitServer` turn it on
and off at a prompt with no restart, and `$env:HIT_SERVER_IDLE` sets how long it sticks
around (default `30m`).

**It is not a service.** No registration, no elevation, no autostart, nothing to uninstall:
it is a child of your shell, on a pipe only your account can open, and it exits when the
shell closes or after sitting idle. MSBuild, the C# compiler and gopls all do the same.
If the server isn't there or won't answer, hit just starts the finder the old way — so the
worst case is the speed you had before.

## Settings

| | Default | Override |
|---|---|---|
| Data | `%LOCALAPPDATA%\hit\` · `~/.local/share/hit/` | `HIT_DATA_DIR` |
| Config | `%APPDATA%\hit\config.toml` · `~/.config/hit/config.toml` | `HIT_CONFIG` |

Commands typed with a leading space aren't recorded. Commands PSReadLine considers sensitive
(password, token, apikey, secret) aren't recorded either.

## Docs

- [Design](docs/DESIGN.md)
- [Wishlist](docs/WISHLIST.md) (`C-` core · `S-` shell · `F-` features)

## Development

Needs Go and PowerShell 7 with Pester 5+. `./scripts/test.ps1` runs exactly what CI runs:
`go vet`, `go test`, a short fuzz pass and the Pester suites. CI runs it on Windows and Ubuntu.

Bugs from daily use become a failing test first, then a fix (DESIGN §13.3).

## License

MIT
