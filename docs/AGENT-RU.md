# Storavix Agent v0.2.0 — установка и подключение

> Статус: первый рабочий MVP. CPU, RAM, заполнение системного тома, скорость сети, uptime и load average на Linux. `/proc/mdstat` используется для **программного Linux RAID mdadm**. Статусы аппаратных Dell PERC/LSI MegaRAID/HPE Smart Array, SMART, GPU, службы, обновления и температура пока **не собираются**. Если данных нет — в интерфейсе отображается «—» или «нет данных», а не «исправно».

## 1. Получить регистрационный токен на центральном сервере

**Linux сервер Storavix** (на сервере, где установлен веб-интерфейс):

```bash
sudo runuser -u storavix -- /usr/local/bin/storavix agent-token --data-dir /var/lib/storavix
```

**Windows сервер Storavix** — PowerShell администратора:

```powershell
& "$env:ProgramFiles\Storavix\storavix.exe" agent-token --data-dir "$env:ProgramData\Storavix"
```

Команда выведет токен длиной 64 шестнадцатеричных символа. Он одинаков для всех агентов этого экземпляра в данном MVP и является секретом: не публикуйте его в GitHub или чатах. Если токен утёк, потребуется заменить `agent.token` на сервере и заново настроить агенты (автоматическая ротация пока не реализована).

**Подключение через Интернет и сегменты с недоверенным трафиком:** обязательно разместите Storavix за HTTPS-прокси и используйте `https://` в агенте. Работа по HTTP разрешена только с явным флагом `--allow-http` для полностью доверенной лабораторной сети, потому что токен иначе передаётся открыто.

## 2. Установка агента на Debian/Ubuntu

Подготовьте исходники или готовый комплект с бинарником `storavix-agent`:

```bash
sudo apt update
sudo apt install -y unzip curl
unzip Storavix-v0.2.0-complete-kit.zip
cd Storavix
sudo bash scripts/install-agent-linux.sh
```

Установщик последовательно показывает стадии, спрашивает URL сервера, затем регистрационный токен. После ввода создаются учётная запись `storavix-agent`, служба `storavix-agent.service`, файл токена `/etc/storavix-agent/token` и постоянный идентификатор в `/var/lib/storavix-agent/agent.id`.

Важные команды:

```bash
sudo systemctl status storavix-agent --no-pager
sudo journalctl -u storavix-agent -n 70 --no-pager
sudo systemctl restart storavix-agent
```

## 3. Установка на Windows Server

Распакуйте полный комплект, запустите PowerShell от имени администратора:

```powershell
Set-ExecutionPolicy -Scope Process Bypass -Force
cd C:\Storavix
.\scripts\install-agent-windows.ps1
Get-ScheduledTask -TaskName StoravixAgent
```

Вопросы аналогичны Linux: URL и токен. Задание `StoravixAgent` работает от SYSTEM, настройки расположены в `C:\ProgramData\StoravixAgent` с ограниченными ACL.

## 4. Проверка передачи данных

Откройте веб-панель: **Все системы**. Через 30–60 секунд после запуска появятся карточки реальных агентов. В карточках видны CPU, RAM, заполнение системного раздела, сетевой обмен и uptime. Первый CPU/сетевой замер может отсутствовать до второго опроса — для вычисления скорости нужен интервал.

Важно: процент строки **«Диск»** означает занятую ёмкость системного раздела, а не состояние накопителя или RAID.

Если агент не появился: проверьте время на компьютерах, доступность TCP-порта сервера, токен, логи агента, корректность URL. На HTTP-сервере при ручном запуске агент требует `--allow-http`.

## 5. Обновление агента

Запустите `install-agent-linux.sh` либо `install-agent-windows.ps1` из **нового комплекта** на хосте, где уже работает агент. Скрипт заменит бинарник и перезапустит задачу/службу, сохранив ID, URL и токен.

Это локальное обновление хоста; централизованная массовая раскатка пока не реализована. Для крупного парка её можно выполнять с помощью Ansible/GPO после тестов.
