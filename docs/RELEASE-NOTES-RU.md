# Storavix v0.2.7 — Windows startup diagnostics and transparent brand

## Windows agent
- Every agent start logs to `C:\ProgramData\StoravixAgent\agent.log` before URL, token and identity checks.
- Failed Task Scheduler start now includes the last lines of the actual agent log in the configuration error and in `setup.log`.
- Existing `token` and `server.txt` files are explicitly granted SYSTEM and Administrators access, including older installations.
- MSI/EXE include `Diagnose Storavix Agent` in the Start menu. The diagnostic script shows task state, last result, server connectivity and non-secret log output.
- CI checks that a real task creates `agent.log`, and confirms fatal startup errors are written into a log.

## Branding
- New Storavix S/A logo and wordmark are transparent SVGs, native to the charcoal interface with cyan and silver accents.
- No opaque picture rectangle; consistent styling in the sidebar and login.
- Version numbers updated consistently across server and agent.

**HTTP is insecure** for registration tokens; use it only in a trusted isolated network. Production systems should use HTTPS.
