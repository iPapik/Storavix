#!/usr/bin/env bash
# In-place Storavix server update; keeps /var/lib/storavix unchanged and rolls back on health-check failure.
set -euo pipefail
umask 077
printf '\n[Storavix Update] Запуск проверки текущей установки...\n'
(( EUID == 0 )) || { echo 'Запустите sudo bash scripts/update-linux.sh' >&2; exit 1; }
[[ -f /etc/systemd/system/storavix.service ]] || { echo 'Служба не установлена. Сначала выполните install-linux.sh.' >&2; exit 1; }
[[ -f /var/lib/storavix/state.json ]] || { echo 'Нет существующих настроек. Останавливаем обновление.' >&2; exit 1; }
BASE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN=/usr/local/bin/storavix
[[ -f "$BIN" ]] || { echo 'Установленный бинарный файл не найден.' >&2; exit 1; }
if [[ -n "${STORAVIX_BINARY:-}" ]]; then
  SOURCE="$STORAVIX_BINARY"
elif command -v go >/dev/null; then
  echo '[1/6] Компиляция новой версии из актуальных исходников...'
  mkdir -p "$BASE/dist"
  (cd "$BASE" && CGO_ENABLED=0 go build -trimpath -o "$BASE/dist/storavix-update" ./cmd/storavix)
  SOURCE="$BASE/dist/storavix-update"
elif [[ -f "$BASE/storavix" ]]; then SOURCE="$BASE/storavix"
else echo 'Требуется Go или новый готовый бинарник STORAVIX_BINARY.' >&2; exit 1
fi
[[ -s "$SOURCE" ]] || { echo 'Новый бинарник отсутствует.' >&2; exit 1; }
TMP="$(mktemp -d /tmp/storavix-update.XXXXXXXX)"
trap 'rm -rf "$TMP"' EXIT
BACKUP="/var/backups/storavix/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP"
echo '[2/6] Резервная копия файлов и настроек...'
cp -a "$BIN" "$BACKUP/storavix"
cp -a /var/lib/storavix "$BACKUP/data"
was_active=0
if systemctl is-active --quiet storavix; then was_active=1; fi
echo '[3/6] Остановка службы и установка обновления...'
systemctl stop storavix
restore(){
 echo '[ROLLBACK] Восстанавливаем бинарный файл предыдущей версии...' >&2
 install -m 0755 "$BACKUP/storavix" "$BIN"
 rm -rf /var/lib/storavix
 cp -a "$BACKUP/data" /var/lib/storavix
 systemctl start storavix || true
 echo "Архив настроек сохранён: $BACKUP/data" >&2
}
if ! install -o root -g root -m 0755 "$SOURCE" "$BIN"; then restore; exit 1; fi
echo '[4/6] Запуск новой версии...'
if ! systemctl start storavix; then restore; exit 1; fi
echo '[5/6] Проверка /api/health (до 20 секунд)...'
healthy=0
for i in {1..20}; do if curl -fsS --max-time 2 http://127.0.0.1:8080/api/health > /dev/null; then healthy=1; break; fi; sleep 1; done
if ((healthy==0));then restore;echo 'Проверка HTTP не пройдена, обновление отменено.' >&2;exit 1;fi
echo '[6/6] Обновление завершено. Пользователи, SMTP и данные сохранены.'
echo "Бэкап: $BACKUP"
echo 'Состояние: sudo systemctl status storavix'
echo 'Журнал: sudo journalctl -u storavix -n 60 --no-pager'
