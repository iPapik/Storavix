#define AppVersion "0.2.23"
#ifndef SourceRoot
#define SourceRoot "..\.."
#endif
[Setup]
AppId={{B19EF4FC-63E8-4501-A3E4-8FE984FEBA31}
AppName=Storavix Agent
AppVersion={#AppVersion}
AppPublisher=Storavix
DefaultDirName={autopf}\Storavix Agent
DefaultGroupName=Storavix Agent
DisableProgramGroupPage=yes
OutputDir={#SourceRoot}\dist
OutputBaseFilename=StoravixAgent-Setup-windows-amd64
Compression=lzma2
SolidCompression=yes
PrivilegesRequired=admin
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
UninstallDisplayName=Storavix Agent
WizardStyle=modern

[Files]
Source: "{#SourceRoot}\build\storavix-agent.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceRoot}\build\storavix-agent-legacy-windows-amd64.exe"; DestDir: "{app}"; DestName: "storavix-agent-legacy.exe"; Flags: ignoreversion
Source: "{#SourceRoot}\scripts\configure-agent-windows.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceRoot}\scripts\configure-agent.cmd"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceRoot}\scripts\diagnose-agent-windows.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceRoot}\scripts\diagnose-agent.cmd"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\Configure Storavix Agent"; Filename: "{app}\configure-agent.cmd"
Name: "{group}\Diagnose Storavix Agent"; Filename: "{app}\diagnose-agent.cmd"

[Run]
; Configure new interactive installs and resume configured agents in every mode.
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\configure-agent-windows.ps1"" -ConfigureOnInstall"; Description: "Configure and start Storavix Agent"; Flags: skipifsilent runascurrentuser waituntilterminated
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File ""{app}\configure-agent-windows.ps1"" -ReuseSavedConfiguration"; Flags: skipifnotsilent runhidden runascurrentuser waituntilterminated

[UninstallRun]
Filename: "{sys}\schtasks.exe"; Parameters: "/End /TN StoravixAgent"; Flags: runhidden
Filename: "{sys}\schtasks.exe"; Parameters: "/Delete /TN StoravixAgent /F"; Flags: runhidden

[Code]
var
  AgentTaskStopped: Boolean;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
begin
  // Stop an existing agent to permit an in-place binary replacement.
  AgentTaskStopped := Exec(ExpandConstant('{sys}\schtasks.exe'), '/End /TN StoravixAgent', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) and (ResultCode = 0);
  if AgentTaskStopped then
    Sleep(1500);
  Result := '';
end;

procedure DeinitializeSetup;
var
  ResultCode: Integer;
begin
  // Also resume the registered task if installation ended before [Run].
  if AgentTaskStopped then
    Exec(ExpandConstant('{sys}\schtasks.exe'), '/Run /TN StoravixAgent', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;
