#!/usr/bin/env bash
# Storavix online updater: source of truth is iPapik/Storavix on GitHub.
# Works from any current directory. Preserves configuration, users and SMTP secrets.
set -Eeuo pipefail
umask 077
export LC_ALL=C

log() { printf '\n[Storavix Update] %s\n' "$*"; }
error() { printf '\n[Storavix Update] ОШИБКА: %s\n' "$*" >&2; }
die() { error "$*"; exit 1; }

(( EUID == 0 )) || die 'Нужны права root. Пример: curl -fsSL https://raw.githubusercontent.com/iPapik/Storavix/main/scripts/update-online-linux.sh | sudo bash'
command -v systemctl >/dev/null 2>&1 || die 'Нужен Linux с systemd.'

REPO_URL="${STORAVIX_UPDATE_REPO:-https://github.com/iPapik/Storavix.git}"
BRANCH="${STORAVIX_UPDATE_BRANCH:-main}"
BIN="${STORAVIX_UPDATE_BIN:-/usr/local/bin/storavix}"
DATA="${STORAVIX_UPDATE_DATA:-/var/lib/storavix}"
UNIT="${STORAVIX_UPDATE_UNIT:-/etc/systemd/system/storavix.service}"
CLI="${STORAVIX_UPDATE_CLI:-/usr/local/sbin/storavix-update}"
AGENT_BIN="${STORAVIX_UPDATE_AGENT_BIN:-/usr/local/bin/storavix-agent}"
AGENT_UNIT="${STORAVIX_UPDATE_AGENT_UNIT:-/etc/systemd/system/storavix-agent.service}"
BACKUP_ROOT="${STORAVIX_UPDATE_BACKUP_ROOT:-/var/backups/storavix}"

# Never use the caller's working directory as the Go module root.
[[ -f "$UNIT" ]] || die "Служба Storavix не установлена: $UNIT"
[[ -f "$BIN" ]] || die "Не найден исполняемый файл Storavix: $BIN"
[[ -s "$DATA/state.json" ]] || die "Отсутствует $DATA/state.json: остановка во избежание потери учётной записи."
command -v flock >/dev/null 2>&1 || die 'Не найдена утилита flock (пакет util-linux).'
mkdir -p /run/lock
exec 9>/run/lock/storavix-update.lock
flock -n 9 || die 'Обновление уже выполняется в другом процессе.'

TEMP_DIR=''
BACKUP=''
CHANGING=0
COMPLETED=0
SERVER_WAS_ACTIVE=0
AGENT_PRESENT=0
AGENT_WAS_ACTIVE=0
SERVER_SWAPPED=0
AGENT_SWAPPED=0
CLI_UPDATED=0

rollback() {
  (( CHANGING == 1 && COMPLETED == 0 )) || return 0
  error 'Обновление прервано: восстанавливаем предыдущую рабочую версию...'
  systemctl stop storavix.service >/dev/null 2>&1 || true
  if (( AGENT_PRESENT )); then systemctl stop storavix-agent.service >/dev/null 2>&1 || true; fi
  if (( SERVER_SWAPPED )); then
    install -o root -g root -m 0755 "$BACKUP/storavix" "${BIN}.restore" && mv -f "${BIN}.restore" "$BIN" || error 'Не удалось восстановить серверный бинарник.'
  fi
  if (( AGENT_SWAPPED )); then
    install -o root -g root -m 0755 "$BACKUP/storavix-agent" "${AGENT_BIN}.restore" && mv -f "${AGENT_BIN}.restore" "$AGENT_BIN" || error 'Не удалось восстановить бинарник агента.'
  fi
  # Preserve the failed newer data set, rather than deleting it (old updater used rm -rf).
  if [[ -d "$BACKUP/data" ]]; then
    if mv "$DATA" "$BACKUP/failed-update-data"; then
      cp -a "$BACKUP/data" "$DATA" || error 'ОШИБКА возврата конфигурации. Смотрите каталог резервной копии!'
    else
      error 'Не удалось перенести изменённые данные. Не удаляйте каталог данных!'
    fi
  fi
  if (( AGENT_PRESENT )) && [[ -d "$BACKUP/agent-data" ]]; then
    if mv /var/lib/storavix-agent "$BACKUP/failed-agent-data"; then
      cp -a "$BACKUP/agent-data" /var/lib/storavix-agent || error 'Не удалось восстановить данные агента.'
    fi
  fi
  if (( SERVER_WAS_ACTIVE )); then
    systemctl start storavix.service || error 'Старая служба не запустилась; смотрите journalctl -u storavix.'
  fi
  if (( AGENT_PRESENT && AGENT_WAS_ACTIVE )); then
    systemctl start storavix-agent.service || error 'Старый агент не запустился.'
  fi
  error "Бэкап и сохранённые данные неудачной попытки: $BACKUP"
}
on_exit() {
  local code=$?
  trap - EXIT INT TERM
  set +e
  if (( code != 0 )); then rollback; fi
  if [[ -n "$TEMP_DIR" && -d "$TEMP_DIR" ]]; then rm -rf -- "$TEMP_DIR"; fi
  exit "$code"
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

log '[1/9] Проверка необходимых утилит и интернет-подключения...'
for utility in git curl python3 tar sha256sum; do
  if command -v "$utility" >/dev/null 2>&1; then continue; fi
  if command -v apt-get >/dev/null 2>&1; then
    log "Установка недостающей зависимости: $utility"
    apt-get update
    case "$utility" in
      python3) apt-get install -y python3 ;;
      sha256sum) apt-get install -y coreutils ;;
      *) apt-get install -y "$utility" ;;
    esac
  else
    die "Не найдена команда $utility. Установите её через менеджер пакетов ОС."
  fi
  command -v "$utility" >/dev/null 2>&1 || die "Не удалось установить $utility"
