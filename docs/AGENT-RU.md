# Storavix Agent — установка на Windows и Linux

Агенты устанавливаются **на конечные серверы**, не на центральную панель (кроме случаев, когда нужно мониторить и сам сервер Storavix). Go и Docker на конечных серверах не нужны.

## Шаг 1. Получи токен на центральном Storavix

**Linux (центральный сервер):**
```bash
sudo runuser -u storavix -- /usr/local/bin/storavix agent-token --data-dir /var/lib/storavix
```

**Windows (если центральная панель работает в Windows):**
```powershell
& "$env:ProgramFiles\Storavix\storavix.exe" agent-token --data-dir "$env:ProgramData\Storavix"
```

Токен содержит секрет авторизации и используется агентами данного экземпляра; не публикуй его. Для подключения потребуется IP или HTTPS-адрес центральной панели.

## Шаг 2A. Linux

На **мониторируемом сервере** (Ubuntu/Debian, systemd) установи curl, если его нет:

```bash
sudo apt update && sudo apt install -y curl ca-certificates
```

**Одна команда установки или обновления:**

```bash
curl -fsSL https://raw.githubusercontent.com/iPapik/Storavix/main/scripts/install-agent-online-linux.sh -o /tmp/storavix-agent-install.sh && sudo bash /tmp/storavix-agent-install.sh
```

Программа определит AMD64/ARM64, скачает `storavix-agent-linux-*.tar.gz` и `SHA256SUMS` из GitHub Releases и проверит файл. Затем спросит (при первой установке) URL сервера и регистрационный токен, установит бинарник в `/usr/local/bin/storavix-agent` и зарегистрирует службу `storavix-agent`.

**Проверка:**

```bash
sudo systemctl status storavix-agent --no-pager
sudo journalctl -u storavix-agent -n 60 --no-pager
```

## Шаг 2B. Windows

Открой PowerShell **от имени администратора** на конечном Windows-сервере. Выполни одной строкой:

```powershell
$p=Join-Path $env:TEMP 'storavix-agent-install.ps1'; Invoke-WebRequest 'https://raw.githubusercontent.com/iPapik/Storavix/main/scripts/install-agent-online-windows.ps1' -OutFile $p -UseBasicParsing; & $p
```

Установщик скачает `storavix-agent-windows-amd64.zip` (либо ARM64), проверит SHA-256, спросит URL сервера и токен, положит `storavix-agent.exe` в `C:\Program Files\Storavix Agent` и создаст задачу автозапуска `StoravixAgent` от SYSTEM. Данные и токен сохраняются в `C:\ProgramData\StoravixAgent`.

**Проверка:**

```powershell
Get-ScheduledTask -TaskName StoravixAgent
```

Windows EXE пока не подписан сертификатом разработчика — SmartScreen может предупредить. Отдельного MSI пока нет; PowerShell устанавливает готовый EXE автоматически.

## Шаг 3. Проверь на панели

Открой **Все системы** на центральном Storavix. Новое устройство появится после отправки первых показателей, обычно за 30–60 секунд. Отсутствие RAID/SMART показателей не является показателем здоровья дисков — аппаратные адаптеры будут добавлены позднее.

## Обновление

Повтори эту же команду на нужном устройстве. Существующие токен, ID агента и URL сервера сохраняются. По умолчанию скачивается последний GitHub Release; для закрепления определённой версии задай переменную окружения `STORAVIX_RELEASE_TAG=v0.2.1` до запуска.

**Безопасность:** для подключения через недоверенные сети используй HTTPS. При подключении по HTTP токен передаётся открыто — это допускается только в изолированной доверенной LAN.

[Все релизы и пакеты](https://github.com/iPapik/Storavix/releases)

## Исправления v0.2.2 — PowerShell и обновление одной командой

В версии v0.2.1 Windows PowerShell 5.1 неверно читал UTF-8 скрипты с русским текстом (символы вида `Р—Р°...`). Теперь оба установочных скрипта совместимы с ANSI/ASCII и проверяются парсером Windows PowerShell 5.1 в CI.

**Linux:** один раз установите агент командой выше, затем для каждого обновления достаточно:

```bash
sudo storavix-agent-update
```

**Windows:** установите из PowerShell администратора командой выше. Для следующего обновления откройте **новое** окно PowerShell/CMD от администратора и выполните:

```powershell
storavix-agent-update
```

Если PATH ещё не обновился в текущем сеансе:

```powershell
& "$env:ProgramFiles\Storavix Agent\storavix-agent-update.cmd"
```

Обе команды загружают последний опубликованный Release и сохраняют прежние токен, URL и ID агента. EXE сам по себе **не является интерактивным установщиком**: он запускается как фоновая задача `StoravixAgent`.
