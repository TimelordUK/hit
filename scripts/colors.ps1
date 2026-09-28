# Shows the colours a category in config.toml can use (DESIGN §17.1), drawn in this
# terminal so you can pick by eye. hit accepts the 16 names below or any "#rrggbb".
#   ./scripts/colors.ps1                       # the 16 names, then a palette of hex values
#   ./scripts/colors.ps1 '#ff8800' '#2aa198'   # preview your own hex values
#   ./scripts/colors.ps1 -Gradient             # the hue spectrum, sampled into 24 picks
#   ./scripts/colors.ps1 -Gradient '#ff5f5f' '#5fafff' -Steps 8   # a set between two colours
#   ./scripts/colors.ps1 -Gradient -Light 0.45 -Saturation 0.9    # a darker, stronger spectrum
#
# The names follow your terminal's colour scheme, so they look different in each theme;
# a hex value looks the same everywhere. A smooth band means true colour; visible steps
# mean the terminal is falling back to 256 colours.
param(
    [Parameter(Position = 0, ValueFromRemainingArguments)][string[]]$Hex,
    [switch]$Gradient,
    [int]$Steps = 24,          # how many picks to sample from the gradient
    [double]$Light = 0.6,      # spectrum lightness, 0..1
    [double]$Saturation = 0.7  # spectrum saturation, 0..1
)

$reset = $PSStyle.Reset

function Show-Swatch([string]$Fg, [string]$Label) {
    # A block, then the label drawn in the colour, the way a category mark reads in a row.
    "  $Fg████$reset  $Fg$Label$reset"
}

function ConvertTo-Fg([string]$HexValue) {
    $b = [Convert]::FromHexString($HexValue.TrimStart('#'))
    $PSStyle.Foreground.FromRgb($b[0], $b[1], $b[2])
}

function ConvertFrom-Hsl([double]$H, [double]$S, [double]$L) {
    $c = (1 - [math]::Abs(2 * $L - 1)) * $S
    $x = $c * (1 - [math]::Abs((($H / 60) % 2) - 1))
    $m = $L - $c / 2
    $r, $g, $b = switch ([int][math]::Floor($H / 60) % 6) {
        0 { $c, $x, 0 } 1 { $x, $c, 0 } 2 { 0, $c, $x }
        3 { 0, $x, $c } 4 { $x, 0, $c } 5 { $c, 0, $x }
    }
    , @([int](($r + $m) * 255), [int](($g + $m) * 255), [int](($b + $m) * 255))
}

# t in 0..1 → RGB: between two hex colours, or round the hue circle.
function Get-GradientRgb([double]$T, [byte[]]$From, [byte[]]$To) {
    if ($From) {
        , @(0..2 | ForEach-Object { [int]($From[$_] + ($To[$_] - $From[$_]) * $T) })
    } else {
        ConvertFrom-Hsl (360 * $T) $Saturation $Light
    }
}

function Format-Hex([int[]]$Rgb) { '#{0:x2}{1:x2}{2:x2}' -f $Rgb[0], $Rgb[1], $Rgb[2] }

if ($Gradient) {
    $from = $to = $null
    if ($Hex) {
        if ($Hex.Count -ne 2 -or ($Hex -notmatch '^#?[0-9A-Fa-f]{6}$').Count) {
            throw 'give -Gradient two #rrggbb colours, or none for the spectrum'
        }
        $from = [Convert]::FromHexString($Hex[0].TrimStart('#'))
        $to = [Convert]::FromHexString($Hex[1].TrimStart('#'))
    }
    # The smooth band: one cell per column, as wide as the window allows.
    $width = [math]::Max(20, [math]::Min(96, $Host.UI.RawUI.WindowSize.Width - 4))
    $band = -join (0..($width - 1) | ForEach-Object {
            $rgb = Get-GradientRgb ($_ / ($width - 1)) $from $to
            $PSStyle.Background.FromRgb($rgb[0], $rgb[1], $rgb[2]) + ' '
        })
    ''
    "  $band$reset"
    "  $band$reset"
    ''
    # The picks, evenly spaced along it. The spectrum wraps, so its last pick is not red again.
    $n = [math]::Max(2, $Steps)
    $den = if ($from) { $n - 1 } else { $n }
    $picks = 0..($n - 1) | ForEach-Object {
        $h = Format-Hex (Get-GradientRgb ($_ / $den) $from $to)
        Show-Swatch (ConvertTo-Fg $h) $h
    }
    for ($i = 0; $i -lt $picks.Count; $i += 4) { $picks[$i..([math]::Min($i + 3, $picks.Count - 1))] -join '' }
    return
}

if ($Hex) {
    foreach ($h in $Hex) {
        if ($h -notmatch '^#?[0-9A-Fa-f]{6}$') { Write-Warning "$h is not #rrggbb"; continue }
        $h = '#' + $h.TrimStart('#').ToLower()
        Show-Swatch (ConvertTo-Fg $h) $h
    }
    return
}

'Names (color = "red"):'
foreach ($n in 'black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white') {
    $bright = 'Bright' + (Get-Culture).TextInfo.ToTitleCase($n)
    $left = Show-Swatch $PSStyle.Foreground.($n) $n.PadRight(8)
    $right = Show-Swatch $PSStyle.Foreground.($bright) "bright-$n"
    "$left$right"
}
"  (gray / grey = bright-black)"
''

'Hex (color = "#ff8700"):'
$palette = @(
    '#d70000', '#ff5f5f', '#ff8700', '#ffaf00', '#d7af00', '#ffd75f',
    '#5f8700', '#87af00', '#5faf5f', '#00af87', '#2aa198', '#5fd7d7',
    '#0087d7', '#5fafff', '#268bd2', '#5f5fd7', '#8787ff', '#af87ff',
    '#8700af', '#af5fd7', '#d75faf', '#ff5faf', '#af8787', '#d7af87',
    '#875f00', '#af875f', '#808080', '#a8a8a8', '#585858', '#d0d0d0'
)
for ($i = 0; $i -lt $palette.Count; $i += 3) {
    ($palette[$i..($i + 2)] | ForEach-Object { Show-Swatch (ConvertTo-Fg $_) $_ }) -join ''
}
