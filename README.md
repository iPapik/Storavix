# Storavix

**Storavix** — self-hosted monitoring platform focused on RAID controllers, physical disks, infrastructure events, network devices and syslog.

**Current version: v0.1.0-dev (bootstrap / foundation).** This is a working HTTP application with a dark dashboard, single-admin login, encrypted SMTP credentials, SMTP connection test and test alert history. **Hardware collectors, RAID diagnostics, network monitoring and syslog ingestion are not implemented yet**; their pages are honest empty states.

## Quick start: Docker Compose (Linux / Windows with Docker)

```bash
git clone https://github.com/iPapik/Storavix.git
cd Storavix
cp .env.example .env
# Edit .env and set a strong STORAVIX_ADMIN_PASSWORD
# For remote access, adjust STORAVIX_BIND_IP and set up HTTPS reverse proxy.
docker compose up -d --build
```

Open **http://127.0.0.1:8080** on the server. If installing on another machine, use SSH port forwarding or configure a TLS reverse proxy. The default Docker port binds to localhost only.

Logs: `docker compose logs -f storavix`. Stop: `docker compose down`. The `storavix-data` volume survives updates.

## Quick start: Go 1.23+ (Linux / Windows)

```bash
# Linux/macOS shell
export STORAVIX_ADMIN_PASSWORD='a-long-random-first-run-password'
go run ./cmd/storavix
```

```powershell
# Windows PowerShell
$env:STORAVIX_ADMIN_PASSWORD = 'a-long-random-first-run-password'
go run .\cmd\storavix
```

Open **http://127.0.0.1:8080**. This generates a local `./data/` directory with `state.json` and `master.key`. If the administrator password is not specified, a random first-run password is printed ONCE in console logs. The environment password is only used when initializing a new data directory; changing it later will not reset the admin password. Use Settings → Security instead.

To accept connections on a trusted network set `STORAVIX_LISTEN=0.0.0.0:8080` and **secure external access using HTTPS, firewall and appropriate authentication**. With an HTTPS reverse proxy also set `STORAVIX_SECURE_COOKIES=true`. **Do not expose plain HTTP to the Internet.**

## Download binaries (CI)

Every push to `main` builds Linux amd64/arm64 and Windows amd64/arm64 executable artifacts in GitHub Actions → CI and builds → the successful run → Artifacts. Download, unpack, run executable with the same `STORAVIX_*` variables. No Go runtime is required to execute a compiled binary. These are executable artifacts, **not** native MSI/DEB/RPM installers yet. On Windows, binaries may need SmartScreen approval because they are unsigned.

## SMTP alert configuration

Go to **Settings → Почтовые уведомления**. Enter host, port, transport security, auth type, sender, recipients, and credentials. Save first, then send test email or create a test alert.

| Scenario | Security | Auth | Default port |
| --- | --- | --- | --- |
| Local SMTP relay | none | none | 25 |
| Authenticated submission | starttls | plain/login/crammd5 | 587 |
| Implicit SSL/TLS | tls | plain/login/crammd5 | 465 |
| SMTP with provider access token | tls/starttls | xoauth2 | 465/587 |

- TLS verifies hostnames and CA chains (TLS 1.2+). Optionally point **CA file** to a PEM trust bundle on the **server filesystem**, mounted into a Docker container if necessary.
- Password/token is encrypted with AES-GCM in `state.json`, using a 32-byte key in `data/master.key`, or an external base64 `STORAVIX_MASTER_KEY` environment variable. **Back up state.json and master.key together**, keep them private, and never commit them to GitHub.
- Basic SMTP auth over unencrypted connections is rejected. A relay on port 25 without auth is supported.
- OAuth2 currently accepts a **manually supplied access token** only; provider-specific interactive OAuth flows and refresh-token management are not yet implemented.
- The application sends test messages and saves test-alert history; automatic delivery for RAID/SNMP/Syslog events will be wired when real collectors arrive.

## Architecture

```text
Browser → Storavix server (Go, embedded CSS/JS)
             ├─ Login / sessions / APIs
             ├─ SMTP client / test alerts
             └─ Local state file (temporary MVP storage)
Future: Windows/Linux agents → RAID adapters → normalizer → PostgreSQL/history → alerts
Future: SNMP poller & syslog receiver → event engine
```

The Go standard library is sufficient to run this foundation; there is **no external database dependency** in v0.1. Historical metrics and syslog ingestion require a proper database in later versions. This first release intentionally uses local state files to minimize installation friction.

## Security notes and limitations

- A single administrator user is supported. PBKDF2-HMAC-SHA256 password hashing; random session cookies, HttpOnly and SameSite=Strict; JSON API authorization; login rate limiting; simple CSRF mitigations and outbound SMTP TLS verification are implemented.
- For shared production deployments, add a TLS reverse proxy, restrict firewall access, and back up the `data` directory. Do not mount the data folder on public shares.
- The local startup command defaults to loopback only, so a fresh installation does not accidentally expose itself on the Internet.
- Do not confuse the UI's **web server online** badge with RAID/server health. **No equipment data is collected yet.**

## Development

```bash
go test ./...
go vet ./...
go build -o storavix ./cmd/storavix
```

Roadmap: v0.1 foundation → RAID agents and vendor CLI adapters (StorCLI / PERCCLI / SSACLI, Linux mdadm, Windows Storage Spaces) → SNMPv3 + syslog → DB-backed metrics, alert rules, LDAP integration, installation packages.

License: not selected yet; please add an explicit OSS license (for example Apache-2.0 or MIT) after approving the licensing choice. A public GitHub repository alone does **not** automatically grant redistribution rights.
