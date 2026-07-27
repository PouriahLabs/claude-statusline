# Renders the status line to PNG for the README.
#
# It runs the real binary, parses the ANSI it emits, and draws it with the
# installed Nerd Font -- so the images are the genuine output, not a mockup.
# Runs are drawn whole (not per character) so glyphs with a double-width
# advance still line up.

param(
    [string]$Exe  = (Join-Path $PSScriptRoot '..\statusline.exe'),
    [string]$Out  = (Join-Path $PSScriptRoot '..\docs'),
    [string]$Font = 'CaskaydiaCove NF',
    [int]$Size    = 17
)

Add-Type -AssemblyName System.Drawing
$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force -Path $Out | Out-Null

$BG = [System.Drawing.Color]::FromArgb(12, 12, 12)

function Get-Line {
    param([string]$Json, [string[]]$ExtraArgs = @())
    $tmp = [IO.Path]::GetTempFileName()
    [IO.File]::WriteAllText($tmp, $Json, (New-Object Text.UTF8Encoding $false))
    $outFile = [IO.Path]::GetTempFileName()
    $argline = ($ExtraArgs -join ' ')
    & cmd /c "`"$Exe`" $argline < `"$tmp`" > `"$outFile`"" | Out-Null
    $bytes = [IO.File]::ReadAllBytes($outFile)
    Remove-Item $tmp, $outFile -Force
    ([Text.Encoding]::UTF8.GetString($bytes)).TrimEnd("`r", "`n")
}

# Split an ANSI string into runs of (text, fg, bg).
#
# The current colours are held in $curFg/$curBg, NOT $fg/$bg. PowerShell
# variable names are case-insensitive, so a local named $bg IS the terminal
# background $BG: the first `48;2` would overwrite $BG inside this function and
# the SGR-0 reset `$bg = $BG` would degrade into a self-assignment. The
# background then never reset, so the inter-pill separator space inherited the
# pill's colour and was painted as a one-cell sliver after every pill.
function Split-Runs {
    param([string]$S)
    $curFg = [System.Drawing.Color]::White
    $curBg = $BG
    $buf = New-Object Text.StringBuilder
    $i = 0
    $flush = {
        if ($buf.Length -gt 0) {
            $script:runs += [pscustomobject]@{ Text = $buf.ToString(); FG = $curFg; BG = $curBg }
            $buf.Clear() | Out-Null
        }
    }
    $script:runs = @()
    while ($i -lt $S.Length) {
        if ($S[$i] -eq [char]27 -and ($i + 1) -lt $S.Length -and $S[$i + 1] -eq '[') {
            & $flush
            $j = $i + 2
            while ($j -lt $S.Length -and $S[$j] -ne 'm') { $j++ }
            $params = $S.Substring($i + 2, $j - $i - 2) -split ';'
            $k = 0
            while ($k -lt $params.Count) {
                switch ($params[$k]) {
                    '0'  { $curFg = [System.Drawing.Color]::White; $curBg = $BG; $k++ }
                    '38' {
                        if ($params[$k+1] -eq '2') {
                            $curFg = [System.Drawing.Color]::FromArgb([int]$params[$k+2], [int]$params[$k+3], [int]$params[$k+4]); $k += 5
                        } else { $curFg = Convert-Palette ([int]$params[$k+2]); $k += 3 }
                    }
                    '48' {
                        if ($params[$k+1] -eq '2') {
                            $curBg = [System.Drawing.Color]::FromArgb([int]$params[$k+2], [int]$params[$k+3], [int]$params[$k+4]); $k += 5
                        } else { $curBg = Convert-Palette ([int]$params[$k+2]); $k += 3 }
                    }
                    default { $k++ }
                }
            }
            $i = $j + 1
            continue
        }
        $buf.Append($S[$i]) | Out-Null
        $i++
    }
    & $flush
    $script:runs
}

