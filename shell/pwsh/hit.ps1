# hit — PowerShell integration (DESIGN §5).
#
# Loaded by `Invoke-Expression (& hit init pwsh | Out-String)`. This file only defines
# functions; key handlers and hooks are registered by Enable-Hit, so tests can dot-source
# it without touching PSReadLine.
#
# Rules for this file:
#   - Nothing called on Enter or at the prompt may spawn a process or touch the network.
#   - Writing history must never throw into the user's session (principle 7).

$script:HitUlidAlphabet = '0123456789ABCDEFGHJKMNPQRSTVWXYZ'
$script:HitUtf8 = [System.Text.UTF8Encoding]::new($false)

# Share mode for appends. On Windows, FileShare.Read denies other writers but lets readers
# in. On Unix, .NET emulates sharing with advisory flock: anything but FileShare.None takes
# a *shared* lock, and FileMode.Append seeks to the end at open (no O_APPEND), so
# concurrent shells would overwrite each other's lines. FileShare.None takes LOCK_EX.
$script:HitAppendShare = if ($IsWindows) { [System.IO.FileShare]::Read } else { [System.IO.FileShare]::None }

# The current time. HIT_NOW pins it for tests.
function Get-HitNow {
    if ($env:HIT_NOW) {
        return [DateTimeOffset]::Parse($env:HIT_NOW, [Globalization.CultureInfo]::InvariantCulture)
    }
    [DateTimeOffset]::UtcNow
}

function ConvertTo-HitTimestamp([DateTimeOffset]$Time) {
    $Time.UtcDateTime.ToString("yyyy-MM-dd'T'HH:mm:ss.fff'Z'", [Globalization.CultureInfo]::InvariantCulture)
}

# A ULID: 48-bit millisecond time + 80 random bits, Crockford base32, 26 chars.
function New-HitId([DateTimeOffset]$Time = (Get-HitNow)) {
    $rand = [byte[]]::new(10)
    [System.Security.Cryptography.RandomNumberGenerator]::Fill($rand)
    # The 128-bit value as two halves: hi = time(48) | rand[0..1], lo = rand[2..9].
    [uint64]$hi = ([uint64]$Time.ToUnixTimeMilliseconds() -shl 16) -bor ([uint64]$rand[0] -shl 8) -bor $rand[1]
    [uint64]$lo = 0
    for ($i = 2; $i -lt 10; $i++) { $lo = ($lo -shl 8) -bor $rand[$i] }

    $chars = [char[]]::new(26)
    for ($i = 0; $i -lt 26; $i++) {
        $shift = 5 * (25 - $i)
        if ($shift -ge 64) {
            $v = ($hi -shr ($shift - 64)) -band 31
        } elseif ($shift -le 59) {
            $v = ($lo -shr $shift) -band 31
        } else {
            $v = (($lo -shr $shift) -bor ($hi -shl (64 - $shift))) -band 31
        }
        $chars[$i] = $script:HitUlidAlphabet[[int]$v]
    }
    [string]::new($chars)
}

# Builds a record as an ordered dictionary with the same key order as the Go writer.
# Empty fields are omitted. Pure: no I/O.
function New-HitRecord {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][ValidateSet('cmd', 'end', 'cd', 'del')][string]$Kind,
        [string]$Id,
        [string]$Timestamp,
        [string]$Command,
        [string]$Cwd,
        [string]$Dir,
        [string]$Shell,
        [string]$HostName,
        [string]$SessionId,
        [Nullable[int]]$ExitCode,
        [Nullable[long]]$DurationMs
    )
    $r = [ordered]@{ k = $Kind }
    if ($Id) { $r.id = $Id }
    if ($Timestamp) { $r.ts = $Timestamp }
    if ($Command) { $r.cmd = $Command }
    if ($Cwd) { $r.cwd = $Cwd }
    if ($Dir) { $r.dir = $Dir }
    if ($Shell) { $r.sh = $Shell }
    if ($HostName) { $r.host = $HostName }
    if ($SessionId) { $r.sid = $SessionId }
    if ($null -ne $ExitCode) { $r.exit = $ExitCode }
    if ($null -ne $DurationMs) { $r.ms = $DurationMs }
    $r
}

# One JSON line, without the newline. JSON escapes CR/LF, so a multi-line command
# can never split the line.
function ConvertTo-HitJsonLine([System.Collections.Specialized.OrderedDictionary]$Record) {
    ConvertTo-Json -InputObject $Record -Compress -Depth 2
}

