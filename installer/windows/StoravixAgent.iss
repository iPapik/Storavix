#define AppVersion "0.2.4"
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
Source: "{#SourceRoot}\scripts\configure-agent-windows.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceRoot}\scripts\configure-agent.cmd"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\Configure Storavix Agent"; Filename: "{app}\configure-agent.cmd"

[Run]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\configure-agent-windows.ps1"""; Description: "Configure and start Storavix Agent"; Flags: postinstall skipifsilent runascurrentuser

[UninstallRun]
Filename: "{sys}\schtasks.exe"; Parameters: "/End /TN StoravixAgent"; Flags: runhidden
Filename: "{sys}\schtasks.exe"; Parameters: "/Delete /TN StoravixAgent /F"; Flags: runhidden

[Code]
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
begin
  // Stop an existing agent to permit a binary replacement.
  Exec(ExpandConstant('{sys}\schtasks.exe'), '/End /TN StoravixAgent', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(1500);
  Result := '';
end;
