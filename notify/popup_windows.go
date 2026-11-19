//go:build windows

package notify

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

const popupScript = `
$ErrorActionPreference = 'SilentlyContinue'
Add-Type -AssemblyName System.Windows.Forms, System.Drawing
Add-Type -ReferencedAssemblies System.Windows.Forms, System.Drawing -TypeDefinition @"
using System.Windows.Forms;
public class BBPopup : Form {
  protected override bool ShowWithoutActivation { get { return true; } }
  protected override CreateParams CreateParams {
    get { CreateParams cp = base.CreateParams; cp.ExStyle |= 0x08000000 | 0x00000080 | 0x00000008; return cp; }
  }
}
"@

$port = $env:BB_PORT
$base = "http://127.0.0.1:$port"
$wc = New-Object Net.WebClient
$wc.Encoding = [Text.Encoding]::UTF8

function C($r, $g, $b) { [Drawing.Color]::FromArgb($r, $g, $b) }

$f = New-Object BBPopup
$f.FormBorderStyle = 'None'
$f.StartPosition = 'Manual'
$f.ShowInTaskbar = $false
$f.TopMost = $true
$f.BackColor = C 15 22 41
$f.Size = New-Object Drawing.Size(410, 138)
$wa = [Windows.Forms.Screen]::PrimaryScreen.WorkingArea
$f.Location = New-Object Drawing.Point(($wa.Right - 426), ($wa.Bottom - 154))

$bar = New-Object Windows.Forms.Panel
$bar.BackColor = C 52 211 153
$bar.Location = New-Object Drawing.Point(0, 0)
$bar.Size = New-Object Drawing.Size(6, 138)
$f.Controls.Add($bar)

$title = New-Object Windows.Forms.Label
$title.Text = 'BEL SEKOLAH  •  SEDANG BERBUNYI'
$title.Font = New-Object Drawing.Font('Segoe UI', 9, [Drawing.FontStyle]::Bold)
$title.ForeColor = C 52 211 153
$title.Location = New-Object Drawing.Point(22, 12)
$title.AutoSize = $true
$f.Controls.Add($title)

$name = New-Object Windows.Forms.Label
$name.Text = 'Memuat...'
$name.Font = New-Object Drawing.Font('Segoe UI', 13, [Drawing.FontStyle]::Bold)
$name.ForeColor = [Drawing.Color]::White
$name.Location = New-Object Drawing.Point(22, 34)
$name.Size = New-Object Drawing.Size(376, 30)
$name.AutoEllipsis = $true
$f.Controls.Add($name)

$info = New-Object Windows.Forms.Label
$info.Text = '00:00  •  Speaker server'
$info.Font = New-Object Drawing.Font('Segoe UI', 9)
$info.ForeColor = C 138 150 184
$info.Location = New-Object Drawing.Point(22, 66)
$info.AutoSize = $true
$f.Controls.Add($info)

function Btn($text, $x, $w, $color) {
  $b = New-Object Windows.Forms.Button
  $b.Text = $text
  $b.Location = New-Object Drawing.Point($x, 94)
  $b.Size = New-Object Drawing.Size($w, 32)
  $b.FlatStyle = 'Flat'
  $b.FlatAppearance.BorderSize = 0
  $b.BackColor = $color
  $b.ForeColor = [Drawing.Color]::White
  $b.Font = New-Object Drawing.Font('Segoe UI', 9, [Drawing.FontStyle]::Bold)
  $b.Cursor = [Windows.Forms.Cursors]::Hand
  $f.Controls.Add($b)
  return $b
}
$stop = Btn 'Hentikan Audio' 22 170 (C 220 38 38)
$dash = Btn 'Dashboard' 200 110 (C 30 58 138)
$close = Btn 'Tutup' 318 80 (C 31 42 72)

$stop.Add_Click({
  $stop.Enabled = $false
  $stop.Text = 'Menghentikan...'
  try { [void]$wc.UploadString("$base/api/audio/stop-server", 'POST', '') } catch {}
  $f.Close()
})
$dash.Add_Click({ Start-Process "http://localhost:$port" })
$close.Add_Click({ $f.Close() })

$script:idle = 0
$script:last = ''
$script:start = Get-Date
$timer = New-Object Windows.Forms.Timer
$timer.Interval = 1000
$timer.Add_Tick({
  try {
    $s = ($wc.DownloadString("$base/api/audio/now") | ConvertFrom-Json).data
  } catch { $s = $null }
  if ($s -and $s.playing) {
    $script:idle = 0
    if ($s.name -ne $script:last) { $script:last = $s.name; $script:start = Get-Date; $name.Text = $s.name }
    $el = (Get-Date) - $script:start
    $info.Text = ('{0:mm\:ss}  •  Speaker server' -f $el)
  } else {
    $script:idle++
    if ($script:idle -ge 3) { $f.Close() }
  }
})
$timer.Start()
[Windows.Forms.Application]::Run($f)
`

func runPopup(port int) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", popupScript)
	cmd.Env = append(os.Environ(), fmt.Sprintf("BB_PORT=%d", port))
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd.Run()
}
