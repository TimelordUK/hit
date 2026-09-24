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
    if ($env:HIT_SERVER) {
        Enable-HitServer -Idle ($env:HIT_SERVER_IDLE ? $env:HIT_SERVER_IDLE : '30m')
    }
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
    $script:HitServerMode = $false
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
    # A one-line note above the prompt, then redraw so the buffer is untouched.
    Notify    = {
        param([string]$Message)
        [Microsoft.PowerShell.PSConsoleReadLine]::InvokePrompt(0)
        Write-Host $Message -ForegroundColor DarkGray
        [Microsoft.PowerShell.PSConsoleReadLine]::InvokePrompt()
    }
}

# Appends to $TEMP\hit-debug.log when HIT_DEBUG is set. The finder swallows its errors so a
# failure can never break the prompt, which also hides them: this is how we get them back.
function Write-HitDebug([string]$Message) {
    if (-not $env:HIT_DEBUG) { return }
    Add-HitLogLine $Message
}

# The same log under the other switch: HIT_TIMING is for one number, and should not
# oblige you to wade through every key press to find it.
function Write-HitTiming([string]$Message) {
    if (-not $env:HIT_TIMING) { return }
    Add-HitLogLine $Message
}

function Add-HitLogLine([string]$Message) {
    try {
        Add-Content -LiteralPath (Join-Path ([System.IO.Path]::GetTempPath()) 'hit-debug.log') `
            -Value ('{0:HH:mm:ss.fff}  {1}' -f (Get-Date), $Message)
    } catch { }
}

# ---------------------------------------------------------------------------------------
# Phase timings (S-024). On with HIT_TIMING. Stopwatch ticks, so a clock change mid-recall
# cannot bend the numbers. The shell can only see its own half: the gap between
# Process.Start and the binary running is reported by the binary itself, which is why it
# is told when it was spawned (--started-at). See DESIGN §15.
# ---------------------------------------------------------------------------------------

function New-HitTimeline {
    if (-not $env:HIT_TIMING) { return $null }
    [pscustomobject]@{
        Start = [System.Diagnostics.Stopwatch]::GetTimestamp()
        Marks = [System.Collections.Generic.List[object]]::new()
    }
}

function Add-HitMark($Timeline, [string]$Name) {
    if (-not $Timeline) { return }
    $Timeline.Marks.Add([pscustomobject]@{
            Name = $Name; At = [System.Diagnostics.Stopwatch]::GetTimestamp()
        })
}

# The one-line report, each phase measured from the one before it.
function Format-HitTimeline($Timeline) {
    if (-not $Timeline -or $Timeline.Marks.Count -eq 0) { return '' }
    $parts = [System.Collections.Generic.List[string]]::new()
    $prev = $Timeline.Start
    foreach ($m in $Timeline.Marks) {
        $ms = [System.Diagnostics.Stopwatch]::GetElapsedTime($prev, $m.At).TotalMilliseconds
        $parts.Add(('{0} {1:F0}' -f $m.Name, $ms))
        $prev = $m.At
    }
    $total = [System.Diagnostics.Stopwatch]::GetElapsedTime($Timeline.Start, $prev).TotalMilliseconds
    $parts.Add(('total {0:F0}' -f $total))
    ($parts -join ' · ') + ' ms'
}

# Runs the binary and waits. Replaced in tests by one that writes a canned choice.
#
# Not `& $exe …`: inside a key handler PowerShell collects a native command's output into
# the pipeline, so the child's stdout is a pipe and the finder draws into nothing (the
# symptom was Ctrl+R "doing nothing" while the binary ran happily). Starting the process
# with UseShellExecute=$false and no redirection lets it inherit the real console, so the
# TUI draws and reads keys directly. ArgumentList quotes each argument properly, which
# matters because --query carries the prompt buffer verbatim, newlines and all.
$script:HitRunner = {
    param([string[]]$Arguments)
    $psi = [System.Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = $script:HitExe
    foreach ($a in $Arguments) { $null = $psi.ArgumentList.Add($a) }
    $psi.UseShellExecute = $false
    $proc = [System.Diagnostics.Process]::Start($psi)
    $proc.WaitForExit()
    $proc.ExitCode
}

# Builds the argument list for `hit search`. Pure, so tests can check it.
function New-HitSearchArguments {
    param(
        [string]$Query, [string]$OutFile, [string]$Scope = 'all',
        # Unix ms at the moment we spawn the binary. 0 leaves the flag off entirely, so
        # nothing changes when timing is off.
        [long]$StartedAt = 0
    )
    $a = [System.Collections.Generic.List[string]]::new()
    $a.AddRange([string[]]@(
            'search'
            '--query', $Query
            '--out', $OutFile
            '--scope', $Scope
            '--cwd', (Get-HitLocationPath $ExecutionContext.SessionState.Path.CurrentLocation)
            '--session', $script:HitSessionId
            '--host', $script:HitHostName
            '--shell', 'pwsh'
        ))
    if ($StartedAt -gt 0) { $a.AddRange([string[]]@('--started-at', [string]$StartedAt)) }
    , $a.ToArray()
}

# ---------------------------------------------------------------------------------------
# The resident finder (C-031). Opt-in with $env:HIT_SERVER, or Enable-HitServer at any
# prompt; Disable-HitServer puts it back with no restart, which matters because this sits
# on Ctrl+R and has to be abandonable the moment it misbehaves.
#
# Why: on a managed machine the cost of Ctrl+R is not the search, it is starting a process
# at all. A binary the endpoint agent has not seen costs seconds on first launch and
# milliseconds once known, and that verdict is evicted through the day (S-029 measured
# 3505 ms against 58 ms on the same file). So the process is started once per shell and
# asked to draw, over a pipe only this account can open.
#
# The cold spawn stays as the fallback for every failure, because a finder that cannot
# be reached must still open (principle 7).
# ---------------------------------------------------------------------------------------

$script:HitServerMode = $false
$script:HitServerIdle = '30m'
$script:HitServerStarted = $false   # we have tried to start one this session

# Must agree with ipc.Name on the Go side; the contract test pins both to the same
# vectors. Session ids are ULIDs, so in practice this only lower-cases them.
function Get-HitPipeName([string]$Session = $script:HitSessionId) {
    $s = ($Session -replace '[^A-Za-z0-9_-]', '')
    if (-not $s) { $s = 'default' }
    if ($s.Length -gt 48) { $s = $s.Substring(0, 48) }
    'hit-' + $s.ToLowerInvariant()
}

function Enable-HitServer {
    param([string]$Idle = $script:HitServerIdle)
    $script:HitServerIdle = $Idle
    $script:HitServerMode = $true
    $script:HitServerStarted = $false
    Write-HitDebug "server: enabled, idle $Idle"
}

function Disable-HitServer {
    $script:HitServerMode = $false
    Write-HitDebug 'server: disabled'
}

# Starts the resident process. No redirection, so it inherits this console and can draw
# on it exactly as a spawned finder does. --quiet because anything it printed would land
# in the middle of the prompt.
function Start-HitServer {
    if ($script:HitServerStarted) { return }
    $script:HitServerStarted = $true
    if (-not $script:HitExe) { return }
    try {
        $psi = [System.Diagnostics.ProcessStartInfo]::new()
        $psi.FileName = $script:HitExe
        foreach ($a in @('serve', '--session', $script:HitSessionId, '--idle', $script:HitServerIdle,
                '--parent', [string]$PID, '--quiet')) {
            $null = $psi.ArgumentList.Add($a)
        }
        $psi.UseShellExecute = $false
        $null = [System.Diagnostics.Process]::Start($psi)
        Write-HitDebug "server: started, idle $script:HitServerIdle, parent $PID"
    } catch {
        Write-HitDebug ("server start failed: " + $_.Exception.Message)
    }
}

# One request/response over the pipe. $Wait means the server is drawing the finder, so
# the read is unbounded — the same as waiting on a spawned finder, which also takes as
# long as you look at it. Without it the read is bounded, so a wedged server can never
# hold the prompt.
function Invoke-HitServerRequest {
    param(
        [Parameter(Mandatory)][hashtable]$Request,
        [int]$ConnectTimeoutMs = 400,
        [int]$ReadTimeoutMs = 1500,
        [switch]$Wait
    )
    $pipe = $null
    try {
        $pipe = [System.IO.Pipes.NamedPipeClientStream]::new(
            '.', (Get-HitPipeName), [System.IO.Pipes.PipeDirection]::InOut)
        $pipe.Connect($ConnectTimeoutMs)
        $json = ConvertTo-Json -InputObject $Request -Compress -Depth 3
        $bytes = $script:HitUtf8.GetBytes($json + "`n")
        $pipe.Write($bytes, 0, $bytes.Length)
        $pipe.Flush()

        $reader = [System.IO.StreamReader]::new($pipe, $script:HitUtf8)
        $task = $reader.ReadLineAsync()
        if (-not $Wait) {
            if (-not $task.Wait($ReadTimeoutMs)) {
                Write-HitDebug 'server: no answer in time'
                return $null
            }
        }
        $line = $task.GetAwaiter().GetResult()
        if (-not $line) { return $null }
        ConvertFrom-Json $line
    } catch {
        Write-HitDebug ("server: " + $_.Exception.Message)
        $null
    } finally {
        if ($pipe) { $pipe.Dispose() }
    }
}

