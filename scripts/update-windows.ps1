# Storavix Windows in-place server updater. Does not erase ProgramData or administrator credentials.
$ErrorActionPreference='Stop'
Write-Host '[Storavix Update] Проверка установленной версии...' -ForegroundColor Cyan
$admin=([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin){throw 'Запустите PowerShell от администратора.'}
$repo=(Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$bin=Join-Path $env:ProgramFiles 'Storavix\storavix.exe'
$data=Join-Path $env:ProgramData 'Storavix'
if (!(Test-Path $bin) -or !(Test-Path (Join-Path $data 'state.json'))){throw 'Storavix ещё не установлен.'}
$source=$null
if ($env:STORAVIX_BINARY -and (Test-Path $env:STORAVIX_BINARY)){$source=$env:STORAVIX_BINARY}
elseif (Get-Command go -ErrorAction SilentlyContinue){
 Write-Host '[1/6] Компиляция обновления из исходников...' -ForegroundColor Cyan
 Push-Location $repo
 try {New-Item -ItemType Directory dist -Force | Out-Null; & go build -trimpath -o '.\dist\storavix-update.exe' '.\cmd\storavix';if ($LASTEXITCODE -ne 0){throw 'Go build не прошёл'}} finally{Pop-Location}
 $source=Join-Path $repo 'dist\storavix-update.exe'
} elseif (Test-Path (Join-Path $repo 'storavix.exe')){$source=Join-Path $repo 'storavix.exe'}
else {throw 'Укажите STORAVIX_BINARY или установите Go 1.23+'}
Write-Host '[2/6] Резервное копирование бинарника и данных...' -ForegroundColor Cyan
$backup=Join-Path $env:ProgramData ('Storavix-Backups\'+(Get-Date -Format 'yyyyMMdd-HHmmss'))
New-Item -ItemType Directory $backup -Force | Out-Null
Copy-Item $bin (Join-Path $backup 'storavix.exe') -Force
Copy-Item $data (Join-Path $backup 'data') -Recurse -Force
Write-Host '[3/6] Остановка задачи Storavix...' -ForegroundColor Cyan
Stop-ScheduledTask -TaskName Storavix -ErrorAction SilentlyContinue
Start-Sleep -Seconds 3
try{
 Copy-Item $source $bin -Force
 Write-Host '[4/6] Запуск обновлённой задачи...' -ForegroundColor Cyan
 Start-ScheduledTask -TaskName Storavix
 Write-Host '[5/6] Проверка /api/health...' -ForegroundColor Cyan
 $ok=$false
 for($i=0;$i -lt 20;$i++){try{$r=Invoke-RestMethod 'http://127.0.0.1:8080/api/health' -TimeoutSec 2;if($r.status -eq 'ok'){$ok=$true;break}}catch{};Start-Sleep -Seconds 1}
 if(-not $ok){throw 'Новая версия не ответила на HTTP health check.'}
 Write-Host '[6/6] Успешно. Все данные остались в ProgramData.' -ForegroundColor Green
 Write-Host "Резервная копия: $backup"
}catch{
 Write-Warning "Ошибка обновления: $_. Откат предыдущей версии..."
 Stop-ScheduledTask -TaskName Storavix -ErrorAction SilentlyContinue
 Start-Sleep -Seconds 3
 Copy-Item (Join-Path $backup 'storavix.exe') $bin -Force
 Remove-Item $data -Recurse -Force
 Copy-Item (Join-Path $backup 'data') $data -Recurse -Force
 Start-ScheduledTask -TaskName Storavix
 throw
}
