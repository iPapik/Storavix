# Storavix Agent: Windows PowerShell 5.1+ ASCII-compatible online installer.
$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
$admin=([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if(-not $admin){throw 'Run PowerShell as Administrator.'}
$arch=[System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToUpperInvariant()
if($arch -eq 'AMD64'){$arch='amd64'} elseif($arch -eq 'ARM64'){$arch='arm64'} else{throw "Unsupported architecture $arch"}
$base='https://github.com/iPapik/Storavix/releases/latest/download'
if($env:STORAVIX_RELEASE_TAG){
  if($env:STORAVIX_RELEASE_TAG -notmatch '^v\d+\.\d+\.\d+$'){throw 'Invalid release version'}
  $base='https://github.com/iPapik/Storavix/releases/download/'+$env:STORAVIX_RELEASE_TAG
}
$asset='storavix-agent-windows-'+$arch+'.zip'
$temp=Join-Path $env:TEMP ('storavix-agent-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temp -Force | Out-Null
try{
  Write-Host "[1/5] Downloading $asset from GitHub Releases..."
  $zip=Join-Path $temp $asset; $sum=Join-Path $temp 'SHA256SUMS'
  Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $zip
  Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile $sum
  Write-Host '[2/5] Verifying SHA-256...'
  $matchesForAsset=@(foreach($line in (Get-Content $sum)){
    if($line -match '^([A-Fa-f0-9]{64})\s+\*?(\S+)$' -and $Matches[2] -eq $asset){$Matches[1].ToUpperInvariant()}
  })
  if($matchesForAsset.Count -ne 1){throw "Missing SHA-256: $asset"}
  if((Get-FileHash -Path $zip -Algorithm SHA256).Hash -ne $matchesForAsset[0]){throw 'SHA-256 mismatch. Installation cancelled.'}
  Write-Host '[3/5] Extracting...'
  $dest=Join-Path $temp 'package'
  Expand-Archive -Path $zip -DestinationPath $dest -Force
  $exe=Join-Path $dest 'storavix-agent.exe'
  $setup=Join-Path $dest 'scripts\install-agent-windows.ps1'
  if(!(Test-Path $exe) -or !(Test-Path $setup)){throw 'Missing EXE or install script in release package'}
  Write-Host '[4/5] Installing or updating agent...'
  $old=$env:STORAVIX_AGENT_BINARY
  try{
    $env:STORAVIX_AGENT_BINARY=$exe
    & $setup
    if(-not $?){throw 'Agent installation failed'}
  }finally{
    if($null -eq $old){Remove-Item Env:STORAVIX_AGENT_BINARY -ErrorAction SilentlyContinue}
    else{$env:STORAVIX_AGENT_BINARY=$old}
  }
  Write-Host '[5/5] Done. Get-ScheduledTask -TaskName StoravixAgent' -ForegroundColor Green
}finally{
  Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}