done
TEMP_DIR="$(mktemp -d /tmp/storavix-online.XXXXXXXX)"

log '[2/9] Загрузка актуальных исходников из GitHub (временная директория)...'
git -c advice.detachedHead=false clone --depth 1 --single-branch --branch "$BRANCH" "$REPO_URL" "$TEMP_DIR/repository" \
  || die "Невозможно загрузить $REPO_URL (ветка $BRANCH). Проверьте DNS/доступ к GitHub."
SOURCE_DIR="$TEMP_DIR/repository"
[[ -f "$SOURCE_DIR/go.mod" && -f "$SOURCE_DIR/cmd/storavix/main.go" ]] || die 'В полученном репозитории отсутствуют исходники сервера.'
COMMIT="$(git -C "$SOURCE_DIR" rev-parse --short=12 HEAD)"
printf '[Storavix Update] Получена версия GitHub: %s\n' "$COMMIT"

log '[3/9] Проверка компилятора Go...'
valid_go() {
  command -v go >/dev/null 2>&1 || return 1
  local version
  version="$(go version 2>/dev/null | sed -nE 's/.*go1\.([0-9]+)(\.[0-9]+)?.*/\1/p')"
  [[ "$version" =~ ^[0-9]+$ ]] && (( version >= 23 ))
}
if ! valid_go; then
  log 'Go 1.23+ не найден. Скачиваем актуальный официальный Go во временный каталог...'
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) die "Автоматическая установка Go для архитектуры $ARCH пока не поддерживается." ;;
  esac
  curl -fsSL --retry 3 --connect-timeout 10 --max-time 90 'https://go.dev/dl/?mode=json' -o "$TEMP_DIR/go-releases.json" \
    || die 'Не удалось получить официальный список версий Go.'
  read -r GO_VERSION GO_SHA < <(python3 - "$TEMP_DIR/go-releases.json" "$ARCH" <<'PY'
import json,sys
with open(sys.argv[1], encoding='utf-8') as source:
    versions=json.load(source)
for version in versions:
    for file in version.get('files', []):
        if file.get('os')=='linux' and file.get('arch')==sys.argv[2] and file.get('kind')=='archive':
            print(version['version'], file['sha256'])
            sys.exit(0)
sys.exit(1)
PY
)
  [[ "$GO_VERSION" =~ ^go1\.[0-9]+\.[0-9]+$ && "$GO_SHA" =~ ^[0-9a-f]{64}$ ]] || die 'Не удалось подтвердить контрольную сумму архива Go.'
  curl -fsSL --retry 3 --connect-timeout 10 --max-time 300 "https://go.dev/dl/${GO_VERSION}.linux-${ARCH}.tar.gz" -o "$TEMP_DIR/go.tar.gz" \
    || die 'Не удалось скачать Go. Никакие файлы Storavix не изменены.'
  printf '%s  %s\n' "$GO_SHA" "$TEMP_DIR/go.tar.gz" | sha256sum --check --status \
    || die 'Проверка SHA256 архива Go не пройдена.'
  mkdir -p "$TEMP_DIR/toolchain"
  tar -xzf "$TEMP_DIR/go.tar.gz" -C "$TEMP_DIR/toolchain"
  export PATH="$TEMP_DIR/toolchain/go/bin:$PATH"
  valid_go || die 'Загруженный компилятор Go не соответствует требованиям.'
fi
printf '[Storavix Update] Компилятор: %s\n' "$(go version)"

log '[4/9] Компиляция свежего сервера и локального агента...'
export CGO_ENABLED=0
(cd "$SOURCE_DIR" && go build -trimpath -o "$TEMP_DIR/storavix-new" ./cmd/storavix) || die 'Сборка сервера завершилась ошибкой. Старая версия не затронута.'
AGENT_PRESENT=0
if [[ -f "$AGENT_UNIT" && -f "$AGENT_BIN" ]]; then
  AGENT_PRESENT=1
  (cd "$SOURCE_DIR" && go build -trimpath -o "$TEMP_DIR/storavix-agent-new" ./cmd/storavix-agent) || die 'Сборка агента завершилась ошибкой.'
