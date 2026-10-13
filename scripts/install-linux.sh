#!/bin/bash
set -euo pipefail
umask 077
(( EUID == 0 )) || { echo 'Use sudo'; exit 1; }
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATA=/var/lib/storavix
if command -v go >/dev/null; then
  (cd "$ROOT" && CGO_ENABLED=0 go build -o storavix ./cmd/storavix)
  SRC="$ROOT/storavix"
else
  SRC="${STORAVIX_BINARY:-$ROOT/storavix}"
  test -f "$SRC" || { echo 'Go 1.23+ or STORAVIX_BINARY required'; exit 1; }
fi
id storavix >/dev/null 2>&1 || useradd --system --home-dir "$DATA" --shell /usr/sbin/nologin storavix
install -d -m 0700 -o storavix -g storavix "$DATA"
systemctl is-active --quiet storavix && systemctl stop storavix || true
install -m 0755 "$SRC" /usr/local/bin/storavix
if [[ ! -f "$DATA/state.json" ]]; then
  while :; do
    read -r -p 'Логин администратора [admin]: ' USERNAME
    USERNAME="${USERNAME:-admin}"
    [[ "$USERNAME" =~ ^[a-zA-Z0-9._-]{3,64}$ ]] && break
    echo 'Логин: 3–64 символа, латиница, цифры, . _ -'
  done
  while :; do
    read -rs -p 'Пароль (от 12 символов): ' PASSWORD; echo
    read -rs -p 'Повторите пароль: ' CONFIRM; echo
    (( ${#PASSWORD} >= 12 )) && [[ "$PASSWORD" == "$CONFIRM" ]] && break
    echo 'Пароли не совпадают или слишком короткие'
  done
  printf '%s\n' "$PASSWORD" | runuser -u storavix -- /usr/local/bin/storavix init --data-dir "$DATA" --admin-user "$USERNAME" --listen 0.0.0.0:8080 --password-stdin
  unset PASSWORD CONFIRM
else
  echo 'Существующая учётная запись и конфигурация сохранены'
  runuser -u storavix -- /usr/local/bin/storavix configure --data-dir "$DATA" --listen 0.0.0.0:8080
fi
cat > /etc/systemd/system/storavix.service <<'EOF'
[Unit]
Description=Storavix Web Monitoring
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
User=storavix
Group=storavix
WorkingDirectory=/var/lib/storavix
ExecStart=/usr/local/bin/storavix --data-dir /var/lib/storavix
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/storavix
UMask=0077
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now storavix
systemctl --no-pager status storavix || true
hostname -I | tr ' ' '\n' | awk '/^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$/ {print "Web panel: http://"$0":8080"}'
echo 'Allow TCP 8080 only for trusted LAN. For production deploy HTTPS reverse proxy.'