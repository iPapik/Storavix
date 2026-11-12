# Storavix v0.2.4 — offline Windows agent installers

- Added an actual offline Windows MSI for the Storavix Agent.
- Added a self-contained Setup EXE built with Inno Setup.
- Both packages bundle the Windows x64 agent and a graphical connection wizard (URL and registration token).
- EXE may launch the connection wizard after installation. MSI provides a Start Menu shortcut and supports silent deployment with msiexec /qn.
- Keeps token, agent ID and server settings in ProgramData on reinstall.
- CI builds, smoke-installs and publishes both packages with SHA256SUMS.
- Works on servers that cannot download GitHub Release assets: transfer installer files from another PC.

MSI silent installation deploys files but must be configured separately with the wizard. Installers are not Authenticode signed. Windows x64 only.
