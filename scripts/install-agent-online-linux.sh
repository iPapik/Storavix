#!/usr/bin/env bash
# Installs or updates the Storavix Agent from an official GitHub Release.
set -Eeuo pipefail
umask 077
echo '[Storavix Agent] Подготовка установки...'
[[ $EUID -eq 0 ]] || { echo 'Нужен sudo.' >&2; exit 1; }
for cmd in curl tar sha256sum systemctl; do command -v "$cmd" >/dev/null || { echo "Требуется: $cmd" >&2; exit 1; }; done
case "$(uname -m)" in x86_64|amd64) arch=amd64;; aarch64|arm64) arch=arm64;; *) echo 'Поддерживаются Linux amd64/arm64' >&2; exit 1;; esac
base=https://github.com/iPapik/Storavix/releases/latest/download
if [[ -n "${STORAVIX_RELEASE_TAG:-}" ]]; then
  [[ "$STORAVIX_RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Неверный номер версии' >&2; exit 1; }
  base="https://github.com/iPapik/Storavix/releases/download/$STORAVIX_RELEASE_TAG"
fi
asset="storavix-agent-linux-$arch.tar.gz"
work=$(mktemp -d /tmp/storavix-agent.XXXXXXXX)
trap 'rm -rf -- "$work"' EXIT
echo "[1/5] Загрузка $asset из GitHub Releases..."
curl -fSL --retry 3 --connect-timeout 15 --max-time 240 "$base/$asset" -o "$work/$asset"
curl -fSL --retry 3 --connect-timeout 15 --max-time 90 "$base/SHA256SUMS" -o "$work/SHA256SUMS"
echo '[2/5] Проверка SHA-256...'
(cd "$work" && awk -v name="$asset" '$2 == name {print}' SHA256SUMS | sha256sum --check --status) || { echo 'Ошибка SHA-256. Установка отменена.' >&2; exit 1; }
echo '[3/5] Распаковка...'
tar -xzf "$work/$asset" -C "$work" --no-same-owner
[[ -s "$work/storavix-agent" && -f "$work/scripts/install-agent-linux.sh" ]] || { echo 'Некорректный пакет!' >&2; exit 1; }
echo '[4/5] Установка службы и сохранение существующих настроек...'
STORAVIX_AGENT_BINARY="$work/storavix-agent" bash "$work/scripts/install-agent-linux.sh"
echo '[5/5] Готово. sudo systemctl status storavix-agent --no-pager'