# Appends one line to the history file, in-process. The file is opened denying other
# writers (see $HitAppendShare), so concurrent shells never interleave or overwrite;
# a sharing violation is retried for a few ms. Returns $false instead of throwing if the
# line could not be written.
function Add-HitLine {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Line
    )
    $bytes = $script:HitUtf8.GetBytes($Line + "`n")
    for ($attempt = 0; $attempt -lt 50; $attempt++) {
        try {
            $fs = [System.IO.FileStream]::new($Path, [System.IO.FileMode]::Append,
                [System.IO.FileAccess]::Write, $script:HitAppendShare)
            try { $fs.Write($bytes, 0, $bytes.Length) } finally { $fs.Dispose() }
            return $true
        } catch [System.IO.DirectoryNotFoundException] {
            try { $null = [System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($Path)) }
            catch { return $false }
        } catch [System.IO.IOException] {
            [System.Threading.Thread]::Sleep(2)
        } catch {
            return $false
        }
    }
    $false
}

# ---------------------------------------------------------------------------------------
# Capture (S-001, S-002, S-005, S-006). Everything below runs on Enter or at the prompt:
# in-process only, and wrapped so a failure never reaches the user.
# ---------------------------------------------------------------------------------------

$script:HitHistoryPath = $null   # set by Enable-Hit; $null = disabled
$script:HitHostName = [Environment]::MachineName
$script:HitSessionId = $null
$script:HitPending = $null       # the recorded command that hasn't reached a prompt yet
$script:HitLastDir = $null
$script:HitPrevHandler = $null   # AddToHistoryHandler we chain to
$script:HitHandler = $null       # our handler, as PSReadLine stores it
$script:HitPrevPrompt = $null    # prompt we wrapped last (restored by Disable-Hit)
$script:HitPromptWrapper = $null # our current prompt wrapper
$script:HitInHandler = $false    # re-entry guard: a chained handler may call back into ours

# FileSystem: the provider path, so UNC shares are stored as \server\share rather than
# Microsoft.PowerShell.Core\FileSystem::\server\share and custom PSDrives resolve.
# Other providers (HKLM:\, Cert:\): the PowerShell path, which Set-Location understands.
function Get-HitLocationPath([System.Management.Automation.PathInfo]$Location) {
    if ($Location.Provider.Name -eq 'FileSystem') { $Location.ProviderPath } else { $Location.Path }
}

# Whether a line accepted at the prompt should be recorded. $Verdict is what the chained
# AddToHistoryHandler said: PSReadLine's default returns MemoryOnly for lines that look
# sensitive (password, token, apikey, secret…), and hit doesn't write those either.
function Test-HitShouldRecord([string]$Line, $Verdict) {
    if ([string]::IsNullOrWhiteSpace($Line)) { return $false }
    if ($Line[0] -eq ' ') { return $false }  # leading space: don't record (S-006)
    if ($Verdict -is [bool]) { return $Verdict }
    if ($null -eq $Verdict) { return $true }
    "$Verdict" -eq 'MemoryAndFile'
}

# The AddToHistoryHandler body. Returns the chained handler's verdict unchanged so
# PSReadLine's own history (Up arrow, predictions) keeps working as before.
function Invoke-HitAddToHistory([string]$Line) {
    if ($script:HitInHandler) { return $true }
    $verdict = $true
    if ($script:HitPrevHandler) {
        $script:HitInHandler = $true
        try { $verdict = $script:HitPrevHandler.Invoke($Line) } catch { $verdict = $true }
        finally { $script:HitInHandler = $false }
    }
    try {
        if ($script:HitHistoryPath -and (Test-HitShouldRecord $Line $verdict)) {
            # A command recorded earlier never reached our prompt hook: something replaced
            # the prompt after us. Wrap it again.
            if ($script:HitPending) { Register-HitPrompt }
            $now = Get-HitNow
            $id = New-HitId $now
            $rec = New-HitRecord -Kind cmd -Id $id -Timestamp (ConvertTo-HitTimestamp $now) -Command $Line `
                -Cwd (Get-HitLocationPath $ExecutionContext.SessionState.Path.CurrentLocation) `
                -Shell pwsh -HostName $script:HitHostName -SessionId $script:HitSessionId
            if (Add-HitLine -Path $script:HitHistoryPath -Line (ConvertTo-HitJsonLine $rec)) {
                $script:HitPending = @{ Id = $id; Start = [System.Diagnostics.Stopwatch]::GetTimestamp() }
            }
        }
    } catch { }
    $verdict
}

