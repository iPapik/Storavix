# Storavix

Self-hosted infrastructure monitoring foundation, focusing on future RAID, disks, SNMP and Syslog.

**v0.1.1-dev** — dark web UI, selectable initial administrator login, protected state, SMTP test alerts, IP auto-discovery. **Collectors are still in development.**

## Установка на русском

**[Полная инструкция от А до Я для Linux, Windows и Docker](docs/INSTALL-RU.md)**

### Linux без Docker

    sudo apt update
    sudo apt install -y git golang-go ca-certificates
    # Requires Go 1.23+ or a compiled binary; https://go.dev/dl/
    git clone https://github.com/iPapik/Storavix.git
    cd Storavix
    sudo bash scripts/install-linux.sh

The installer prompts for **administrator username, password and confirmation**; sets up systemd, displays IPv4 browser URLs and preserves state on upgrades.

### Windows / Windows Server без Docker

Install Git and Go 1.23+ (or use a compiled EXE), run PowerShell as Administrator:

    cd C:\
    git clone https://github.com/iPapik/Storavix.git
    cd C:\Storavix
    Set-ExecutionPolicy -Scope Process Bypass -Force
    .\scripts\install-windows.ps1

The installer prompts for credentials, sets Windows Task Scheduler autostart under SYSTEM, secures ProgramData and shows actual IPv4 addresses.

### Development

    go test ./...
    go vet ./...
    go build -o storavix ./cmd/storavix

Use HTTPS reverse proxy or VPN before untrusted access; port 8080 is plain HTTP. Docker is OPTIONAL; see guide. SMTP test emails work, but real RAID events need future agents and alert rules.

GitHub issues: [RAID agent](https://github.com/iPapik/Storavix/issues/1), [mail alert rules](https://github.com/iPapik/Storavix/issues/2), [SNMP and syslog](https://github.com/iPapik/Storavix/issues/3).

License not yet selected.