# Is there a live server? A ping draws nothing, so this is safe to ask before committing
# the console to it.
function Test-HitServer {
    $res = Invoke-HitServerRequest -Request @{ ping = $true }
    [bool]($res -and $res.pong)
}

# What this shell knows about its resident finder (S-032). `hit status` lists every server
# on the machine; this answers for this session alone, without spawning anything, and adds
# what only the shell knows — whether server mode is on at all.
function Get-HitStatus {
    $res = if ($script:HitSessionId) { Invoke-HitServerRequest -Request @{ ping = $true } }
    $answering = [bool]($res -and $res.pong)
    $srv = if ($answering) { $res.server }
    $name = Get-HitPipeName
    [pscustomobject]@{
        ServerMode = $script:HitServerMode
        Session    = $script:HitSessionId
        Pipe       = if ($IsWindows) { '\\.\pipe\' + $name } else { $name }
        Answering  = $answering
        Pid        = if ($srv) { $srv.pid }
        Parent     = if ($srv) { $srv.parent }
        ParentIsUs = [bool]($srv -and $srv.parent -eq $PID)
        Version    = if ($answering) { $res.version }
        # Started before the last install, so Ctrl+R here is still the old code (S-031).
        Stale      = [bool]($answering -and $script:HitVersion -and $res.version -ne $script:HitVersion)
        Started    = if ($srv) { $srv.started }
        LastUsed   = if ($srv) { $srv.lastUsed }
        Requests   = if ($srv) { $srv.requests }
        Exe        = if ($srv) { $srv.exe }
    }
}

# Asks the resident process to draw. Returns the choice, or $null to mean "not served" —
# never throws, so the caller can simply fall through to spawning.
function Invoke-HitFinderOnServer([string]$Query) {
    if (-not (Test-HitServer)) { return $null }
    $res = Invoke-HitServerRequest -Wait -Request @{
        query   = $Query
        scope   = $script:HitScope
        cwd     = (Get-HitLocationPath $ExecutionContext.SessionState.Path.CurrentLocation)
        session = $script:HitSessionId
        host    = $script:HitHostName
        shell   = 'pwsh'
    }
    if (-not $res) { return $null }
    if ($res.error) {
        Write-HitDebug ("server error: " + $res.error)
        return $null
    }
    $res
}

# The original path: spawn the finder and read its answer out of a temp file.
function Invoke-HitFinderBySpawn([string]$Query, $Timeline) {
    $out = [System.IO.Path]::GetTempFileName()
    Add-HitMark $Timeline 'temp'
    try {
        $startedAt = if ($Timeline) { [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() } else { 0 }
        $arguments = New-HitSearchArguments -Query $Query -OutFile $out -Scope $script:HitScope `
            -StartedAt $startedAt
        Write-HitDebug ("run: {0} {1}" -f $script:HitExe, ($arguments -join ' '))
        $code = & $script:HitRunner $arguments
        # "run" spans the whole finder session, so it includes however long you spent
        # looking at it. The binary reports its own spawn-to-first-paint separately.
        Add-HitMark $Timeline 'run'
        Write-HitDebug "exit: $code"
        if ($code -ne 0) { return $null }
        $choice = $null
        if (Test-Path -LiteralPath $out) {
            $text = [System.IO.File]::ReadAllText($out)
            Write-HitDebug "choice: $text"
            if ($text.Trim()) { $choice = ConvertFrom-Json $text }
        } else {
            Write-HitDebug "no out file"
        }
        Add-HitMark $Timeline 'readback'
        $choice
    } finally {
        Remove-Item -LiteralPath $out -ErrorAction SilentlyContinue
    }
}

# Ctrl+R: open the finder seeded with what's already typed, then replace the buffer with
# whatever comes back. Esc in the finder leaves the prompt untouched.
function Invoke-HitFinder {
    $timeline = New-HitTimeline
    try {
        $query = & $script:HitEditor.GetBuffer
        Add-HitMark $timeline 'buffer'

        $choice = $null
        if ($script:HitServerMode) {
            $choice = Invoke-HitFinderOnServer $query
            Add-HitMark $timeline 'server'
            if (-not $choice) {
                # Nothing there, or it would not answer: this recall pays the old price,
                # and one is started so the next does not.
                Start-HitServer
            }
        }
        if (-not $choice) {
            $choice = Invoke-HitFinderBySpawn $query $timeline
        }
        if ($choice -and $choice.action -in @('insert', 'edit') -and $choice.cmd) {
            & $script:HitEditor.SetBuffer $choice.cmd
        }
    } catch {
        # Principle 7: a broken finder must never break the prompt.
        Write-HitDebug ("error: " + ($_ | Out-String))
    } finally {
        & $script:HitEditor.Redraw
        Add-HitMark $timeline 'redraw'
        if ($report = Format-HitTimeline $timeline) {
            Write-HitTiming "timing (pwsh): $report"
        }
    }
}

$script:HitScope = 'all'

# Binds Ctrl+R. In vi edit mode a chord must be bound per vi mode, and the owner uses
# `Set-PSReadLineOption -EditMode vi`, so bind insert and command mode too (DESIGN §14.1).
function Register-HitKeyHandlers {
    $finder = @{ ScriptBlock = { Invoke-HitFinder }; BriefDescription = 'HitFinder'
        Description = 'Search hit history'
    }
    # Alt+M: neither Zellij, Windows Terminal nor PSReadLine claims it (DESIGN §14.1).
    $toggle = @{ ScriptBlock = { Invoke-HitToggleMultiline }; BriefDescription = 'HitToggleMultiline'
        Description = 'Split a command over lines, or join it back onto one'
    }
    Set-HitKeyHandler -Chord 'Ctrl+r' -Options $finder
    Set-HitKeyHandler -Chord 'Alt+m' -Options $toggle
    $script:HitKeysBound = $true
}

# Binds a chord, covering both vi modes when vi editing is on.
function Set-HitKeyHandler {
    param([Parameter(Mandatory)][string]$Chord, [Parameter(Mandatory)][hashtable]$Options)
    if ((Get-PSReadLineOption).EditMode -eq 'Vi') {
        Set-PSReadLineKeyHandler -Chord $Chord -ViMode Insert @Options
        Set-PSReadLineKeyHandler -Chord $Chord -ViMode Command @Options
    } else {
        Set-PSReadLineKeyHandler -Chord $Chord @Options
    }
}

# Puts Ctrl+R back to PSReadLine's own reverse search and drops Alt+M.
function Remove-HitKeyHandlers {
    Set-HitKeyHandler -Chord 'Ctrl+r' -Options @{ Function = 'ReverseSearchHistory' }
    Set-HitKeyHandler -Chord 'Alt+m' -Options @{ Function = 'SelfInsert' }
    $script:HitKeysBound = $false
}

# ---------------------------------------------------------------------------------------
# One line ↔ many lines (S-004). Both directions go through PowerShell's own parser and are
# checked by re-parsing the result: if the token stream differs in any way, the original is
# returned untouched. Worst case is "not reformatted", never "broken" (DESIGN §7).
# The stored history record is never changed: this only rewrites the prompt buffer.
# ---------------------------------------------------------------------------------------

$script:HitLF = [string][char]10
$script:HitBacktick = [string][char]96

# Tokens that open and close a nesting level. Only breaks at depth 0 are made, so
# parameters inside { }, ( ), @{ }, $( ) and [ ] stay where they are.
$script:HitOpenKinds = @('LParen', 'LCurly', 'LBracket', 'AtParen', 'AtCurly', 'DollarParen')
$script:HitCloseKinds = @('RParen', 'RCurly', 'RBracket')

function Get-HitTokens([string]$Command, [ref]$Errors) {
    $tokens = $null
    $parseErrors = $null
    $null = [System.Management.Automation.Language.Parser]::ParseInput($Command, [ref]$tokens, [ref]$parseErrors)
    $Errors.Value = $parseErrors
    $tokens
}

# The signature used to prove a rewrite changed nothing but layout: every token's kind and
# text, ignoring newlines and line continuations.
function Get-HitTokenSignature([string]$Command) {
    $errors = $null
    $tokens = Get-HitTokens $Command ([ref]$errors)
    if ($errors.Count) { return $null }
    ($tokens |
        Where-Object { $_.Kind -notin @('NewLine', 'LineContinuation', 'EndOfInput') } |
        ForEach-Object { $_.Kind.ToString() + ':' + $_.Text }) -join [string][char]31
}

# $true when $Rewritten is the same command as $Original, laid out differently.
function Test-HitSameCommand([string]$Original, [string]$Rewritten) {
    $a = Get-HitTokenSignature $Original
    $b = Get-HitTokenSignature $Rewritten
    $null -ne $a -and $a -eq $b
}

# Splits a single-line command at its top-level parameters and pipes:
#   irm -Uri x -Method Post | select -First 2
# becomes
#   irm `
#       -Uri x `
#       -Method Post |
#       select -First 2
function Format-HitCommand {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][AllowEmptyString()][string]$Command,
        [string]$Indent = '    ',
        [ref]$Reason
    )
    $setReason = { param($why) if ($Reason) { $Reason.Value = $why } }
    if ([string]::IsNullOrWhiteSpace($Command)) { return $Command }
    if ($Command -match '[\r\n]') { & $setReason 'already on several lines'; return $Command }

    $errors = $null
    $tokens = Get-HitTokens $Command ([ref]$errors)
    if ($errors.Count) { & $setReason "it doesn't parse"; return $Command }

    # Pipeline stages line up under each other; a stage's parameters sit one level deeper.
    $breaks = [System.Collections.Generic.List[object]]::new()
    $depth = 0
    $stage = 0
    for ($i = 0; $i -lt $tokens.Count; $i++) {
        $t = $tokens[$i]
        if ($t.Kind -in $script:HitOpenKinds) { $depth++; continue }
        if ($t.Kind -in $script:HitCloseKinds) { $depth--; continue }
        if ($depth -ne 0) { continue }
        if ($t.Kind -eq 'Parameter' -and $i -gt 0) {
            $level = if ($stage -eq 0) { 1 } else { 2 }
            $breaks.Add([pscustomobject]@{ Offset = $t.Extent.StartOffset; AfterPipe = $false; Level = $level })
        } elseif ($t.Kind -eq 'Pipe') {
            $stage++
            $breaks.Add([pscustomobject]@{ Offset = $t.Extent.EndOffset; AfterPipe = $true; Level = 1 })
        }
    }
    if ($breaks.Count -eq 0) { & $setReason 'there is nothing to split on'; return $Command }

    $sb = [System.Text.StringBuilder]::new()
    $pos = 0
    foreach ($b in $breaks) {
        $chunk = $Command.Substring($pos, $b.Offset - $pos)
        if ($pos -gt 0) { $chunk = $chunk.TrimStart() }  # the indent replaces it
        $pad = $Indent * $b.Level
        if ($b.AfterPipe) {
            # A trailing pipe continues the line by itself: no backtick needed.
            $null = $sb.Append($chunk.TrimEnd()).Append($script:HitLF).Append($pad)
        } else {
            $null = $sb.Append($chunk.TrimEnd()).Append(' ').Append($script:HitBacktick).
                Append($script:HitLF).Append($pad)
        }
        $pos = $b.Offset
    }
    $null = $sb.Append($Command.Substring($pos).TrimStart())
    $result = $sb.ToString()

    if (-not (Test-HitSameCommand $Command $result)) {
        & $setReason 'the check refused it'
        return $Command
    }
    $result
}

# A bare newline may only be collapsed when the line before it cannot stand alone: after a
# pipe, a chained operator, a comma, an assignment or an opening brace. Anywhere else the
# newline separates two statements, and joining them would need a `;` — that is a rewrite,
# not a reflow, so the command is left alone instead.
$script:HitContinuingKinds = @(
    'Pipe', 'AndAnd', 'OrOr', 'Ampersand', 'Comma', 'Equals', 'PlusEquals', 'MinusEquals',
    'MultiplyEquals', 'DivideEquals', 'RemainderEquals', 'LCurly', 'LParen', 'LBracket',
    'AtCurly', 'AtParen', 'DollarParen', 'Semi'
)

# The reverse: brings a continued command back onto one line for editing. Newlines that
# live *inside* a token (a here-string, or a double-quoted string written across lines)
# belong to the command's text and are left exactly as they are.
function Join-HitCommand {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][AllowEmptyString()][string]$Command,
        [ref]$Reason
    )
    $setReason = { param($why) if ($Reason) { $Reason.Value = $why } }
    if ([string]::IsNullOrWhiteSpace($Command)) { return $Command }
    if ($Command -notmatch '[\r\n]') { & $setReason 'already on one line'; return $Command }

    $errors = $null
    $tokens = Get-HitTokens $Command ([ref]$errors)
    if ($errors.Count) { & $setReason "it doesn't parse"; return $Command }

    $significant = @($tokens | Where-Object { $_.Kind -notin @('NewLine', 'LineContinuation', 'EndOfInput') })
    $sb = [System.Text.StringBuilder]::new()
    $pos = 0
    $pendingSpace = $false
    $seen = 0
    foreach ($t in $tokens) {
        if ($t.Kind -eq 'EndOfInput') { break }
        if ($t.Kind -eq 'NewLine') {
            $prev = if ($seen -gt 0) { $significant[$seen - 1].Kind } else { $null }
            if ($prev -and $prev -notin $script:HitContinuingKinds) {
                & $setReason 'it is more than one statement'
                return $Command
            }
            $pendingSpace = $true
            $pos = $t.Extent.EndOffset
            continue
        }
        if ($t.Kind -eq 'LineContinuation') {
            $pendingSpace = $true
            $pos = $t.Extent.EndOffset
            continue
        }
        $seen++
        $gap = $Command.Substring($pos, $t.Extent.StartOffset - $pos)
        if ($pendingSpace -or $gap -match '[\r\n]') {
            if ($sb.Length -gt 0) { $null = $sb.Append(' ') }
        } elseif ($gap) {
            $null = $sb.Append($gap)
        }
        $null = $sb.Append($t.Text)
        $pendingSpace = $false
        $pos = $t.Extent.EndOffset
    }
    $result = $sb.ToString().Trim()

    if (-not (Test-HitSameCommand $Command $result)) {
        & $setReason 'the check refused it'
        return $Command
    }
    if ($result -eq $Command) { & $setReason 'its newlines are inside a string' }
    $result
}

# Alt+M: one line ↔ many lines, whichever way the buffer isn't. When nothing can be done
# safely it says so instead of appearing to ignore the key.
function Invoke-HitToggleMultiline {
    try {
        $buffer = & $script:HitEditor.GetBuffer
        if ([string]::IsNullOrWhiteSpace($buffer)) { return }
        $reason = $null
        $result = if ($buffer -match '[\r\n]') {
            Join-HitCommand $buffer -Reason ([ref]$reason)
        } else {
            Format-HitCommand $buffer -Reason ([ref]$reason)
        }
        if ($result -ne $buffer) {
            & $script:HitEditor.SetBuffer $result
            return
        }
        Write-HitDebug "toggle: unchanged ($reason)"
        if ($reason -and $script:HitEditor.Notify) { & $script:HitEditor.Notify "hit: left as it is, $reason" }
    } catch {
        Write-HitDebug ("toggle error: " + ($_ | Out-String))
    }
}
