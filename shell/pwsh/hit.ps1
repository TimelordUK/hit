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