$script:pal = $null
function Convert-Palette {
    param([int]$N)
    if (-not $script:pal) {
        $p = New-Object 'System.Drawing.Color[]' 256
        $base = @(@(0,0,0),@(128,0,0),@(0,128,0),@(128,128,0),@(0,0,128),@(128,0,128),@(0,128,128),@(192,192,192),
                  @(128,128,128),@(255,0,0),@(0,255,0),@(255,255,0),@(0,0,255),@(255,0,255),@(0,255,255),@(255,255,255))
        for ($x = 0; $x -lt 16; $x++) { $p[$x] = [System.Drawing.Color]::FromArgb($base[$x][0], $base[$x][1], $base[$x][2]) }
        $lv = @(0,95,135,175,215,255); $x = 16
        foreach ($r in 0..5) { foreach ($g in 0..5) { foreach ($b in 0..5) {
            $p[$x] = [System.Drawing.Color]::FromArgb($lv[$r], $lv[$g], $lv[$b]); $x++ } } }
        foreach ($j in 0..23) { $v = 8 + $j * 10; $p[$x] = [System.Drawing.Color]::FromArgb($v,$v,$v); $x++ }
        $script:pal = $p
    }
    $script:pal[$N]
}

# Terminal columns occupied by a string. Surrogate pairs (the robot icon is
# U+F06A9, two UTF-16 units) occupy one cell, so count text elements rather
# than .Length.
function Get-CellCount {
    param([string]$S)
    $n = 0
    $e = [Globalization.StringInfo]::GetTextElementEnumerator($S)
    while ($e.MoveNext()) { $n++ }
    $n
}

function New-Shot {
    param([string[]]$Lines, [string]$File, [int]$PadX = 24, [int]$PadY = 18)

    $font = New-Object System.Drawing.Font($Font, $Size, [System.Drawing.FontStyle]::Regular,
                                           [System.Drawing.GraphicsUnit]::Pixel)
    $probe = New-Object System.Drawing.Bitmap 1, 1
    $pg = [System.Drawing.Graphics]::FromImage($probe)
    $pg.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::ClearTypeGridFit
    $fmt = [System.Drawing.StringFormat]::GenericTypographic
    $fmt.FormatFlags = $fmt.FormatFlags -bor [System.Drawing.StringFormatFlags]::MeasureTrailingSpaces

    # Lay out on a fixed cell grid, exactly like a terminal. Measuring each run
    # with MeasureString under-reports multi-character strings, so every run
    # crept leftwards and swallowed the one-cell gap between pills -- the chips
    # looked joined in the PNG while the terminal showed them separated.
    $cellW = $pg.MeasureString('0', $font, [int]::MaxValue, $fmt).Width

    $lineH = [int]([math]::Ceiling($font.GetHeight($pg))) + 8
    $parsed = @()
    $maxW = 0
    foreach ($l in $Lines) {
        $runs = Split-Runs $l
        $cells = 0
        foreach ($r in $runs) {
            $r | Add-Member -NotePropertyName Cells -NotePropertyValue (Get-CellCount $r.Text) -Force
            $cells += $r.Cells
        }
        $lineW = $cells * $cellW
        if ($lineW -gt $maxW) { $maxW = $lineW }
        $parsed += ,$runs
    }
    $pg.Dispose(); $probe.Dispose()

    $W = [int]($maxW + $PadX * 2)
    $H = $lineH * $parsed.Count + $PadY * 2
    $bmp = New-Object System.Drawing.Bitmap $W, $H
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::ClearTypeGridFit
    $g.Clear($BG)

    $y = $PadY
    foreach ($runs in $parsed) {
        $x = [single]$PadX
        foreach ($r in $runs) {
            $w = [single]($r.Cells * $cellW)

            # Clip to the run's own cell box before drawing. Powerline caps
            # paint ink wider than their advance width; a terminal clips every
            # glyph to its cell, GDI+ does not. Without this the caps bleed
            # sideways and swallow the one-cell gap between pills, which makes
            # the chips look joined in the PNG but not in the terminal.
            $cell = New-Object System.Drawing.RectangleF($x, [single]($y - 6), $w, [single]($lineH + 6))
            $g.SetClip($cell)

            $top = [single]($y - 3)
            $h   = [single]($lineH - 2)

            # Pill end-caps are drawn as geometry rather than as glyphs.
            # GDI+ rasterises the powerline PUA codepoints with ink that does
            # not sit inside its advance box, which closed the one-cell gap
            # between pills and made them look welded together -- the exact
            # thing a terminal avoids by clipping each glyph to its cell.
            # Colours, text and cell positions still come from the binary's
            # real ANSI output; only the cap shape is synthesised.
            $isCapL = $r.Text -eq [string][char]0xE0B6
            $isCapR = $r.Text -eq [string][char]0xE0B4
            $isArrL = $r.Text -eq [string][char]0xE0B2
            $isArrR = $r.Text -eq [string][char]0xE0B0

            if ($isCapL -or $isCapR -or $isArrL -or $isArrR) {
                $br = New-Object System.Drawing.SolidBrush $r.FG
                if ($isCapL) { $g.FillPie($br, $x, $top, [single]($w * 2), $h, 90, 180) }
                elseif ($isCapR) { $g.FillPie($br, [single]($x - $w), $top, [single]($w * 2), $h, 270, 180) }
                elseif ($isArrL) {
                    $pts = @([System.Drawing.PointF]::new([single]($x + $w), $top),
                             [System.Drawing.PointF]::new($x, [single]($top + $h / 2)),
                             [System.Drawing.PointF]::new([single]($x + $w), [single]($top + $h)))
                    $g.FillPolygon($br, $pts)
                } else {
                    $pts = @([System.Drawing.PointF]::new($x, $top),
                             [System.Drawing.PointF]::new([single]($x + $w), [single]($top + $h / 2)),
                             [System.Drawing.PointF]::new($x, [single]($top + $h)))
                    $g.FillPolygon($br, $pts)
                }
                $br.Dispose()
            } else {
                if ($r.BG -ne $BG) {
                    $br = New-Object System.Drawing.SolidBrush $r.BG
                    $g.FillRectangle($br, $x, $top, $w, $h)
                    $br.Dispose()
                }
                $br = New-Object System.Drawing.SolidBrush $r.FG
                $g.DrawString($r.Text, $font, $br, $x, [single]$y, $fmt)
                $br.Dispose()
            }

            $g.ResetClip()
            $x += $w

            # No explicit inter-pill advance. The binary already emits a
            # separator space with the background reset, so it occupies exactly
            # one cell of terminal background between pills -- same as a real
            # terminal. Adding an extra advance here would double the gap.
        }
        $y += $lineH
    }
    $path = Join-Path $Out $File
    $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose(); $font.Dispose()
    "  wrote $path"
}

