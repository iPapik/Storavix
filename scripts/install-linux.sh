#!/usr/bin/env bash
# Storavix bare-metal installer for Debian / Ubuntu / other systemd distributions.
# Usage: sudo bash scripts/install-linux.sh
set -euo pipefail
umask 077

echo "[Storavix] Начало установки — проверка требований..."
if (( EUID != 0 )); then echo 'Ошибка: запустите sudo bash scripts/install-linux.sh' >&2; exit 1; fi
if ! command -v systemctl >/dev/null; then echo 'Требуется Linux с systemd.' >&2; exit 1; fi
if ! command -v runuser >/dev/null; then echo 'Требуется утилита runuser (util-linux).' >&2; exit 1; fi
BASE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN='/usr/local/bin/storavix'
DATA='/var/lib/storavix'
PORT=8080

echo '[1/7] Проверка бинарного файла и зависимостей...' 
if [[ -n "${STORAVIX_BINARY:-}" ]]; then
  [[ -f "$STORAVIX_BINARY" ]] || { echo "Бинарный файл не найден: $STORAVIX_BINARY" >&2; exit 1; }
  SOURCE="$STORAVIX_BINARY"
elif command -v go >/dev/null; then
  MINOR="$(go version | sed -nE 's/.*go1\.([0-9]+)(\.[0-9]+)?.*/\1/p')"
  [[ "$MINOR" =~ ^[0-9]+$ ]] && ((MINOR>=23)) || { echo 'Для сборки требуется Go >= 1.23.' >&2; exit 1; }
  # Always rebuild sources on upgrade to avoid deploying a stale binary.
  echo 'Сборка сервера из актуальных исходников...'
  (cd "$BASE" && CGO_ENABLED=0 go build -trimpath -o "$BASE/storavix" ./cmd/storavix)
  SOURCE="$BASE/storavix"
elif [[ -f "$BASE/storavix" ]]; then
  SOURCE="$BASE/storavix"
elif [[ -f "$BASE/dist/storavix-linux-amd64" ]]; then
  SOURCE="$BASE/dist/storavix-linux-amd64"
else
  echo 'Go >=1.23 или готовый бинарный файл не найден. См. https://go.dev/dl/' >&2
  exit 1
fi

echo '[2/7] Создание служебной учётной записи и каталога данных...'
if ! id storavix >/dev/null 2>&1; then
  useradd --system --home-dir "$DATA" --shell /usr/sbin/nologin storavix
fi
install -d -o storavix -g storavix -m 0700 "$DATA"
echo '[3/7] Остановка старой службы (если присутствует)...'
if systemctl is-active --quiet storavix.service; then systemctl stop storavix.service; fi
echo '[4/7] Копирование исполняемого файла...'
install -o root -g root -m 0755 "$SOURCE" "$BIN"

echo '[5/7] Настройка администратора и сети...'
if [[ ! -f "$DATA/state.json" ]]; then
  while :; do
    read -r -p 'Придумайте логин администратора [admin]: ' USERNAME
    USERNAME="${USERNAME:-admin}"
    if [[ "$USERNAME" =~ ^[A-Za-z0-9._-]{3,64}$ ]]; then break; fi
    echo 'Допустимы 3–64 символа: латиница, цифры, точка, дефис, подчёркивание.'
  done
  while :; do
    read -r -s -p 'Придумайте пароль (от 8 символов): ' PASSWORD; echo
    read -r -s -p 'Повторите пароль: ' CONFIRM; echo
    if (( ${#PASSWORD} < 8 )); then echo 'Пароль слишком короткий.'; continue; fi
    if [[ "$PASSWORD" != "$CONFIRM" ]]; then echo 'Пароли не совпадают.'; continue; fi
    break
  done
  printf '%s\n' "$PASSWORD" | runuser -u storavix -- "$BIN" init --data-dir "$DATA" --listen "0.0.0.0:${PORT}" --admin-user "$USERNAME" --password-stdin
  unset PASSWORD CONFIRM
else
  echo 'Установка уже существует: сохраняем пароль, SMTP, события и данные.'
  runuser -u storavix -- "$BIN" configure --data-dir "$DATA" --listen "0.0.0.0:${PORT}"
fi

echo '[6/7] Создание службы systemd и включение автозапуска...'
cat > /etc/systemd/system/storavix.service <<'SERVICE'
[Unit]
Description=Storavix infrastructure monitoring web server
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
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/storavix
UMask=0077

[Install]
WantedBy=multi-user.target
SERVICE
systemctl daemon-reload
systemctl enable --now storavix.service
echo '[7/7] Установка команды обновления и проверка адресов...'
if [[ -f "$BASE/scripts/update-online-linux.sh" ]]; then
  install -D -o root -g root -m 0755 "$BASE/scripts/update-online-linux.sh" /usr/local/sbin/storavix-update
  echo 'Готово: для следующих обновлений — sudo storavix-update'
else
  echo 'Для первого обновления установите updater из GitHub (см. docs/UPDATE-RU.md).'
fi
sleep 1
systemctl --no-pager --full status storavix.service || true
printf '\nАдреса доступа (фактические IPv4-адреса этого сервера):\n'
hostname -I 2>/dev/null | tr ' ' '\n' | awk -v p="$PORT" '/^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$/ {print "  http://"$0":"p}' || true
printf '\nВажно: разрешите TCP %s только из доверенной локальной сети. Для доступа извне настройте HTTPS.\n' "$PORT"
printf 'Проверка: curl -f http://127.0.0.1:%s/api/health\n' "$PORT"
printf 'Журнал: sudo journalctl -u storavix -f\n'
