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

Windows-агент также доступен в MSI и Setup EXE в GitHub Releases. PowerShell-установщик скачивает готовый агент автоматически.

## Шаг 3. Проверь на панели

Открой **Все системы** на центральном Storavix. Новое устройство появится после отправки первых показателей, обычно за 30–60 секунд. Карточка сервера показывает ЦП, оперативную память, загрузку файловых систем, скорость сети и время работы. Средняя нагрузка доступна на Linux. В разделе **RAID и диски** доступны контроллеры, логические массивы, физические диски, их привязка и время опроса.

### Источники RAID

| Хранилище | Источник |
| --- | --- |
| Linux MD | Встроенный сбор через `/proc/mdstat` и sysfs |
| Windows Storage Spaces | Встроенные команды модуля Storage, состав каждого тома через `Get-PhysicalDisk -VirtualDisk` |
| Broadcom/LSI MegaRAID, Dell PERC | Установленные StorCLI, StorCLI2, PercCLI или PercCLI2, подходящие для данного контроллера |
| HPE Smart Array | Установленный HPE SSA CLI (`ssacli` или `hpssacli`) |

Агент выполняет команды инвентаризации `show`. Windows-задача запускается от SYSTEM. На Linux аппаратные контроллеры опрашивает `storavix-agent-storage.timer` через локальную root-службу, а сетевой агент читает её отчёт под служебной учётной записью.

При отсутствии ответа от RAID CLI агент показывает контроллер, который обнаружила ОС. Точная модель зависит от драйвера и базы PCI, а при отсутствии названия виден PCI ID. Состояние контроллера отмечается как неизвестное. Состав аппаратного массива, состояние и физические диски появятся, когда подходящая утилита вернёт сведения о них.

### Автоматическая установка RAID CLI

Один раз возьмите подходящую для модели и ОС утилиту у производителя. Извлеките исполняемый файл, например `storcli64.exe` для Windows или `storcli64` для Linux. Разместите его во внутреннем HTTPS-репозитории или на доступной для чтения файловой шаре Windows. Рассчитайте SHA-256 **исполняемого файла** командой `Get-FileHash -Algorithm SHA256` или `sha256sum`. Для разнородного парка используйте свой файл и хеш для каждого семейства, ОС и архитектуры.

При запуске онлайн-установщика или обновления задайте три переменные:

| Переменная | Значение |
| --- | --- |
| `STORAVIX_RAID_TOOL` | `storcli`, `storcli2`, `perccli`, `perccli2` или `ssacli` |
| `STORAVIX_RAID_TOOL_SOURCE` | HTTPS URL или абсолютный путь к исполняемому файлу. В Windows поддерживается UNC-путь |
| `STORAVIX_RAID_TOOL_SHA256` | SHA-256 опубликованного файла |

Установщик выбирает хосты по семейству обнаруженного контроллера. Для Dell PERC указывайте PercCLI/PercCLI2, а для Broadcom MegaRAID — StorCLI/StorCLI2, совместимый с моделью. Хост с уже установленным файлом с тем же хешем пропустит загрузку. Установка агента продолжается, если RAID CLI получить не удалось. На GPO Startup Script дайте право чтения шары компьютерам, от имени которых запускается скрипт.

Если агент уже обновлён, ту же установку можно запустить из GPO, Ansible или вручную. Команда Windows берёт исполняемый файл из установленной задачи, поэтому подходит и для legacy-сборки на Windows Server 2012/R2. Для обновления агента на этих ОС используйте MSI/EXE из релиза.

```powershell
$source = '\\fileserver\RAID\windows\storcli64.exe'
$sha256 = '<SHA-256 утверждённого файла>'
$agent = (Get-ScheduledTask -TaskName StoravixAgent).Actions[0].Execute
& $agent --install-raid-tool storcli --raid-tool-source $source --raid-tool-sha256 $sha256 --raid-tool-if-detected
```

```bash
sudo storavix-agent --install-raid-tool storcli \
  --raid-tool-source /mnt/raid-tools/linux/storcli64 \
  --raid-tool-sha256 '<SHA-256 утверждённого файла>' --raid-tool-if-detected
```

Подготовленные утилиты сохраняются в `%ProgramFiles%\Storavix Agent\raid-tools` или `/opt/storavix-agent/raid-tools` и подхватываются при следующем опросе. Агент проверяет SHA-256 и целевую архитектуру и не запускает скрипты из пакетов. Если утилите нужны дополнительные библиотеки или служба, установите полный пакет производителя обычным способом.