function J {
    param($effort='xhigh', $model='claude-opus-5', $tokens=406000, $cost=13.26,
          $dur=1466000, $add=121, $del=55, $h5=25, $d7=13, $dir='C:\Users\Pouri',
          $h5reset=15900, $d7reset=142200)
    $d = $dir -replace '\\', '\\'
    # Reset stamps are offsets from now, so an elevated window in the states
    # image renders its countdown instead of silently omitting it.
    $now = [DateTimeOffset]::Now.ToUnixTimeSeconds()
    $r5 = $now + $h5reset; $r7 = $now + $d7reset
    "{`"effort`":{`"level`":`"$effort`"},`"model`":{`"id`":`"$model`"},`"workspace`":{`"current_dir`":`"$d`"},`"cost`":{`"total_cost_usd`":$cost,`"total_duration_ms`":$dur,`"total_lines_added`":$add,`"total_lines_removed`":$del},`"context_window`":{`"total_input_tokens`":$tokens,`"context_window_size`":200000},`"rate_limits`":{`"five_hour`":{`"used_percentage`":$h5,`"resets_at`":$r5},`"seven_day`":{`"used_percentage`":$d7,`"resets_at`":$r7}}}"
}

Write-Host 'Rendering screenshots...'

New-Shot -Lines @((Get-Line (J))) -File 'hero.png'

New-Shot -File 'states.png' -Lines @(
    (Get-Line (J -tokens 406000 -h5 25 -d7 13)),
    (Get-Line (J -tokens 620000 -h5 63 -d7 30)),
    (Get-Line (J -tokens 880000 -h5 91 -d7 84))
)

New-Shot -File 'effort.png' -Lines @(
    (Get-Line (J -effort 'low')),
    (Get-Line (J -effort 'medium')),
    (Get-Line (J -effort 'high')),
    (Get-Line (J -effort 'xhigh')),
    (Get-Line (J -effort 'max'))
)

New-Shot -File 'tiers.png' -Lines @(
    (Get-Line (J) @('--icons nerd --caps round')),
    (Get-Line (J) @('--icons nerd --caps arrow')),
    (Get-Line (J) @('--icons unicode --caps block')),
    (Get-Line (J) @('--icons ascii --caps none'))
)

Write-Host 'Done.'
