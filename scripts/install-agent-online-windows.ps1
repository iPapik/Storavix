# Run from an elevated Windows PowerShell terminal. Windows 10/11/Server.
$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
$admin=([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if(-not $admin){throw 'Запустите PowerShell от имени администратора.'}
$arch=$env:PROCESSOR_ARCHITECTURE.ToUpperInvariant()
if($arch -eq 'AMD64'){$arch='amd64'} elseif($arch -eq 'ARM64'){$arch='arm64'} else{throw "Архитектура $arch не поддерживается"}
$base='https://github.com/iPapik/Storavix/releases/latest/download'
if($env:STORAVIX_RELEASE_TAG){
  if($env:STORAVIX_RELEASE_TAG -notmatch '^v\d+\.\d+\.\d+$'){throw 'Неправильная версия релиза'}
  $base='https://github.com/iPapik/Storavix/releases/download/'+$env:STORAVIX_RELEASE_TAG
}
$asset='storavix-agent-windows-'+$arch+'.zip'
$temp=Join-Path $env:TEMP ('storavix-agent-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temp -Force | Out-Null
try{
  Write-Host "[1/5] Скачивание $asset из GitHub Releases..."
  $zip=Join-Path $temp $asset; $sum=Join-Path $temp 'SHA256SUMS'
  Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $zip
  Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile $sum
  Write-Host '[2/5] Проверка SHA-256...'
  $matchesForAsset=@(foreach($line in (Get-Content $sum)){
    if($line -match '^([A-Fa-f0-9]{64})\s+\*?(\S+)$' -and $Matches[2] -eq $asset){$Matches[1].ToUpperInvariant()}
  })
  if($matchesForAsset.Count -ne 1){throw "Не найдена контрольная сумма: $asset"}
  if((Get-FileHash -Path $zip -Algorithm SHA256).Hash -ne $matchesForAsset[0]){throw 'Контрольная сумма не совпала. Установка отменена.'}
  Write-Host '[3/5] Распаковка...'
  $dest=Join-Path $temp 'package'
  Expand-Archive -Path $zip -DestinationPath $dest -Force
  $exe=Join-Path $dest 'storavix-agent.exe'
  $setup=Join-Path $dest 'scripts\install-agent-windows.ps1'
  if(!(Test-Path $exe) -or !(Test-Path $setup)){throw 'Не найден EXE или скрипт установки в пакете'}
  Write-Host '[4/5] Установка / обновление агента...'
  $old=$env:STORAVIX_AGENT_BINARY
  try{
    $env:STORAVIX_AGENT_BINARY=$exe
    & $setup
    if(-not $?){throw 'Установка агента завершилась ошибкой'}
  }finally{
    if($null -eq $old){Remove-Item Env:STORAVIX_AGENT_BINARY -ErrorAction SilentlyContinue}
    else{$env:STORAVIX_AGENT_BINARY=$old}
  }
  Write-Host '[5/5] Готово. Get-ScheduledTask -TaskName StoravixAgent' -ForegroundColor Green
}finally{
  Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}
