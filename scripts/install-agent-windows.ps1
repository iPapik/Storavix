# Installs / upgrades agent as SYSTEM task; keeps token and persistent agent ID.
$ErrorActionPreference='Stop'
Write-Host '[Storavix Agent] Начало установки...' -ForegroundColor Cyan
$admin=([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if(!$admin){throw 'Запустите PowerShell от администратора.'}
$root=(Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$dir=Join-Path $env:ProgramFiles 'Storavix Agent'
$data=Join-Path $env:ProgramData 'StoravixAgent'
$binary=Join-Path $dir 'storavix-agent.exe'
$source=$null
if($env:STORAVIX_AGENT_BINARY -and (Test-Path $env:STORAVIX_AGENT_BINARY)){$source=$env:STORAVIX_AGENT_BINARY}
elseif(Get-Command go -ErrorAction SilentlyContinue){Write-Host '[1/6] Компиляция агента...' -ForegroundColor Cyan;Push-Location $root;try{& go build -trimpath -o '.\storavix-agent.exe' '.\cmd\storavix-agent';if($LASTEXITCODE -ne 0){throw 'Go build failed'}}finally{Pop-Location};$source=Join-Path $root 'storavix-agent.exe'}
elseif(Test-Path (Join-Path $root 'storavix-agent.exe')){$source=Join-Path $root 'storavix-agent.exe'}
else{throw 'Для агента нужен готовый EXE или Go 1.23+'}
Write-Host '[2/6] Создание защищённого каталога...' -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path $dir,$data | Out-Null
& icacls.exe $data /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if($LASTEXITCODE -ne 0){throw 'ACL для токена установить не удалось'}
$serverFile=Join-Path $data 'server.txt';$tokenFile=Join-Path $data 'token'
if((Test-Path $serverFile) -and (Test-Path $tokenFile)){Write-Host '[3/6] Старые настройки подключения сохранены.' -ForegroundColor Cyan;$url=(Get-Content $serverFile -Raw).Trim()}
else{
 Write-Host '[3/6] Ввод адреса сервера и токена...' -ForegroundColor Cyan
 $url=(Read-Host 'Адрес Storavix (https://host или http://IP:8080)').Trim()
 if($url -notmatch '^https?://[A-Za-z0-9._:-]+$'){throw 'Некорректный URL'}
 $sec=Read-Host 'Токен агента из команды agent-token' -AsSecureString
 $ptr=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec)
 try{$token=[Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr);if($token -notmatch '^[0-9a-f]{64}$'){throw 'Неверный токен'};Set-Content -Path $tokenFile -Value $token -NoNewline -Encoding ascii}
 finally{[Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr);Remove-Variable token -ErrorAction SilentlyContinue}
 Set-Content -Path $serverFile -Value $url -NoNewline -Encoding ascii
}
Write-Host '[4/6] Установка службы через Планировщик заданий...' -ForegroundColor Cyan
Stop-ScheduledTask -TaskName StoravixAgent -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2
Copy-Item $source $binary -Force
$extra='';if($url -match '^http://'){$extra=' --allow-http';Write-Warning 'HTTP не шифрует токен; используйте HTTPS на продуктивных серверах.'}
$args='--server "'+$url+'" --token-file "'+$tokenFile+'" --data-dir "'+$data+'" --interval 30s'+$extra
$action=New-ScheduledTaskAction -Execute $binary -Argument $args -WorkingDirectory $dir
$trigger=New-ScheduledTaskTrigger -AtStartup
$settings=New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Seconds 0) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -StartWhenAvailable
Register-ScheduledTask -TaskName StoravixAgent -Action $action -Trigger $trigger -Settings $settings -User SYSTEM -RunLevel Highest -Force | Out-Null
Write-Host '[5/6] Запуск агента...' -ForegroundColor Cyan
Start-ScheduledTask -TaskName StoravixAgent
Write-Host '[6/6] Задача и статус:' -ForegroundColor Green
Get-ScheduledTask -TaskName StoravixAgent | Select-Object TaskName,State
Write-Host "Состояние агента: $data"
