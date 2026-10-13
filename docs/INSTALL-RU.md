# Storavix: установка от А до Я

**Версия 0.1.1-dev.** Настоящая инструкция охватывает сервер и веб-интерфейс. RAID, SNMP, Syslog пока не собирают реальные показатели. В тестовом SMTP доступны сохранение настроек и тестовые письма.

## 1. Что изменилось и нужен ли Docker

**Docker для обычной установки не нужен.** На Linux Storavix устанавливается напрямую и запускается службой systemd. На Windows он ставится напрямую и автоматически запускается Планировщиком заданий. Docker — третий, дополнительный способ.

При первой установке мастер спрашивает логин администратора, пароль и повтор пароля. Данные хранятся отдельно от исполняемого файла, поэтому повторная установка сохраняет их. Storavix слушает все интерфейсы (0.0.0.0:8080) и выводит действительные IPv4-адреса для браузера. **0.0.0.0 не является адресом для браузера:** используйте, например, http://192.168.1.20:8080.

**Безопасность:** HTTP-передача логина и пароля не зашифрована. Разрешайте TCP 8080 только из доверенной локальной сети. Для доступа через Интернет или из недоверенной сети нужен HTTPS reverse proxy, VPN или SSH-туннель. Для стабильного адреса рекомендуется DHCP-резервация или DNS.

## 2. Установка на Debian или Ubuntu без Docker

### 2.1. Зайдите по SSH и установите зависимости

Войдите под пользователем с sudo или под root. Для Ubuntu/Debian выполните:

    sudo apt update
    sudo apt install -y git golang-go ca-certificates curl
    go version

Проверьте Go: необходима **версия 1.23 и новее**. Если ваш дистрибутив предлагает старый пакет, установите актуальную версию по https://go.dev/dl/. С готовым бинарником из GitHub Actions Go на целевом сервере не требуется.

### 2.2. Скачайте исходники

    cd ~
    git clone https://github.com/iPapik/Storavix.git
    cd Storavix

### 2.3. Запустите мастер установки

    sudo bash scripts/install-linux.sh

На вопрос о логине введите, например, operator (допустимы 3–64 латинских букв, цифр, точек, подчёркиваний и дефисов). На запрос пароля введите уникальный пароль длиной от 12 символов и повторите его. **Пароль при вводе не отображается.**

Мастер соберёт программу, создаст системного пользователя storavix, установит бинарник в /usr/local/bin/storavix, защитит каталог данных /var/lib/storavix, зарегистрирует storavix.service и выведет доступные адреса веб-интерфейса.

### 2.4. Проверка запуска

    sudo systemctl status storavix --no-pager
    curl -f http://127.0.0.1:8080/api/health

Ожидается JSON со статусом ok и версией. Если ответа нет, смотрите журналы:

    sudo journalctl -u storavix -n 100 --no-pager
    sudo journalctl -u storavix -f

Управление сервером:

    sudo systemctl restart storavix
    sudo systemctl stop storavix
    sudo systemctl start storavix

### 2.5. Доступ из локальной сети

Проверьте IPv4-адрес Linux:

    ip -4 addr

Если используется UFW, разрешите порт только своей подсети. Замените пример 192.168.1.0/24 на настоящий диапазон:

    sudo ufw allow from 192.168.1.0/24 to any port 8080 proto tcp
    sudo ufw status

В браузере другого компьютера откройте http://IP_СЕРВЕРА:8080, введите свой логин и пароль. Не используйте 127.0.0.1 с другого ПК — это его собственный адрес.

### 2.6. Без установленного Go

В GitHub откройте Actions → CI and builds → успешная сборка → Artifacts → storavix-linux-amd64. Распакуйте бинарный файл в каталог репозитория под именем storavix и запустите установщик. Для ARM64 используйте подходящий бинарник через переменную окружения STORAVIX_BINARY.

## 3. Установка на Windows и Windows Server без Docker

### 3.1. Подготовка

Откройте PowerShell **от имени администратора**. Установите Git с https://git-scm.com/download/win и Go 1.23+ с https://go.dev/dl/. Затем откройте новое окно PowerShell и проверьте go version. Если используется заранее собранный EXE из GitHub Actions, Go можно не устанавливать.

### 3.2. Скачивание проекта

    cd C:\
    git clone https://github.com/iPapik/Storavix.git
    cd C:\Storavix

Или скачайте Code → Download ZIP и распакуйте репозиторий в C:\Storavix так, чтобы были каталоги scripts, cmd и internal.

### 3.3. Установка и создание администратора

    Set-ExecutionPolicy -Scope Process Bypass -Force
    .\scripts\install-windows.ps1

Выберите логин, введите пароль от 12 символов, подтвердите пароль. Скрипт создаст C:\Program Files\Storavix\storavix.exe и защищённый каталог C:\ProgramData\Storavix. Он зарегистрирует задание Storavix для автозапуска **от SYSTEM при запуске Windows** и правило Windows Firewall (TCP 8080, только LocalSubnet, профили Domain/Private).

Важно: Windows-версия на этом этапе запускается через **Планировщик заданий**, а не как нативная служба в services.msc. Запуск этого сценария на реальном Windows Server всё ещё требует эксплуатационной проверки.

