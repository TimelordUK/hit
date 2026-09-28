# Shows the colours a category in config.toml can use (DESIGN §17.1), drawn in this
# terminal so you can pick by eye. hit accepts the 16 names below or any "#rrggbb".
#   ./scripts/colors.ps1                       # the 16 names, then a palette of hex values
#   ./scripts/colors.ps1 '#ff8800' '#2aa198'   # preview your own hex values
#
# The names follow your terminal's colour scheme, so they look different in each theme;
# a hex value looks the same everywhere.
param([string[]]$Hex)

$reset = $PSStyle.Reset

function Show-Swatch([string]$Fg, [string]$Label) {
    # A block, then the label drawn in the colour, the way a category mark reads in a row.
    "  $Fg████$reset  $Fg$Label$reset"
}

function ConvertTo-Fg([string]$HexValue) {
    $b = [Convert]::FromHexString($HexValue.TrimStart('#'))
    $PSStyle.Foreground.FromRgb($b[0], $b[1], $b[2])
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
