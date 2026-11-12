# Storavix Agent offline configuration and diagnostics. Compatible with Windows PowerShell 5.1.
[CmdletBinding()]
param(
  [switch]$Console,
  [string]$ServerUrl,
  [string]$RegistrationTokenFile
)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$usingGui = (-not $Console -and [string]::IsNullOrEmpty($ServerUrl) -and [string]::IsNullOrEmpty($RegistrationTokenFile))

function Show-StoravixDialog([string]$message,[bool]$isError) {
  Add-Type -AssemblyName System.Windows.Forms
  $icon = [System.Windows.Forms.MessageBoxIcon]::Information
  if ($isError) { $icon = [System.Windows.Forms.MessageBoxIcon]::Error }
  [System.Windows.Forms.MessageBox]::Show($message, 'Storavix Agent', 'OK', $icon) | Out-Null
}
function Write-SetupLog([string]$message) {
  $line = ('{0:u} {1}' -f (Get-Date), $message)
  Write-Host $line
  if ($script:logPath) {
    try { Add-Content -LiteralPath $script:logPath -Value $line -Encoding UTF8 -ErrorAction Stop } catch {}
  }
}

try {
  $admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
  if (-not $admin) {
    if (-not $usingGui) { throw 'Start an elevated PowerShell (Run as Administrator).' }
    $argumentList = '-NoProfile -ExecutionPolicy Bypass -File "' + $PSCommandPath + '"'
    $proc = Start-Process -FilePath 'powershell.exe' -ArgumentList $argumentList -Verb RunAs -Wait -PassThru
    if ($proc.ExitCode -ne 0) { throw ('Elevated configuration failed. Exit code ' + $proc.ExitCode + '. See C:\ProgramData\StoravixAgent\setup.log.') }
    exit 0
  }

  $installDir = $PSScriptRoot
  $windowsVersion = [Environment]::OSVersion.Version
  $legacyOS = $windowsVersion.Major -lt 10
  $binaryName = 'storavix-agent.exe'
  if ($legacyOS) { $binaryName = 'storavix-agent-legacy.exe' }
  $binary = Join-Path $installDir $binaryName
  if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) {
    throw ('Compatible agent executable not found: '+$binary+'. Old Windows Server versions require the Storavix compatibility build.')
  }
  $dataDir = Join-Path $env:ProgramData 'StoravixAgent'
  New-Item -ItemType Directory -Path $dataDir -Force | Out-Null
  & icacls.exe $dataDir /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
  if ($LASTEXITCODE -ne 0) { throw ('Cannot set folder ACL: ' + $dataDir) }
  $script:logPath = Join-Path $dataDir 'setup.log'
  Write-SetupLog 'Configuration started.'
  Write-SetupLog ('Operating system version: '+$windowsVersion+'; selected binary: '+$binaryName)
  if ($legacyOS) { Write-SetupLog 'Legacy compatibility mode: compiled with unsupported Go 1.20 runtime. Restrict agent network access and upgrade the operating system when possible.' }
  $serverPath = Join-Path $dataDir 'server.txt'
  $tokenPath = Join-Path $dataDir 'token'
  $agentLogPath = Join-Path $dataDir 'agent.log'
  $url = ''
  if (Test-Path -LiteralPath $serverPath) { $url = (Get-Content -LiteralPath $serverPath -Raw).Trim() }
  $hasToken = Test-Path -LiteralPath $tokenPath
  $token = ''

  if ($usingGui) {
    Add-Type -AssemblyName System.Windows.Forms
    Add-Type -AssemblyName System.Drawing
    [System.Windows.Forms.Application]::EnableVisualStyles()
    $form = New-Object System.Windows.Forms.Form
    $form.Text='Storavix Agent - Initial configuration'
    $form.ClientSize = New-Object System.Drawing.Size(570,270)
    $form.StartPosition='CenterScreen'
    $form.FormBorderStyle='FixedDialog'
    $form.MaximizeBox=$false; $form.MinimizeBox=$false
    $intro=New-Object System.Windows.Forms.Label
    $intro.Location=New-Object System.Drawing.Point(20,18)
    $intro.Size=New-Object System.Drawing.Size(530,44)
    $intro.Text='Enter your Storavix monitoring server and agent registration token. After saving, Windows will create and start the StoravixAgent scheduled task.'
    $form.Controls.Add($intro)
    $label=New-Object System.Windows.Forms.Label
    $label.Location=New-Object System.Drawing.Point(20,72)
    $label.AutoSize=$true; $label.Text='Server URL (e.g. http://192.168.111.130:8080)'
    $form.Controls.Add($label)
    $urlBox=New-Object System.Windows.Forms.TextBox
    $urlBox.Location=New-Object System.Drawing.Point(20,94)
    $urlBox.Width=525; $urlBox.Text=$url
    $form.Controls.Add($urlBox)
    $tokenLabel=New-Object System.Windows.Forms.Label
    $tokenLabel.Location=New-Object System.Drawing.Point(20,133)
    $tokenLabel.AutoSize=$true; $tokenLabel.Text='Agent token (leave blank to keep previous token)'
    $form.Controls.Add($tokenLabel)
    $tokenBox=New-Object System.Windows.Forms.TextBox
    $tokenBox.Location=New-Object System.Drawing.Point(20,154)
    $tokenBox.Width=525; $tokenBox.UseSystemPasswordChar=$true
    $form.Controls.Add($tokenBox)
    $info=New-Object System.Windows.Forms.Label
    $info.Location=New-Object System.Drawing.Point(20,187)
    $info.Size=New-Object System.Drawing.Size(530,32)
    $info.Text='No Internet or GitHub access is required. Diagnostics: C:\ProgramData\StoravixAgent\setup.log'
    $form.Controls.Add($info)
    $ok=New-Object System.Windows.Forms.Button
    $ok.Text='Save and start';$ok.Size=New-Object System.Drawing.Size(130,30)
    $ok.Location=New-Object System.Drawing.Point(324,225)
    $ok.DialogResult=[System.Windows.Forms.DialogResult]::OK
    $form.Controls.Add($ok);$form.AcceptButton=$ok
    $cancel=New-Object System.Windows.Forms.Button
    $cancel.Text='Cancel';$cancel.Size=New-Object System.Drawing.Size(90,30)
    $cancel.Location=New-Object System.Drawing.Point(460,225)
    $cancel.DialogResult=[System.Windows.Forms.DialogResult]::Cancel
    $form.Controls.Add($cancel);$form.CancelButton=$cancel
    $dialogResult=$form.ShowDialog()
    if ($dialogResult -ne [System.Windows.Forms.DialogResult]::OK) {
      $form.Dispose(); Write-SetupLog 'Setup cancelled by user.'; exit 2
    }
    $url=$urlBox.Text.Trim()
    $token=$tokenBox.Text.Trim()
    $form.Dispose()
  } elseif (-not [string]::IsNullOrEmpty($ServerUrl)) {
    $url=$ServerUrl.Trim()
    if (-not [string]::IsNullOrEmpty($RegistrationTokenFile)) {
      if (-not (Test-Path -LiteralPath $RegistrationTokenFile -PathType Leaf)) { throw 'Registration token file not found.' }
      $token=(Get-Content -LiteralPath $RegistrationTokenFile -Raw).Trim()
    }
  } else {
    $answer=Read-Host ('Central Storavix URL [' + $url + ']')
    if ($answer.Trim()) { $url=$answer.Trim() }
    $secure=Read-Host 'Agent token (leave blank to keep previous)' -AsSecureString
    $ptr=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try { $token=[Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr) }
  }

  $parsed=$null
  if (-not [uri]::TryCreate($url,[System.UriKind]::Absolute,[ref]$parsed) -or $parsed.Scheme -notin @('http','https') -or $parsed.UserInfo -or $parsed.Query -or $parsed.Fragment -or $parsed.AbsolutePath -ne '/') {
    throw 'Invalid URL: enter http(s)://hostname[:port] without an extra path, query or credentials.'
  }
  # System.Uri.AbsolutePath is "/" for both an origin and origin + trailing slash.
  # Always persist the origin without the trailing slash, so upgrades also heal old config.
  $url = $parsed.GetLeftPart([System.UriPartial]::Authority).TrimEnd('/')
  Write-SetupLog ('Normalized server URL: '+$url)
  if ($token -and $token -notmatch '^[a-fA-F0-9]{64}$') { throw 'Registration token must contain exactly 64 hexadecimal characters.' }
  if (-not $token -and -not $hasToken) { throw 'Agent token is required for the first installation.' }
  if ($parsed.Scheme -eq 'http') {
    Write-SetupLog 'HTTP is unencrypted. This is a security warning, not a setup failure.'
    if ($usingGui) {
      if ([System.Windows.Forms.MessageBox]::Show('This is a security warning, not a setup error. HTTP sends the token unencrypted. On an isolated trusted LAN click Yes to continue; otherwise use HTTPS.','Storavix Agent','YesNo','Warning') -ne [System.Windows.Forms.DialogResult]::Yes) { Write-SetupLog 'User declined HTTP connection.'; exit 2 }
    }
  }

  if ($parsed.Scheme -eq 'http') { Write-SetupLog 'HTTP warning acknowledged; continuing setup.' }
  if ($token) {
    $tmpToken=Join-Path $dataDir ('token.new.'+[guid]::NewGuid().ToString('N'))
    Set-Content -LiteralPath $tmpToken -Value $token -Encoding Ascii -NoNewline
    Move-Item -LiteralPath $tmpToken -Destination $tokenPath -Force
    $token='' # Do not retain secret in memory longer than necessary.
  }
  Set-Content -LiteralPath $serverPath -Value $url -Encoding Ascii -NoNewline
  # Files created by older installers may retain restrictive ACLs that block LocalSystem.
  # Apply explicit SYSTEM + Administrators permissions to configuration files.
  foreach ($configPath in @($tokenPath, $serverPath)) {
    if (Test-Path -LiteralPath $configPath -PathType Leaf) {
      & icacls.exe $configPath /inheritance:r /grant:r '*S-1-5-18:F' '*S-1-5-32-544:F' | Out-Null
      if ($LASTEXITCODE -ne 0) { throw ('Could not grant SYSTEM access to '+$configPath) }
    }
  }
  $flags=''; if ($parsed.Scheme -eq 'http') { $flags=' --allow-http' }
  $args='--server "'+$url+'" --token-file "'+$tokenPath+'" --data-dir "'+$dataDir+'" --log-file "'+$agentLogPath+'" --interval 30s'+$flags

  Write-SetupLog 'Creating StoravixAgent scheduled task under LocalSystem.'
  $action=New-ScheduledTaskAction -Execute $binary -Argument $args -WorkingDirectory $installDir
  $trigger=New-ScheduledTaskTrigger -AtStartup
  $settings=New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -StartWhenAvailable
  Stop-ScheduledTask -TaskName StoravixAgent -ErrorAction SilentlyContinue
  Register-ScheduledTask -TaskName StoravixAgent -Action $action -Trigger $trigger -Settings $settings -User 'SYSTEM' -RunLevel Highest -Force -ErrorAction Stop | Out-Null
  $task=Get-ScheduledTask -TaskName StoravixAgent -ErrorAction Stop
  Write-SetupLog ('Task registered: '+$task.TaskName)
  Start-ScheduledTask -TaskName StoravixAgent -ErrorAction Stop
  $state=''
  for ($i=0; $i -lt 12; $i++) {
    Start-Sleep -Seconds 1
    $state=(Get-ScheduledTask -TaskName StoravixAgent -ErrorAction Stop).State.ToString()
    if ($state -eq 'Running') { break }
  }
  $result=(Get-ScheduledTaskInfo -TaskName StoravixAgent -ErrorAction Stop).LastTaskResult
  Write-SetupLog ('Task state='+$state+' LastTaskResult='+$result)
  if ($state -ne 'Running') {
    $details = ''
    if (Test-Path -LiteralPath $agentLogPath -PathType Leaf) {
      $recent = @(Get-Content -LiteralPath $agentLogPath -Tail 12 -ErrorAction SilentlyContinue)
      $details = ($recent -join [Environment]::NewLine)
      Write-SetupLog ('Recent agent log: '+($recent -join ' | '))
    } else {
      $details = 'Agent log file was not created. Check Windows Defender, AppLocker, Code Integrity and executable permissions.'
      Write-SetupLog $details
    }
    throw ('Agent process exited. Task state='+$state+' LastTaskResult='+$result+[Environment]::NewLine+'Agent diagnostics: '+$agentLogPath+[Environment]::NewLine+$details)
  }
  Write-SetupLog ('SUCCESS. Server='+$url+' AgentLog='+$agentLogPath)
  $success = 'Storavix Agent is running.'+[Environment]::NewLine+'Scheduled task: StoravixAgent'+[Environment]::NewLine+'Server: '+$url+[Environment]::NewLine+'Diagnostics: '+$script:logPath
  if ($usingGui) { Show-StoravixDialog $success $false }
  else { Write-Host $success -ForegroundColor Green }
} catch {
  $message=$_.Exception.Message
  if ($script:logPath) { Write-SetupLog ('ERROR: '+$message) }
  [Console]::Error.WriteLine('Storavix Agent setup ERROR: '+$message)
  [Console]::Error.WriteLine('Diagnostics: C:\ProgramData\StoravixAgent\setup.log')
  if ($usingGui) { Show-StoravixDialog ('Setup failed: '+$message+[Environment]::NewLine+[Environment]::NewLine+'Log: C:\ProgramData\StoravixAgent\setup.log') $true }
  exit 1
}