### 3.4. Проверка

    Get-ScheduledTask -TaskName Storavix
    Invoke-RestMethod http://127.0.0.1:8080/api/health
    Get-Content 'C:\ProgramData\Storavix\storavix.log' -Tail 60

Определить IPv4:

    Get-NetIPAddress -AddressFamily IPv4 | Select-Object IPAddress,InterfaceAlias

Управлять процессом:

    Stop-ScheduledTask -TaskName Storavix
    Start-ScheduledTask -TaskName Storavix

Затем откройте http://IP_СЕРВЕРА:8080 на компьютере в локальной сети. Для сети с несколькими подсетями может потребоваться отдельное правило межсетевого экрана вместо LocalSubnet.

## 4. Настройка SMTP и алертов

Зайдите в веб-интерфейс → Настройки → Почтовые уведомления. Укажите SMTP-сервер, порт, TLS, метод авторизации, отправителя и получателей. Сначала нажмите Сохранить настройки, затем Отправить тестовое письмо.

| Сервер | Порт | Шифрование | Авторизация |
| --- | --- | --- | --- |
| Локальный relay | 25 | none | none |
| SMTP submission | 587 | STARTTLS | PLAIN/LOGIN/CRAM-MD5 |
| SMTPS | 465 | TLS | PLAIN/LOGIN/CRAM-MD5 |
| SMTP с OAuth2 | 465/587 | TLS/STARTTLS | XOAUTH2 с готовым токеном |

Токен OAuth2 пока нужно получать и обновлять вручную. Не все почтовые системы разрешают парольную авторизацию. Для собственного CA укажите PEM-файл на сервере Storavix. Сейчас отправляются тестовые письма и тестовые события; реальные аварийные уведомления добавим после реализации RAID-сборщика и правил.

## 5. Docker — только дополнительная альтернатива

Если вы **осознанно** выбираете Docker, сначала установите Docker Engine и Docker Compose plugin по официальным инструкциям:

- Debian: https://docs.docker.com/engine/install/debian/
- Ubuntu: https://docs.docker.com/engine/install/ubuntu/
- Compose: https://docs.docker.com/compose/install/linux/

Убедитесь, что команды sudo docker version и sudo docker compose version работают. Затем:

    git clone https://github.com/iPapik/Storavix.git
    cd Storavix
    cp .env.example .env
    nano .env

В файле .env задайте STORAVIX_ADMIN_USER=operator, STORAVIX_ADMIN_PASSWORD=сложный_пароль. По умолчанию STORAVIX_BIND_IP=127.0.0.1, поэтому контейнер доступен с Docker-хоста. Если хотите LAN, укажите STORAVIX_BIND_IP=0.0.0.0 и ограничьте порт на хосте с учётом Docker firewall/DOCKER-USER (Docker может обходить обычные UFW-правила).

    chmod 600 .env
    sudo docker compose up -d --build
    sudo docker compose ps
    sudo docker compose logs -f storavix

Данные сохраняются в томе storavix-data. Не удаляйте его и не используйте compose down -v без резервной копии.

## 6. Где хранится конфигурация

| Вариант | Бинарник | Данные | Журнал |
| --- | --- | --- | --- |
| Linux | /usr/local/bin/storavix | /var/lib/storavix | journalctl -u storavix |
| Windows | C:\Program Files\Storavix\storavix.exe | C:\ProgramData\Storavix | C:\ProgramData\Storavix\storavix.log |
| Docker | контейнер | том storavix-data | docker compose logs |

Создавайте резервную копию state.json, instance.json и master.key **вместе**. SMTP-пароль зашифрован ключом master.key. Никогда не копируйте эти файлы в открытый GitHub.

## 7. Обновление без потери логина и пароля

**Linux:**

    cd ~/Storavix
    git pull --ff-only
    sudo bash scripts/install-linux.sh

**Windows:** обновите C:\Storavix из GitHub и снова запустите scripts\install-windows.ps1 как администратор. Повторная установка обнаружит существующий state.json и не перезапишет администратора или SMTP.

**Docker:** git pull --ff-only, затем sudo docker compose up -d --build. Не удаляйте том данных.

## 8. Если что-то не работает

- API локально не отвечает: проверьте службу, журнал и занят ли порт 8080.
- Браузер с другого ПК не открывает страницу: проверьте фактический IPv4, правила firewall и ACL маршрутизации. 0.0.0.0 — адрес прослушивания, не URL.
- SMTP не отправляет: проверьте DNS, исходящий порт, пароль/токен, TLS и доверие к CA.
- Сменить порт Linux: sudo /usr/local/bin/storavix configure --data-dir /var/lib/storavix --listen 0.0.0.0:8090, затем sudo systemctl restart storavix; не забудьте firewall.
- Забыли пароль: если ещё вошли в веб-панель, Настройки → Безопасность. Отдельный режим CLI-восстановления пока не реализован — не удаляйте каталог данных.

## 9. Ограничения текущей версии

Один локальный администратор (с заданным при установке логином). Нет LDAP/AD и многоуровневых ролей. Нет сбора аппаратных RAID/SMART/SNMP/Syslog и нет автоматических SMTP-уведомлений от оборудования. Веб-панель и SMTP-тесты работают. Все функции полноценного мониторинга разрабатываются по задачам репозитория.
