# Storavix Agent configuration wizard - PowerShell 5.1 compatible, offline.
param([switch]$Console)
$ErrorActionPreference = 'Stop'
$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin) {
  $args = '-NoProfile -ExecutionPolicy Bypass -File "' + $PSCommandPath + '"'
  if ($Console) { $args += ' -Console' }
  $proc = Start-Process -FilePath 'powershell.exe' -ArgumentList $args -Verb RunAs -Wait -PassThru
  exit $proc.ExitCode
}
$installDir = $PSScriptRoot
$binary = Join-Path $installDir 'storavix-agent.exe'
if (-not (Test-Path -LiteralPath $binary)) { throw 'Storavix agent executable was not found.' }
$dataDir = Join-Path $env:ProgramData 'StoravixAgent'
$serverPath = Join-Path $dataDir 'server.txt'
$tokenPath = Join-Path $dataDir 'token'
$oldUrl = ''
if (Test-Path -LiteralPath $serverPath) { $oldUrl = (Get-Content -LiteralPath $serverPath -Raw).Trim() }
$hasToken = Test-Path -LiteralPath $tokenPath
$url = $oldUrl
$token = ''

if ($Console) {
  $entered = Read-Host ('Central Storavix URL [' + $oldUrl + ']')
  if ($entered.Trim()) { $url = $entered.Trim() }
  $sec = Read-Host 'Registration token (leave empty to keep existing)' -AsSecureString
  $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec)
  try { $token = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr) }
  finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr) }
} else {
  Add-Type -AssemblyName System.Windows.Forms
  Add-Type -AssemblyName System.Drawing
  [System.Windows.Forms.Application]::EnableVisualStyles()
  $form = New-Object System.Windows.Forms.Form
  $form.Text = 'Storavix Agent - Connect to server'
  $form.Width = 545; $form.Height = 300
  $form.StartPosition = 'CenterScreen'; $form.FormBorderStyle = 'FixedDialog'
  $form.MaximizeBox = $false; $form.MinimizeBox = $false
  $intro = New-Object System.Windows.Forms.Label
  $intro.Location = New-Object System.Drawing.Point(20,15)
  $intro.Size = New-Object System.Drawing.Size(485,42)
  $intro.Text = 'Enter the central Storavix address and registration token. This runs locally; GitHub is not required.'
  $form.Controls.Add($intro)
  $lblUrl = New-Object System.Windows.Forms.Label
  $lblUrl.Location = New-Object System.Drawing.Point(20,68); $lblUrl.Text = 'Server URL (https://server or http://IP:8080)'; $lblUrl.AutoSize=$true
  $form.Controls.Add($lblUrl)
  $urlBox = New-Object System.Windows.Forms.TextBox
  $urlBox.Location = New-Object System.Drawing.Point(20,90); $urlBox.Width=480; $urlBox.Text=$oldUrl
  $form.Controls.Add($urlBox)
  $lblToken=New-Object System.Windows.Forms.Label
  $lblToken.Location = New-Object System.Drawing.Point(20,126); $lblToken.AutoSize=$true
  $lblToken.Text = 'Registration token (leave blank to keep the existing one)'
  $form.Controls.Add($lblToken)
  $tokenBox=New-Object System.Windows.Forms.TextBox
  $tokenBox.Location = New-Object System.Drawing.Point(20,148); $tokenBox.Width=480; $tokenBox.UseSystemPasswordChar=$true
  $form.Controls.Add($tokenBox)
  $hint = New-Object System.Windows.Forms.Label
  $hint.Location = New-Object System.Drawing.Point(20,181); $hint.Size=New-Object System.Drawing.Size(480,30)
  $hint.Text = 'The token is stored under ProgramData with SYSTEM/Admin-only ACLs.'
  $form.Controls.Add($hint)
  $ok=New-Object System.Windows.Forms.Button
  $ok.Text='Save and start'; $ok.Location=New-Object System.Drawing.Point(278,216)
  $ok.Width=130; $ok.DialogResult='OK'; $form.AcceptButton=$ok; $form.Controls.Add($ok)
  $cancel=New-Object System.Windows.Forms.Button
  $cancel.Text='Cancel';$cancel.Location=New-Object System.Drawing.Point(419,216)
  $cancel.Width=82;$cancel.DialogResult='Cancel';$form.CancelButton=$cancel;$form.Controls.Add($cancel)
  if ($form.ShowDialog() -ne [System.Windows.Forms.DialogResult]::OK) { exit 1 }
  $url=$urlBox.Text.Trim();$token=$tokenBox.Text.Trim()
  $form.Dispose()
}
$parsed = $null
if (-not [uri]::TryCreate($url,[System.UriKind]::Absolute,[ref]$parsed) -or $parsed.Scheme -notin @('https','http') -or $parsed.UserInfo -or $parsed.Query -or $parsed.Fragment -or $parsed.AbsolutePath -ne '/') { throw 'Enter a valid base http(s) URL without a path, query or credentials.' }
if ($token -and $token -notmatch '^[a-fA-F0-9]{64}$') { throw 'Registration token must be exactly 64 hexadecimal characters.' }
if (-not $token -and -not $hasToken) { throw 'Registration token is required for first-time setup.' }
if ($url -match '^http://') {
  Write-Warning 'HTTP does not encrypt the agent token. Prefer HTTPS.'
  if (-not $Console) {
    if ([System.Windows.Forms.MessageBox]::Show('HTTP sends the agent token without encryption. Continue only on an isolated trusted network.','Storavix Agent',[System.Windows.Forms.MessageBoxButtons]::YesNo,[System.Windows.Forms.MessageBoxIcon]::Warning) -ne [System.Windows.Forms.DialogResult]::Yes) { exit 1 }
  }
}
New-Item -ItemType Directory -Path $dataDir -Force | Out-Null
& icacls.exe $dataDir /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to secure the data directory.' }
if ($token) { Set-Content -LiteralPath $tokenPath -Value $token -Encoding Ascii -NoNewline }
Set-Content -LiteralPath $serverPath -Value $url -Encoding Ascii -NoNewline
$extra = ''; if ($parsed.Scheme -eq 'http') {$extra=' --allow-http'}
$args = '--server "' + $url + '" --token-file "' + $tokenPath + '" --data-dir "' + $dataDir + '" --interval 30s' + $extra
$action=New-ScheduledTaskAction -Execute $binary -Argument $args -WorkingDirectory $installDir
$trigger=New-ScheduledTaskTrigger -AtStartup
$settings=New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Seconds 0) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -StartWhenAvailable
Stop-ScheduledTask -TaskName StoravixAgent -ErrorAction SilentlyContinue
Register-ScheduledTask -TaskName StoravixAgent -Action $action -Trigger $trigger -Settings $settings -User SYSTEM -RunLevel Highest -Force | Out-Null
Start-ScheduledTask -TaskName StoravixAgent -ErrorAction Stop
Start-Sleep -Seconds 2
$state=(Get-ScheduledTask -TaskName StoravixAgent).State
if ($state -ne 'Running') { throw "Agent task was created but is not running: $state" }
if ($Console) { Write-Host "Storavix Agent configured and running. Server: $url" -ForegroundColor Green }
else { [System.Windows.Forms.MessageBox]::Show('Storavix Agent is running. Check the central Storavix dashboard for the new system.','Storavix Agent') | Out-Null }