### Путь к ранее установленной утилите

На Windows для CLI, распакованной в нестандартный каталог, задайте **путь к exe** от администратора. Пример для MegaRAID:

```powershell
[Environment]::SetEnvironmentVariable('STORAVIX_MEGA_CLI', 'C:\Program Files\StorCLI\storcli64.exe', 'Machine')
```

Подставьте уже установленный путь. Для HPE используется `STORAVIX_HPE_CLI`. Храните утилиту в каталоге с правом записи для администраторов и SYSTEM. Агент перечитывает системное значение при очередном опросе.

На Linux, если программа установлена вне стандартного каталога, задайте её абсолютный путь в `sudo systemctl edit storavix-agent-storage.service`:

```ini
[Service]
Environment="STORAVIX_MEGA_CLI=/opt/MegaRAID/storcli/storcli64"
```

Примените настройку командой `sudo systemctl start storavix-agent-storage.service`. Журнал локального опроса: `sudo journalctl -u storavix-agent-storage -n 30 --no-pager`.

Состав, серийные номера, температура и процент восстановления зависят от данных выбранного источника. Виртуальная машина обычно видит виртуальные диски. Для физических массивов установите агент и утилиту на гипервизоре с доступом к контроллеру. В источниках опроса отдельно видны ошибки и недоступные утилиты.

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


## Исправление v0.2.3: ошибка RuntimeInformation на Windows

В некоторых версиях Windows PowerShell 5.1 свойство `[System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture` возвращало `null`, поэтому онлайн-установка завершалась ошибкой `InvokeMethodOnNull`. Загрузчик v0.2.3 использует переменные окружения Windows и CIM-резервный метод, не требуя нового PowerShell.

В PowerShell администратора повтори установку, чтобы заново скачать **обновлённый скрипт из main**:

```powershell
$p=Join-Path $env:TEMP 'storavix-agent-install.ps1'
Invoke-WebRequest 'https://raw.githubusercontent.com/iPapik/Storavix/main/scripts/install-agent-online-windows.ps1' -OutFile $p -UseBasicParsing
& $p
```

Для проверки архитектуры до установки можно запустить `& $p -ArchitectureOnly`: ожидается `amd64` или `arm64`. Сохранённые URL, токен и ID агента при повторной установке не стираются.


## Windows MSI / EXE offline (v0.2.4)

For a Windows Server that cannot resolve release-assets.githubusercontent.com, download the complete offline installer on another PC and transfer it over SMB/RDP/USB. Both contain the agent itself and require no Go, Docker or GitHub connection on the monitored host. See [Windows installation guide](WINDOWS-INSTALL-RU.md).

- StoravixAgent-windows-amd64.msi: standard MSI; after installation launch Storavix Agent / Configure Storavix Agent from the Start menu. Silent /qn installs do not launch the interactive wizard.
- StoravixAgent-Setup-windows-amd64.exe: graphical Inno Setup wizard followed by connection configuration.

The wizard saves token/URL under restricted ProgramData and keeps agent identity on reinstall.


## Удаление старого устройства из панели (v0.2.6)

Войди в Storavix под администратором, открой **Все системы**, выбери сервер, нажми **Удалить устройство**, подтверди. Запись исчезнет с панели и не появится снова с прежним ID, даже если старый агент продолжает отправлять данные. Это не удаляет установленный агент с компьютера.

Чтобы **осознанно вернуть удалённый сервер**, останови агент на целевой ОС, удали только файл `agent.id` из каталога данных, затем снова запусти агент. Его токен и адрес сервера оставь без изменений.

**Linux:**
```bash
sudo systemctl stop storavix-agent
sudo rm -f /var/lib/storavix-agent/agent.id
sudo systemctl start storavix-agent
```

**Windows (PowerShell администратора):**
```powershell
Stop-ScheduledTask -TaskName StoravixAgent
Remove-Item -LiteralPath "$env:ProgramData\StoravixAgent\agent.id" -Force -ErrorAction SilentlyContinue
Start-ScheduledTask -TaskName StoravixAgent
```

Если сервер навсегда выведен из эксплуатации, агент на нём нужно отдельно остановить или удалить; удаление карточки не выключает программу на удалённом сервере.