# The prompt hook body: the end record for the pending command, and a cd record when the
# location changed. Idempotent, so being reached twice through a chain of wrappers is fine.
# $At is when the prompt started (Stopwatch timestamp): the command's duration ends there,
# not after the wrapped prompt has drawn itself.
function Invoke-HitPrompt([bool]$Success, $ExitCode, [long]$At = [System.Diagnostics.Stopwatch]::GetTimestamp()) {
    try {
        if (-not $script:HitHistoryPath) { return }
        if ($p = $script:HitPending) {
            $script:HitPending = $null
            $ms = [long][System.Diagnostics.Stopwatch]::GetElapsedTime($p.Start, $At).TotalMilliseconds
            # $LASTEXITCODE only changes for native commands, so it can be stale: trust it
            # only when the command failed and it is non-zero.
            $exit = if ($Success) { 0 } elseif ($ExitCode -is [int] -and $ExitCode -ne 0) { $ExitCode } else { 1 }
            $null = Add-HitLine -Path $script:HitHistoryPath -Line (ConvertTo-HitJsonLine (
                    New-HitRecord -Kind end -Id $p.Id -ExitCode $exit -DurationMs $ms))
        }
        $dir = Get-HitLocationPath $ExecutionContext.SessionState.Path.CurrentLocation
        if ($dir -ne $script:HitLastDir) {
            $script:HitLastDir = $dir
            $null = Add-HitLine -Path $script:HitHistoryPath -Line (ConvertTo-HitJsonLine (
                    New-HitRecord -Kind cd -Timestamp (ConvertTo-HitTimestamp (Get-HitNow)) -Dir $dir `
                        -Shell pwsh -HostName $script:HitHostName -SessionId $script:HitSessionId))
        }
        # Something set its own AddToHistoryHandler after us (e.g. a later init script):
        # chain it and take over again.
        if ($script:HitHandler -and
            -not [object]::ReferenceEquals((Get-PSReadLineOption).AddToHistoryHandler, $script:HitHandler)) {
            Register-HitHistoryHandler
        }
    } catch { }
}

function Register-HitHistoryHandler {
    $current = (Get-PSReadLineOption).AddToHistoryHandler
    if (-not [object]::ReferenceEquals($current, $script:HitHandler)) { $script:HitPrevHandler = $current }
    Set-PSReadLineOption -AddToHistoryHandler { param([string]$line) Invoke-HitAddToHistory $line }
    $script:HitHandler = (Get-PSReadLineOption).AddToHistoryHandler
}

# Wraps the current global prompt (starship, zoxide's wrapper, …). $? is captured first and
# restored before the wrapped prompt runs, so prompts that show the last status still see it.
# Each wrapper closes over its own $prev: after a re-wrap the chain can reach us twice
# (Invoke-HitPrompt is idempotent) but never loops.
function Register-HitPrompt {
    $current = $function:global:prompt
    if ([object]::ReferenceEquals($current, $script:HitPromptWrapper)) { return }
    $script:HitPrevPrompt = $current
    $prev = $current
    $hook = ${function:Invoke-HitPrompt}  # module-bound, so it can reach hit's private state
    $wrapper = {
        $hitOk = $global:?
        $hitAt = [System.Diagnostics.Stopwatch]::GetTimestamp()
        $hitExit = $global:LASTEXITCODE
        if (-not $hitOk) { Write-Error '' -ErrorAction Ignore }  # sets $? back to false
        $hitOut = & $prev
        & $hook $hitOk $hitExit $hitAt
        $hitOut
    }.GetNewClosure()
    $function:global:prompt = $wrapper
    $script:HitPromptWrapper = $function:global:prompt
}

# Starts recording. Called at the end of `hit init pwsh` with the history path the Go
# binary resolved, so there is one implementation of the path rules.
function Enable-Hit {
    param([Parameter(Mandatory)][string]$HistoryPath)
    if (-not (Get-Module PSReadLine)) { return }  # not an interactive console host
    $script:HitHistoryPath = $HistoryPath
    $script:HitSessionId = New-HitId
    $script:HitLastDir = Get-HitLocationPath $ExecutionContext.SessionState.Path.CurrentLocation
    Register-HitHistoryHandler
    Register-HitPrompt
    Register-HitKeyHandlers
}

# Stops recording and puts back the handler and prompt hit wrapped.
function Disable-Hit {
    if (-not $script:HitHistoryPath) { return }
    $script:HitHistoryPath = $null
    if ($script:HitHandler -and
        [object]::ReferenceEquals((Get-PSReadLineOption).AddToHistoryHandler, $script:HitHandler)) {
        Set-PSReadLineOption -AddToHistoryHandler $script:HitPrevHandler
    }
    if ($script:HitPromptWrapper -and
        [object]::ReferenceEquals($function:global:prompt, $script:HitPromptWrapper)) {
        $function:global:prompt = $script:HitPrevPrompt
    }
    $script:HitHandler = $null
    $script:HitPromptWrapper = $null
    $script:HitPending = $null
    if ($script:HitKeysBound) { Remove-HitKeyHandlers }
}

# ---------------------------------------------------------------------------------------
# The finder (S-003). The Ctrl+R handler is a thin binding: it reads the prompt buffer
# through an adapter, runs `hit search`, and puts the result back. Tests replace the
# adapter and the runner, so handlers can be exercised with no keyboard and no binary.
# ---------------------------------------------------------------------------------------

$script:HitKeysBound = $false

# Line editor adapter (S-015). Every call into PSReadLine goes through this.
$script:HitEditor = @{
    GetBuffer = {
        $line = $null; $cursor = $null
        [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)
        $line
    }
    SetBuffer = {
        param([string]$Text)
        [Microsoft.PowerShell.PSConsoleReadLine]::RevertLine()
        [Microsoft.PowerShell.PSConsoleReadLine]::Insert($Text)
    }
    Redraw    = { [Microsoft.PowerShell.PSConsoleReadLine]::InvokePrompt() }
}

# Runs the binary. Replaced in tests by one that writes a canned choice.
$script:HitRunner = {
    param([string[]]$Arguments)
    $null = & $script:HitExe @Arguments   # the TUI draws on stderr; stdout stays clean
    $LASTEXITCODE
}

# Builds the argument list for `hit search`. Pure, so tests can check it.
function New-HitSearchArguments {
    param([string]$Query, [string]$OutFile, [string]$Scope = 'all')
    @(
        'search'
        '--query', $Query
        '--out', $OutFile
        '--scope', $Scope
        '--cwd', (Get-HitLocationPath $ExecutionContext.SessionState.Path.CurrentLocation)
        '--session', $script:HitSessionId
        '--host', $script:HitHostName
        '--shell', 'pwsh'
    )
}

# Ctrl+R: open the finder seeded with what's already typed, then replace the buffer with
# whatever comes back. Esc in the finder leaves the prompt untouched.
function Invoke-HitFinder {
    $out = [System.IO.Path]::GetTempFileName()
    try {
        $query = & $script:HitEditor.GetBuffer
        $code = & $script:HitRunner (New-HitSearchArguments -Query $query -OutFile $out -Scope $script:HitScope)
        if ($code -ne 0) { return }
        $choice = $null
        if (Test-Path -LiteralPath $out) {
            $text = [System.IO.File]::ReadAllText($out)
            if ($text.Trim()) { $choice = ConvertFrom-Json $text }
        }
        if ($choice -and $choice.action -in @('insert', 'edit') -and $choice.cmd) {
            & $script:HitEditor.SetBuffer $choice.cmd
        }
    } catch {
        # Principle 7: a broken finder must never break the prompt.
    } finally {
        Remove-Item -LiteralPath $out -ErrorAction SilentlyContinue
        & $script:HitEditor.Redraw
    }
}

$script:HitScope = 'all'

# Binds Ctrl+R. In vi edit mode a chord must be bound per vi mode, and the owner uses
# `Set-PSReadLineOption -EditMode vi`, so bind insert and command mode too (DESIGN §14.1).
function Register-HitKeyHandlers {
    $common = @{ ScriptBlock = { Invoke-HitFinder }; BriefDescription = 'HitFinder'
        Description = 'Search hit history'
    }
    if ((Get-PSReadLineOption).EditMode -eq 'Vi') {
        Set-PSReadLineKeyHandler -Chord 'Ctrl+r' -ViMode Insert @common
        Set-PSReadLineKeyHandler -Chord 'Ctrl+r' -ViMode Command @common
    } else {
        Set-PSReadLineKeyHandler -Chord 'Ctrl+r' @common
    }
    $script:HitKeysBound = $true
}

# Puts Ctrl+R back to PSReadLine's own reverse search.
function Remove-HitKeyHandlers {
    $common = @{ Function = 'ReverseSearchHistory' }
    if ((Get-PSReadLineOption).EditMode -eq 'Vi') {
        Set-PSReadLineKeyHandler -Chord 'Ctrl+r' -ViMode Insert @common
        Set-PSReadLineKeyHandler -Chord 'Ctrl+r' -ViMode Command @common
    } else {
        Set-PSReadLineKeyHandler -Chord 'Ctrl+r' @common
    }
    $script:HitKeysBound = $false
}
