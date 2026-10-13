# Run in elevated Windows PowerShell:
# Set-ExecutionPolicy -Scope Process Bypass -Force
# .\scripts\install-windows.ps1
$ErrorActionPreference='Stop'
$admin=([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin) { throw 'Run PowerShell as Administrator' }
$root=(Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$install=Join-Path $env:ProgramFiles 'Storavix'
$data=Join-Path $env:ProgramData 'Storavix'
$bin=Join-Path $install 'storavix.exe'
if ($env:STORAVIX_BINARY -and (Test-Path $env:STORAVIX_BINARY)) {$source=$env:STORAVIX_BINARY}
elseif (Get-Command go -ErrorAction SilentlyContinue) {
  Push-Location $root
  try {& go build -o '.\storavix.exe' '.\cmd\storavix'; if ($LASTEXITCODE -ne 0) {throw 'Go build failed'}}
  finally {Pop-Location}
  $source=Join-Path $root 'storavix.exe'
} elseif ($env:STORAVIX_BINARY -and (Test-Path $env:STORAVIX_BINARY)) {
  $source=$env:STORAVIX_BINARY
} else { $source=Join-Path $root 'storavix.exe'; if (-not (Test-Path $source)) {throw 'Go 1.23+ or compiled storavix.exe is required'} }
New-Item -ItemType Directory -Force -Path $install,$data | Out-Null
& icacls.exe $data /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Cannot secure state directory' }
$t=Get-ScheduledTask -TaskName Storavix -ErrorAction SilentlyContinue
if ($t) {Stop-ScheduledTask -TaskName Storavix -ErrorAction SilentlyContinue}
Start-Sleep -Milliseconds 750
Copy-Item $source $bin -Force
if (-not (Test-Path (Join-Path $data 'state.json'))) {
  do {
    $user=Read-Host 'Логин администратора [admin]'
    if ([string]::IsNullOrWhiteSpace($user)) {$user='admin'}
  } while ($user -notmatch '^[A-Za-z0-9._-]{3,64}$')
  $p1=Read-Host 'Пароль (12+ символов)' -AsSecureString
  $p2=Read-Host 'Повторите пароль' -AsSecureString
  $b1=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($p1)
  $b2=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($p2)
  try {
    $pass=[Runtime.InteropServices.Marshal]::PtrToStringBSTR($b1)
    $check=[Runtime.InteropServices.Marshal]::PtrToStringBSTR($b2)
    if ($pass.Length -lt 12 -or $pass -ne $check) {throw 'Password too short or confirmation differs'}
    $old=$OutputEncoding
    try {
      $OutputEncoding=New-Object System.Text.UTF8Encoding($false)
      $pass | & $bin init --data-dir $data --admin-user $user --listen 0.0.0.0:8080 --password-stdin
      if ($LASTEXITCODE -ne 0) {throw 'Cannot initialize account'}
    } finally {$OutputEncoding=$old}
  } finally {
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($b1)
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($b2)
    Remove-Variable pass,check -ErrorAction SilentlyContinue
  }
} else {
  & $bin configure --data-dir $data --listen 0.0.0.0:8080
  if ($LASTEXITCODE -ne 0) {throw 'Cannot update listening address'}
}
$log=Join-Path $data 'storavix.log'
$q=[char]34
$arguments='--data-dir '+$q+$data+$q+' --log-file '+$q+$log+$q
$action=New-ScheduledTaskAction -Execute $bin -Argument $arguments -WorkingDirectory $install
$trigger=New-ScheduledTaskTrigger -AtStartup
$settings=New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Seconds 0) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -StartWhenAvailable
Register-ScheduledTask -TaskName Storavix -Action $action -Trigger $trigger -Settings $settings -User SYSTEM -RunLevel Highest -Force | Out-Null
if (-not (Get-NetFirewallRule -DisplayName 'Storavix HTTP LAN' -ErrorAction SilentlyContinue)) {
  New-NetFirewallRule -DisplayName 'Storavix HTTP LAN' -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow -RemoteAddress LocalSubnet -Profile Domain,Private | Out-Null
}
Start-ScheduledTask Storavix
Start-Sleep -Seconds 2
try {Invoke-RestMethod http://127.0.0.1:8080/api/health -TimeoutSec 5 | Format-List}
catch {Write-Warning ('Check log: '+$log)}
Get-NetIPAddress -AddressFamily IPv4 | Where-Object {$_.IPAddress -ne '127.0.0.1' -and $_.IPAddress -notlike '169.254.*' -and $_.AddressState -eq 'Preferred'} | ForEach-Object {Write-Host ('http://'+$_.IPAddress+':8080')}
Write-Warning 'HTTP sends credentials without encryption. Restrict to trusted LAN; add HTTPS proxy for production.'