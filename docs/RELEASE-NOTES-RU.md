# Storavix v0.2.2 — Windows Agent installer fix and host layouts

- Fixed Windows PowerShell 5.1 parsing failures from UTF-8 without BOM. Both agent installer scripts use ASCII-only prompts/status.
- Added one-command agent updates: `sudo storavix-agent-update` (Linux) and `storavix-agent-update` (Windows in a new elevated terminal).
- Reuse existing agent token, server URL and persistent identity during updates.
- Windows agent setup backs up the previous binary and attempts to restore it on startup failure.
- Enlarged host cards and added 2-column, 3-column, 4-column and list layouts with browser-side persistence.
- Release includes agent/server binaries for Windows and Linux, AMD64/ARM64, with SHA256SUMS.

**Note:** The agent EXE is a background worker, not a GUI installer. Hardware RAID/SMART are still not fully implemented. Unsigned binaries may trigger SmartScreen; HTTPS is recommended.
