#!/usr/bin/env bash
# Install or upgrade Storavix Agent on Linux without deleting agent ID/token.
set -euo pipefail
umask 077
echo '[Storavix Agent] Начало установки...'
(( EUID == 0 )) || { echo 'Запустите с sudo.' >&2; exit 1; }
command -v systemctl >/dev/null || { echo 'Требуется systemd.' >&2; exit 1; }
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN=/usr/local/bin/storavix-agent
DATA=/var/lib/storavix-agent
CONFIG=/etc/storavix-agent
if [[ -n "${STORAVIX_AGENT_BINARY:-}" ]];then SRC="$STORAVIX_AGENT_BINARY"
elif command -v go >/dev/null;then echo '[1/6] Компиляция агента...';(cd "$ROOT" && CGO_ENABLED=0 go build -trimpath -o "$ROOT/storavix-agent" ./cmd/storavix-agent);SRC="$ROOT/storavix-agent"
elif [[ -f "$ROOT/storavix-agent" ]];then SRC="$ROOT/storavix-agent"
else echo 'Нет агента: нужен готовый бинарник или Go 1.23+' >&2;exit 1;fi
[[ -s "$SRC" ]] || { echo 'Бинарник агента пустой.' >&2;exit 1; }
echo '[2/6] Каталог состояния и служебная учётная запись...'
if ! id storavix-agent &>/dev/null;then useradd --system --home-dir "$DATA" --shell /usr/sbin/nologin storavix-agent;fi
install -d -o storavix-agent -g storavix-agent -m 0700 "$DATA"
install -d -o root -g storavix-agent -m 0750 "$CONFIG"
if [[ -f "$CONFIG/token" && -f "$CONFIG/server" ]];then
 echo '[3/6] Используем прежний токен и URL сервера (без перенастройки).'
 SERVER="$(cat "$CONFIG/server")"
else
 echo '[3/6] Настройка соединения с сервером Storavix...'
 read -r -p 'Адрес сервера Storavix (https://host или http://IP:8080): ' SERVER
 [[ "$SERVER" =~ ^https?://[A-Za-z0-9._:-]+$ ]] || { echo 'Неверный URL.' >&2;exit 1; }
 read -r -s -p 'Токен агента (с центрального Storavix agent-token): ' TOKEN; echo
 [[ "$TOKEN" =~ ^[0-9a-f]{64}$ ]] || { echo 'Неверный токен (64 hex-символа).' >&2;exit 1; }
 printf '%s\n' "$TOKEN" > "$CONFIG/token";unset TOKEN
 printf '%s\n' "$SERVER" > "$CONFIG/server"
 chown root:storavix-agent "$CONFIG/token" "$CONFIG/server"
 chmod 0640 "$CONFIG/token" "$CONFIG/server"
fi
EXTRA=''
if [[ "$SERVER" == http://* ]];then
 echo 'ВНИМАНИЕ: HTTP передаёт токен без шифрования. Используйте только изолированную доверенную сеть.'
 EXTRA='--allow-http'
fi
echo '[4/6] Установка бинарного файла...'
if systemctl is-active --quiet storavix-agent;then systemctl stop storavix-agent;fi
install -o root -g root -m 0755 "$SRC" "$BIN"
cat > /etc/systemd/system/storavix-agent.service <<SERVICE
[Unit]
Description=Storavix monitoring agent
Wants=network-online.target
After=network-online.target
[Service]
Type=simple
User=storavix-agent
Group=storavix-agent
ExecStart=/usr/local/bin/storavix-agent --server $SERVER --token-file $CONFIG/token --data-dir $DATA --interval 30s $EXTRA
Restart=on-failure
RestartSec=10
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=$DATA
UMask=0077
[Install]
WantedBy=multi-user.target
SERVICE
echo '[5/6] Включение службы...'
systemctl daemon-reload
systemctl enable --now storavix-agent
echo '[6/6] Статус агента:'
systemctl --no-pager status storavix-agent || true
printf '\nЖурнал: sudo journalctl -u storavix-agent -f\n'
