# Storavix Agent diagnostics (Windows PowerShell 5.1+). Does not print tokens.
[CmdletBinding()]
param()
$ErrorActionPreference = 'Continue'
$data = Join-Path $env:ProgramData 'StoravixAgent'
$dir = Join-Path $env:ProgramFiles 'Storavix Agent'
Write-Host '=== Storavix Agent Windows diagnostics ==='
Write-Host ('Agent executable present: '+(Test-Path (Join-Path $dir 'storavix-agent.exe')))
Write-Host ('Data directory present: '+(Test-Path $data))
Write-Host ('Token file present: '+(Test-Path (Join-Path $data 'token')))
Write-Host ('Agent ID present: '+(Test-Path (Join-Path $data 'agent.id')))
$serverFile=Join-Path $data 'server.txt'
if(Test-Path $serverFile){
  $server=(Get-Content $serverFile -Raw).Trim()
  Write-Host ('Server: '+$server)
  try{
    $u=[uri]$server
    $port=if($u.IsDefaultPort){if($u.Scheme -eq 'https'){443}else{80}}else{$u.Port}
    $tcp=Test-NetConnection -ComputerName $u.Host -Port $port -WarningAction SilentlyContinue
    Write-Host ('Central server TCP reachable: '+$tcp.TcpTestSucceeded)
  }catch{Write-Host ('Network probe failed: '+$_.Exception.Message)}
}
try {
  $task=Get-ScheduledTask -TaskName StoravixAgent -ErrorAction Stop
  $task | Select-Object TaskName, State | Format-Table -AutoSize
  $task.Actions | Select-Object Execute,Arguments,WorkingDirectory | Format-List
  Get-ScheduledTaskInfo -TaskName StoravixAgent | Select-Object LastRunTime,LastTaskResult | Format-List
}catch{Write-Host ('Scheduled task lookup failed: '+$_.Exception.Message)}
foreach($name in @('setup.log','agent.log')){
  $file=Join-Path $data $name
  Write-Host ('--- '+$file+' ---')
  if(Test-Path $file){Get-Content $file -Tail 40}
  else{Write-Host '(file not created)'}
}
Write-Host '=== End of diagnostics (registration token not displayed) ==='
