# Storavix v0.2.8 — Windows Server compatibility

- The Windows MSI and EXE embed both the modern Go 1.23 agent and a Go 1.20 legacy compatibility agent.
- The configuration wizard selects the correct executable automatically based on Windows version. Windows Server 2012 and 2012 R2 use the legacy executable; Windows 10 / Windows Server 2016+ use the modern executable.
- The previous version's persistent agent diagnostics, SYSTEM ACL fixes and simplified dark-mode SVG branding are retained.
- GitHub CI builds both binaries and verifies MSI/EXE include each of them.

**Legacy warning**: Go 1.20 and Windows Server 2012 are end-of-life and do not receive current security patches. Run legacy agents only on restricted trusted networks and upgrade the operating system when possible. Older Windows platforms are not available as GitHub-hosted test runners, so compatibility requires validation on a real Server 2012 host.
