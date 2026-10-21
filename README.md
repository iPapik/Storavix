# Storavix

**Storavix** — самостоятельная платформа мониторинга Windows, Linux, RAID и сетевой инфраструктуры. Версия **v0.2.2**: чёрный веб-интерфейс и первые реальные агенты с мониторингом CPU/RAM/системного раздела/сети/uptime; программный Linux RAID mdadm через `/proc/mdstat`. Аппаратные RAID-контроллеры, SNMP и Syslog находятся в разработке. Никаких сгенерированных данных «здоровых серверов» в интерфейсе нет.

![Storavix logo](internal/web/static/storavix-logo.webp)

## Быстрый старт: Linux без Docker

Из готового комплекта (в папке лежит готовый бинарный `storavix`):

```bash
sudo apt update && sudo apt install -y unzip
unzip Storavix-v0.2.0-complete-kit.zip
cd Storavix
sudo env STORAVIX_BINARY="$(pwd)/storavix" bash scripts/install-linux.sh
```

Из GitHub-исходников (нужен Go 1.23+):

```bash
git clone https://github.com/iPapik/Storavix.git
cd Storavix
sudo bash scripts/install-linux.sh
```

Скрипт показывает этапы установки, запрашивает логин/пароль администратора **от 8 символов** и открывает сервис на `0.0.0.0:8080`. В браузере используется *фактический адрес* сервера, например `http://192.168.1.20:8080`. Без TLS допускается только доверенная сеть.

## Быстрый старт: Windows

Распакуйте готовый комплект в `C:\Storavix`. Откройте PowerShell администратором:

```powershell
Set-ExecutionPolicy -Scope Process Bypass -Force
cd C:\Storavix
$env:STORAVIX_BINARY = "C:\Storavix\storavix.exe"
.\scripts\install-windows.ps1
```

## Агент: как подключить первый сервер

На **центральном Linux-сервере** скопируйте регистрационный токен:

```bash
sudo runuser -u storavix -- /usr/local/bin/storavix agent-token --data-dir /var/lib/storavix
```

На тестовом хосте Linux распакуйте тот же комплект и запустите:

```bash
sudo bash scripts/install-agent-linux.sh
```

Введите `http://IP-СЕРВЕРА:8080` и токен. На Windows-агенте откройте PowerShell администратором и выполните `scripts\install-agent-windows.ps1`. Через 30–60 секунд откройте раздел **Все системы**.

> Примечание: HTTP раскрывает токен сетевым наблюдателям. Для production установите HTTPS-прокси. Показатели RAID физических дисков Dell/HPE/LSI пока **не реализованы**, не путайте заполнение системного тома с исправностью RAID.

## Обновление прямо из GitHub — без ZIP и без привязки к каталогу

На **уже установленном Linux-сервере Storavix** выполните одну команду (в любом каталоге):

```bash
bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/iPapik/Storavix/main/scripts/update-online-linux.sh | sudo bash'
```

Обновлятор скачает чистые исходники с GitHub, установит временный компилятор Go при необходимости, соберёт новую версию, создаст резервную копию и проверит запуск. При отказе новой службы выполнит откат.

**Для всех следующих обновлений:**

```bash
sudo storavix-update
```

Сохраняются учётные записи, пароли, SMTP и данные в `/var/lib/storavix`. Подробности и разбор проблем: [обновление Linux/Windows](docs/UPDATE-RU.md). Windows пока обновляется отдельным PowerShell-скриптом; эта команда предназначена только для Linux с systemd. Агенты, установленные **на других серверах**, отдельно не обновляются.

## Документация

- [Установка агентов и получение токена](docs/AGENT-RU.md)
- [Обновление и откат](docs/UPDATE-RU.md)
- [Установка сервера Linux/Windows, SMTP, Docker и диагностика](docs/INSTALL-RU.md)

## Разработка

```bash
go test ./...
go vet ./...
go build ./cmd/storavix
go build ./cmd/storavix-agent
```

**Лицензия:** пока не задана; перед публичными сторонними вкладами необходимо выбрать файл LICENSE.


## Установка агентов из GitHub Releases (v0.2.1)

**Linux:** `curl -fsSL https://raw.githubusercontent.com/iPapik/Storavix/main/scripts/install-agent-online-linux.sh -o /tmp/storavix-agent-install.sh && sudo bash /tmp/storavix-agent-install.sh`

**Windows PowerShell (администратор):** `$p=Join-Path $env:TEMP 'storavix-agent-install.ps1'; Invoke-WebRequest 'https://raw.githubusercontent.com/iPapik/Storavix/main/scripts/install-agent-online-windows.ps1' -OutFile $p -UseBasicParsing; & $p`

Подробно: [AGENT-RU.md](docs/AGENT-RU.md). Готовые бинарники: [Releases](https://github.com/iPapik/Storavix/releases).

## Актуальная установка агентов (v0.2.2)

Windows PowerShell 5.1/7 от администратора:

```powershell
$p=Join-Path $env:TEMP 'storavix-agent-install.ps1'; Invoke-WebRequest 'https://raw.githubusercontent.com/iPapik/Storavix/main/scripts/install-agent-online-windows.ps1' -OutFile $p -UseBasicParsing; & $p
```

Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/iPapik/Storavix/main/scripts/install-agent-online-linux.sh -o /tmp/storavix-agent-install.sh && sudo bash /tmp/storavix-agent-install.sh
```

Обновление после установки: `sudo storavix-agent-update` (Linux), `storavix-agent-update` (новый PowerShell/CMD от администратора, Windows). В Windows новая версия исправляет проблему UTF-8 без BOM, которая приводила к ошибкам PowerShell 5.1. Подробнее: [инструкция по агентам](docs/AGENT-RU.md).