fi
[[ -s "$TEMP_DIR/storavix-new" ]] || die 'Сборка создала пустой бинарник.'

log '[5/9] Резервное копирование программы и данных...'
mkdir -p "$BACKUP_ROOT"
chmod 0700 "$BACKUP_ROOT"
BACKUP="$(mktemp -d "$BACKUP_ROOT/backup-$(date +%Y%m%d-%H%M%S)-XXXXXXXX")"
chmod 0700 "$BACKUP"
cp -a "$BIN" "$BACKUP/storavix"
if (( AGENT_PRESENT )); then
  cp -a "$AGENT_BIN" "$BACKUP/storavix-agent"
fi
printf '%s\n' "$COMMIT" > "$BACKUP/target-commit.txt"
printf '[Storavix Update] Резервная копия: %s\n' "$BACKUP"

log '[6/9] Выключение служб и замена файлов...'
if systemctl is-active --quiet storavix.service; then SERVER_WAS_ACTIVE=1; fi
if (( AGENT_PRESENT )) && systemctl is-active --quiet storavix-agent.service; then AGENT_WAS_ACTIVE=1; fi
CHANGING=1
systemctl stop storavix.service || die 'Не удалось остановить сервер Storavix.'
if (( AGENT_PRESENT )); then systemctl stop storavix-agent.service || die 'Не удалось остановить локальный агент.'; fi
# Stop before snapshotting state.json/master.key, avoiding a partially written backup.
cp -a "$DATA" "$BACKUP/data" || die 'Не удалось скопировать каталог данных.'
if (( AGENT_PRESENT )) && [[ -d /var/lib/storavix-agent ]]; then
  cp -a /var/lib/storavix-agent "$BACKUP/agent-data" || die 'Не удалось скопировать данные локального агента.'
fi
install -o root -g root -m 0755 "$TEMP_DIR/storavix-new" "${BIN}.new"
mv -f "${BIN}.new" "$BIN"
SERVER_SWAPPED=1
if (( AGENT_PRESENT )); then
  install -o root -g root -m 0755 "$TEMP_DIR/storavix-agent-new" "${AGENT_BIN}.new"
  mv -f "${AGENT_BIN}.new" "$AGENT_BIN"
  AGENT_SWAPPED=1
fi

log '[7/9] Запуск служб...'
systemctl start storavix.service || die 'Новая служба не запустилась.'
if (( AGENT_PRESENT && AGENT_WAS_ACTIVE )); then
  systemctl start storavix-agent.service || die 'Локальный агент не запустился.'
fi

log '[8/9] Проверка HTTP API, сохранности данных и статуса systemd...'
HEALTH_URL="$(python3 - "$DATA/instance.json" <<'PY'
import json,os,sys
host,port='127.0.0.1','8080'
try:
    with open(sys.argv[1],encoding='utf-8') as f:
        listen=json.load(f).get('listen','')
    if listen:
        addr,port=listen.rsplit(':',1)
        addr=addr.strip('[]')
        if addr not in ('','0.0.0.0','::','localhost'):
            host=addr
        elif addr=='::':
            host='::1'
except (OSError,ValueError,TypeError):
    pass
print('http://'+('['+host+']' if ':' in host else host)+':'+port+'/api/health')
PY
)"
HEALTH_OK=0
for attempt in $(seq 1 30); do
  if systemctl is-active --quiet storavix.service && curl -fsS --noproxy '*' --connect-timeout 1 --max-time 2 "$HEALTH_URL" >/dev/null 2>&1; then
    HEALTH_OK=1
    break
  fi
  sleep 1
done
(( HEALTH_OK == 1 )) || die "Нет ответа $HEALTH_URL через 30 секунд. Выполняется откат."
[[ -s "$DATA/state.json" && -s "$DATA/master.key" ]] || die 'Не найдены прежние файлы конфигурации. Выполняется откат.'
if (( AGENT_PRESENT && AGENT_WAS_ACTIVE )); then
  systemctl is-active --quiet storavix-agent.service || die 'Локальный агент не запущен.'
fi

log '[9/9] Установка команды storavix-update и завершение...'
install -d -m 0755 "$(dirname "$CLI")"
if [[ -f "$SOURCE_DIR/scripts/update-online-linux.sh" ]]; then
  install -o root -g root -m 0755 "$SOURCE_DIR/scripts/update-online-linux.sh" "${CLI}.new"
  mv -f "${CLI}.new" "$CLI"
  CLI_UPDATED=1
fi
COMPLETED=1
CHANGING=0
printf '\n=============================================\n'
printf ' Storavix успешно обновлён до %s\n' "$COMMIT"
printf ' Служба:       sudo systemctl status storavix\n'
printf ' Web UI:       %s\n' "${HEALTH_URL%/api/health}"
printf ' Следующий раз: sudo storavix-update\n'
printf ' Резервная копия: %s\n' "$BACKUP"
printf ' Журнал: sudo journalctl -u storavix -n 60 --no-pager\n'
printf '=============================================\n'
