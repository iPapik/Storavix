# Installs / upgrades agent as SYSTEM task; keeps token and persistent agent ID.
$ErrorActionPreference='Stop'
Write-Host '[Storavix Agent] Starting installation...' -ForegroundColor Cyan
$admin=([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if(!$admin){throw 'Run PowerShell as Administrator.'}
$root=(Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$dir=Join-Path $env:ProgramFiles 'Storavix Agent'
$data=Join-Path $env:ProgramData 'StoravixAgent'
$binary=Join-Path $dir 'storavix-agent.exe'
$source=$null
if($env:STORAVIX_AGENT_BINARY -and (Test-Path $env:STORAVIX_AGENT_BINARY)){$source=$env:STORAVIX_AGENT_BINARY}
elseif(Get-Command go -ErrorAction SilentlyContinue){Write-Host '[1/6] Building agent...' -ForegroundColor Cyan;Push-Location $root;try{& go build -trimpath -o '.\storavix-agent.exe' '.\cmd\storavix-agent';if($LASTEXITCODE -ne 0){throw 'Go build failed'}}finally{Pop-Location};$source=Join-Path $root 'storavix-agent.exe'}
elseif(Test-Path (Join-Path $root 'storavix-agent.exe')){$source=Join-Path $root 'storavix-agent.exe'}
else{throw 'Agent binary not found (EXE or Go 1.23+ required).'}
if ($env:STORAVIX_RAID_TOOL -or $env:STORAVIX_RAID_TOOL_SOURCE -or $env:STORAVIX_RAID_TOOL_SHA256) {
    try {
        & $source "--install-raid-tool=$env:STORAVIX_RAID_TOOL" "--raid-tool-source=$env:STORAVIX_RAID_TOOL_SOURCE" "--raid-tool-sha256=$env:STORAVIX_RAID_TOOL_SHA256" --raid-tool-if-detected
        if ($LASTEXITCODE -ne 0) { throw 'Agent could not provision the RAID CLI.' }
    } catch { Write-Warning "RAID CLI installation failed. Agent setup will continue. $_" }
}
Write-Host '[2/6] Preparing secured directories...' -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path $dir,$data | Out-Null
& icacls.exe $data /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if($LASTEXITCODE -ne 0){throw 'Failed to set token file ACL'}
$serverFile=Join-Path $data 'server.txt';$tokenFile=Join-Path $data 'token'
$url = ''
if (Test-Path -LiteralPath $serverFile -PathType Leaf) {
    $url = [IO.File]::ReadAllText($serverFile).Trim()
}
if (-not $url) {
    $url = (Read-Host 'Storavix server URL (https://host or http://IP:8080)').Trim()
}
$uri = $null
if (-not [uri]::TryCreate($url, [System.UriKind]::Absolute, [ref]$uri) -or
    $uri.Scheme -notin @('http','https') -or -not $uri.Host -or
    $uri.UserInfo -or $uri.Query -or $uri.Fragment -or $uri.AbsolutePath -ne '/') { throw 'Invalid server URL' }
$origin = $uri.GetLeftPart([System.UriPartial]::Authority)
if ((Test-Path -LiteralPath $tokenFile -PathType Leaf) -and
    ([IO.File]::ReadAllText($tokenFile).Trim() -match '^[0-9a-fA-F]{64}$')) {
    Write-Host '[3/6] Using saved registration token.' -ForegroundColor Cyan
} else {
    $sec = Read-Host 'Agent token (from storavix agent-token)' -AsSecureString
    $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec)
    try {
        $token = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr)
        if ($token -notmatch '^[0-9a-fA-F]{64}$') { throw 'Invalid token' }
        [IO.File]::WriteAllText($tokenFile, $token, [Text.Encoding]::ASCII)
    } finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr)
        Remove-Variable token -ErrorAction SilentlyContinue
    }
}
if (-not (Test-Path -LiteralPath $serverFile -PathType Leaf) -or $origin -ne $url) {
    [IO.File]::WriteAllText($serverFile, $origin, [Text.Encoding]::ASCII)
}
$url = $origin
Write-Host '[4/6] Registering SYSTEM scheduled task...' -ForegroundColor Cyan
Stop-ScheduledTask -TaskName StoravixAgent -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2
$backup = Join-Path $dir 'storavix-agent.exe.previous'
$hadBinary = Test-Path -LiteralPath $binary
if ($hadBinary) { Copy-Item -LiteralPath $binary -Destination $backup -Force }
try { Copy-Item -LiteralPath $source -Destination $binary -Force }
catch {
    if ($hadBinary) { Start-ScheduledTask -TaskName StoravixAgent -ErrorAction SilentlyContinue }
    throw
}
$extra='';if($url -match '^http://'){$extra=' --allow-http';Write-Warning 'HTTP does not encrypt the token. Use HTTPS in production.'}
$args='--server "'+$url+'" --token-file "'+$tokenFile+'" --data-dir "'+$data+'" --interval 30s'+$extra
$action=New-ScheduledTaskAction -Execute $binary -Argument $args -WorkingDirectory $dir
$trigger=New-ScheduledTaskTrigger -AtStartup
$settings=New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Seconds 0) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -StartWhenAvailable
Register-ScheduledTask -TaskName StoravixAgent -Action $action -Trigger $trigger -Settings $settings -User SYSTEM -RunLevel Highest -Force | Out-Null
Write-Host '[5/6] Starting agent...' -ForegroundColor Cyan
try {
    Start-ScheduledTask -TaskName StoravixAgent -ErrorAction Stop
    Start-Sleep -Seconds 3
    $state = (Get-ScheduledTask -TaskName StoravixAgent).State
    if ($state -ne 'Running') { throw "Agent task not running (state: $state)" }
} catch {
    if ($hadBinary -and (Test-Path $backup)) {
        Stop-ScheduledTask -TaskName StoravixAgent -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 2
        Copy-Item -LiteralPath $backup -Destination $binary -Force
        Start-ScheduledTask -TaskName StoravixAgent -ErrorAction SilentlyContinue
    }
    throw
}
Write-Host '[6/6] Task status:' -ForegroundColor Green
Get-ScheduledTask -TaskName StoravixAgent | Select-Object TaskName,State
Write-Host "Agent data directory: $data"

# Permanent one-command updater, no local repository required.
$updateScript = Join-Path $dir 'storavix-agent-update.ps1'
$updateCmd = Join-Path $dir 'storavix-agent-update.cmd'
$updateContent = @'
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin) { throw 'Run PowerShell as Administrator.' }
$url = 'https://raw.githubusercontent.com/iPapik/Storavix/main/scripts/install-agent-online-windows.ps1'
$temp = Join-Path $env:TEMP ('storavix-agent-update-' + [guid]::NewGuid().ToString('N') + '.ps1')
try {
  Invoke-WebRequest -Uri $url -OutFile $temp -UseBasicParsing
  & $temp
  if (-not $?) { throw 'Storavix agent update failed.' }
} finally {
  Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue
}
'@
Set-Content -LiteralPath $updateScript -Value $updateContent -Encoding Ascii
$updateCommand = @'
@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0storavix-agent-update.ps1" %*
exit /b %ERRORLEVEL%
'@
Set-Content -LiteralPath $updateCmd -Value $updateCommand -Encoding Ascii
$machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
if (($machinePath -split ';') -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', ($machinePath.TrimEnd(';') + ';' + $dir), 'Machine')
}
Write-Host ('Future updates: storavix-agent-update (new terminal) or: & "' + $updateCmd + '"') -ForegroundColor Green
