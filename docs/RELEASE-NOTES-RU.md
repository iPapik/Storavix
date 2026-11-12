# Storavix v0.2.9 — URL trailing slash regression fix

## Fixed Windows agent startup
- Agent now accepts a valid base URL either with or without a trailing slash, e.g. `http://192.168.111.130:8080/`.
- The Windows setup wizard normalizes the URL to the origin (without trailing slash) before writing the config file or registering a scheduled task.
- Invalid paths, query strings, embedded credentials and insecure HTTP without explicit `--allow-http` remain rejected.
- Added Go table tests with both URL forms and negative cases.
- Changed Windows MSI and EXE smoke tests to pass a trailing-slash URL and assert the persisted URL and scheduled-task command are normalized.

## Web UI
- Replaced the remaining old logo image on the overview page with the new transparent SVG mark.
- Removed the old framed background around the overview mark.

## Upgrade
- Do not delete the stored token or agent identity. Install MSI/EXE v0.2.9 over existing installation.
- The Windows setup wizard will normalize an existing URL saved with a trailing slash.
