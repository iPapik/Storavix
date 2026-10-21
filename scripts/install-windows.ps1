# Storavix installer for Windows Server 2019+ and Windows 10/11 (PowerShell 5.1+).
# Open PowerShell as Administrator:
#   Set-ExecutionPolicy -Scope Process Bypass -Force
#   .\scripts\install-windows.ps1
$ErrorActionPreference = 'Stop'
Write-Host '[Storavix] Начало установки — проверка требований...' -ForegroundColor Cyan
$IsElevated = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $IsElevated) { throw 'Запустите PowerShell от имени администратора.' }
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$installDir = Join-Path $env:ProgramFiles 'Storavix'
$dataDir = Join-Path $env:ProgramData 'Storavix'
$bin = Join-Path $installDir 'storavix.exe'
$taskName = 'Storavix'
$listen = '0.0.0.0:8080'
Write-Host '[1/6] Проверка программы и подготовка бинарного файла...' -ForegroundColor Cyan

$source = $null
if ($env:STORAVIX_BINARY -and (Test-Path $env:STORAVIX_BINARY)) { $source = $env:STORAVIX_BINARY }
elseif (Get-Command go -ErrorAction SilentlyContinue) {
    $ver = (& go version) -join ''
    if ($ver -notmatch 'go1\.(\d+)') { throw "Не удалось определить версию Go: $ver" }
    if ([int]$Matches[1] -lt 23) { throw 'Для сборки требуется Go 1.23 или новее.' }
    Write-Host 'Сборка Storavix из актуальных исходников...'
    Push-Location $repo
    try { & go build -trimpath -o '.\storavix.exe' '.\cmd\storavix'; if ($LASTEXITCODE -ne 0) { throw 'Сборка Go завершилась ошибкой.' } }
    finally { Pop-Location }
    $source = Join-Path $repo 'storavix.exe'
}
elseif (Test-Path (Join-Path $repo 'storavix.exe')) { $source = Join-Path $repo 'storavix.exe' }
elseif (Test-Path (Join-Path $repo 'dist\storavix-windows-amd64.exe')) { $source = Join-Path $repo 'dist\storavix-windows-amd64.exe' }
else { throw 'Для установки нужен Go 1.23+ (https://go.dev/dl/) либо готовый EXE из GitHub Actions.' }
Write-Host '[2/6] Создание каталогов и настройка прав...' -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path $installDir,$dataDir | Out-Null
# Restrict locally stored password hash, SMTP secrets and encryption key to administrators/SYSTEM.
& icacls.exe $dataDir /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Не удалось защитить папку данных ACL.' }
Write-Host '[3/6] Остановка предыдущего экземпляра (если установлен)...' -ForegroundColor Cyan
$task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($task) { Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue }
Start-Sleep -Milliseconds 600
Write-Host '[4/6] Копирование исполняемого файла...' -ForegroundColor Cyan
Copy-Item -Path $source -Destination $bin -Force

Write-Host '[5/6] Инициализация администратора и сохранение настроек...' -ForegroundColor Cyan
if (-not (Test-Path (Join-Path $dataDir 'state.json'))) {
    do {
        $user = Read-Host 'Логин администратора [admin]'
        if ([string]::IsNullOrWhiteSpace($user)) { $user = 'admin' }
        if ($user -notmatch '^[A-Za-z0-9._-]{3,64}$') { Write-Host '3–64 символа: латиница, цифры, . _ -' -ForegroundColor Yellow }
    } while ($user -notmatch '^[A-Za-z0-9._-]{3,64}$')
    $p1 = Read-Host 'Пароль администратора (минимум 8 символов)' -AsSecureString
    $p2 = Read-Host 'Повторите пароль' -AsSecureString
    $ptr1=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($p1)
    $ptr2=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($p2)
    try {
        $plain1=[Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr1)
        $plain2=[Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr2)
        if ($plain1.Length -lt 8 -or $plain1 -ne $plain2) { throw 'Пароль короче 8 символов или пароли не совпадают. Запустите установщик ещё раз.' }
        $oldEncoding = $OutputEncoding
        try {
            $OutputEncoding = New-Object System.Text.UTF8Encoding($false)
            $plain1 | & $bin init --data-dir $dataDir --listen $listen --admin-user $user --password-stdin
            if ($LASTEXITCODE -ne 0) { throw 'Ошибка создания администратора.' }
        } finally { $OutputEncoding = $oldEncoding }
    } finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr1)
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr2)
        Remove-Variable plain1,plain2 -ErrorAction SilentlyContinue
    }
} else {
    Write-Host 'Найдена существующая установка. Учётная запись и данные сохраняются.'
    & $bin configure --data-dir $dataDir --listen $listen
    if ($LASTEXITCODE -ne 0) { throw 'Не удалось обновить сетевые настройки.' }
}

Write-Host '[6/6] Настройка автозапуска, запуск и проверка HTTP...' -ForegroundColor Cyan
$action=New-ScheduledTaskAction -Execute $bin -Argument "--data-dir `"$dataDir`" --log-file `"$(Join-Path $dataDir 'storavix.log')`"" -WorkingDirectory $installDir
$trigger=New-ScheduledTaskTrigger -AtStartup
$settings=New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Seconds 0) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -StartWhenAvailable
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Settings $settings -User 'SYSTEM' -RunLevel Highest -Force | Out-Null
$rule = Get-NetFirewallRule -DisplayName 'Storavix Web UI 8080 (LocalSubnet)' -ErrorAction SilentlyContinue
if (-not $rule) { New-NetFirewallRule -DisplayName 'Storavix Web UI 8080 (LocalSubnet)' -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow -RemoteAddress LocalSubnet -Profile Domain,Private | Out-Null }
Start-ScheduledTask -TaskName $taskName
Start-Sleep -Seconds 2
Write-Host 'Проверка: http://127.0.0.1:8080/api/health'
try { (Invoke-RestMethod -Uri 'http://127.0.0.1:8080/api/health' -TimeoutSec 5) | Format-List }
catch { Write-Warning "Сервис не ответил: $_. Проверьте $dataDir\storavix.log и планировщик заданий." }
$ips=Get-NetIPAddress -AddressFamily IPv4 | Where-Object {$_.IPAddress -notlike '169.254.*' -and $_.IPAddress -ne '127.0.0.1' -and $_.AddressState -eq 'Preferred'} | Select-Object -ExpandProperty IPAddress -Unique
Write-Host 'Адреса для браузера:'
foreach ($ip in $ips) { Write-Host "  http://${ip}:8080" }
Write-Host 'Storavix запускается при старте Windows от SYSTEM через Планировщик задач.'
Write-Host "Конфигурация: $dataDir; журнал: $dataDir\storavix.log"
Write-Warning 'Сейчас доступ осуществляется по HTTP. Не публикуйте 8080 в интернет; настройте HTTPS для внешних соединений.'
