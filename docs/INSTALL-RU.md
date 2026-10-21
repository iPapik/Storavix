# Storavix — установка и первый запуск (Windows / Linux)

**Актуальная версия проекта:** v0.2.0-dev. Исходники: https://github.com/iPapik/Storavix. Для диагностики аппаратных RAID пока нужны будущие адаптеры; первые агенты передают CPU, RAM, системный диск, сеть, uptime и базовые сведения mdadm на Linux.

## 1. Перед установкой

- Linux с `systemd` (рекомендуются Debian 12/13 или Ubuntu Server 22.04/24.04/26.04) либо Windows с правами администратора.
- Для установки из GitHub-исходников: `git` и Go **1.23 или новее**. Для установки из уже готового бинарника Go не нужен.
- Открытый только в доверенной локальной сети TCP-порт 8080. Для эксплуатации в недоверенной сети нужен HTTPS-прокси.
- Docker для установки на обычный Linux-сервер не нужен.

## 2. Установка сервера на Linux

Откройте терминал под учётной записью с правами `sudo`:

```bash
sudo apt update
sudo apt install -y git curl ca-certificates golang-go

go version  # требуется >= 1.23

git clone https://github.com/iPapik/Storavix.git
cd Storavix
sudo bash scripts/install-linux.sh
```

**Если `go version` показывает версию ниже 1.23**, установите актуальный Go с https://go.dev/dl/ перед вызовом установщика. Важно: установщик проверяет версию компилятора и не использует слишком старый Go.

Во время установки выводятся этапы сборки, затем запрашиваются логин и пароль администратора (минимум **8** символов). Создаются системная учётная запись `storavix`, бинарник `/usr/local/bin/storavix`, каталог данных `/var/lib/storavix` и служба `storavix.service`. Установщик показывает IP-адреса сервера для браузера.

После установки:

```bash
sudo systemctl status storavix --no-pager
curl -fsS http://127.0.0.1:8080/api/health
hostname -I
```

Откройте `http://IP-СЕРВЕРА:8080` с другого компьютера, если порт разрешён межсетевым экраном. Входите с созданными при установке логином и паролем. `0.0.0.0` — адрес прослушивания, а **не URL для браузера**.

## 3. Обновление установленной Linux-версии одной командой

**Не нужно скачивать новый ZIP или находить локальный скрипт в `~/Storavix`.** В любом каталоге выполните:

```bash
bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/iPapik/Storavix/main/scripts/update-online-linux.sh | sudo bash'
```

После первого успешного обновления следующую версию устанавливайте ещё проще:

```bash
sudo storavix-update
```

Эта команда получает свежий исходный код с GitHub, собирает бинарник во временном каталоге, сохраняет настройки администратора и SMTP, обновляет службу и проверяет `/api/health`. Подробности и откат — [UPDATE-RU.md](UPDATE-RU.md).

## 4. Установка сервера на Windows

Windows-сборка (`storavix.exe`) должна быть получена из **успешной сборки GitHub Actions** или из подготовленного комплекта. Распакуйте её вместе с папкой `scripts` и откройте PowerShell от имени администратора:

```powershell
cd C:\Storavix
$env:STORAVIX_BINARY = 'C:\Storavix\storavix.exe'
Set-ExecutionPolicy -Scope Process Bypass -Force
.\scripts\install-windows.ps1
```

Установщик создаёт администратора, сохраняет данные в `C:\ProgramData\Storavix` и настраивает автозапуск через Планировщик заданий. Проверка:

```powershell
Get-ScheduledTask -TaskName Storavix
Invoke-RestMethod http://127.0.0.1:8080/api/health
```

Обновление Windows пока производится отдельным PowerShell-скриптом из новой версии (`scripts/update-windows.ps1`); команда `sudo storavix-update` предназначена **только для Linux**.

## 5. Подключение агентов

Получите токен на центральном Linux-сервере:

```bash
sudo runuser -u storavix -- /usr/local/bin/storavix agent-token --data-dir /var/lib/storavix
```

На наблюдаемом Linux-хосте с распакованными файлами агента выполните `sudo bash scripts/install-agent-linux.sh`; скрипт попросит URL центрального Storavix и токен. На Windows выполните `scripts/install-agent-windows.ps1` в PowerShell администратора. Данные появляются на странице «Все системы». Полное руководство: [AGENT-RU.md](AGENT-RU.md).

## 6. SMTP и диагностика

В панели откройте настройки почтовых уведомлений и введите SMTP-хост, порт, режим шифрования (`STARTTLS`, `SMTPS/TLS` или `none` для доверенного локального relay), способ авторизации, адрес отправителя и получателей. Отправьте тестовое письмо. XOAUTH2 поддерживает уже выданный токен, но автоматического обновления токена ещё нет.

```bash
sudo journalctl -u storavix -n 100 --no-pager
sudo systemctl restart storavix
sudo ss -lntp | grep ':8080'
```

Не удаляйте `/var/lib/storavix` — там хранятся хэши паролей, ключ SMTP, история и инвентаризация. Для постоянного доступа с рабочих мест настройте HTTPS и ограничьте вход на порт 8080 доверенными адресами.