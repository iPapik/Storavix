# Storavix — руководство по установке от А до Я

**Версия:** 0.2.0-dev (сервер и первые агенты)  
**Репозиторий:** https://github.com/iPapik/Storavix  
**Цель:** открыть тёмную панель Storavix из браузера на компьютерах локальной сети, создать администратора, настроить почту и обеспечить автоматический запуск сервера.

> ВАЖНО: сервер и агент собирают базовые метрики CPU/RAM/сети/uptime, а на Linux — программный RAID mdadm. Аппаратные RAID, SNMP и Syslog ещё в разработке. Обновление существующей установки одной командой: [UPDATE-RU.md](UPDATE-RU.md).

## 0. Самый простой способ: готовый архив без Docker, Git и Go

Скачайте актуальный комплект Storavix с GitHub Actions/релиза, если опубликован готовый архив. Внутри уже лежат серверные бинарники для Windows x64 и Linux x64, установщики, исходный код и это руководство. **Go, Git и Docker на целевом сервере не требуются.**

**Linux:** скопируйте ZIP на сервер, затем (подставьте действительное расположение архива):

```bash
sudo apt update
sudo apt install -y unzip
unzip ~/Storavix-actual-complete-kit.zip -d ~/
cd ~/Storavix
sudo env STORAVIX_BINARY="$(pwd)/storavix" bash scripts/install-linux.sh
```

**Windows / Windows Server:** скопируйте ZIP на сервер, откройте PowerShell от имени администратора:

```powershell
Expand-Archive -Path "$env:USERPROFILE\Downloads\Storavix-actual-complete-kit.zip" -DestinationPath 'C:\' -Force
cd C:\Storavix
$env:STORAVIX_BINARY = 'C:\Storavix\storavix.exe'
Set-ExecutionPolicy -Scope Process Bypass -Force
.\scripts\install-windows.ps1
```

Во время установки нужно задать логин, пароль и подтверждение. После запуска выводятся реальные IPv4-адреса для веб-интерфейса. Для обновлений в дальнейшем предпочтительно использовать GitHub и проверенные релизы: архив является снимком конкретной версии.

---

## 1. Что потребуется

- **Сервер:** Linux с systemd (например Debian 12/13, Ubuntu Server 22.04/24.04/26.04) **или** Windows 10/11, Windows Server 2019/2022/2025. Скрипты ориентированы на современные x64-версии этих систем; сборка для других дистрибутивов требует проверки.
- **ОЗУ:** для этого первоначального веб-сервера достаточно небольшой виртуальной машины (ориентир — от 1 ГБ RAM), но конкретные требования будут пересмотрены после внедрения мониторинга.
- **Сеть:** статический IP или DHCP-резервация **рекомендованы**, но не требуются; доступ по TCP 8080 из доверенной локальной сети.
- **Источники установки:** GitHub и Go 1.23+ для сборки из исходного кода. Если у вас готовый бинарник Storavix из CI — Go на целевом сервере **не нужен**.
- **Docker не обязателен.** Основная инструкция ниже устанавливает Storavix напрямую в ОС и настраивает автозапуск.

### Как автоматически работает IP

Во время установки выставляется `0.0.0.0:8080`, что означает «слушать все сетевые интерфейсы». Это **не адрес для браузера**: установщик найдёт IPv4-адреса сервера и выведет настоящие ссылки, например `http://192.168.1.20:8080`.

Если у машины несколько сетевых карт, адресов может быть несколько: выберите тот, который доступен в вашей LAN. Если DHCP позже выдаст новый адрес, подключаться нужно будет по новому адресу; чтобы ссылка не менялась, создайте DHCP-резервацию или DNS-имя. Storavix не заменяет настройку статического IP в самой ОС.

## 2. Рекомендуемый путь: установка на Linux **без Docker**

Все команды в этом разделе выполняются на **том Linux-сервере, где должен работать Storavix**. Для Debian/Ubuntu сначала войдите через SSH под пользователем с `sudo`.

### 2.1. Обновить пакеты и поставить Git

```bash
sudo apt update
sudo apt install -y git ca-certificates curl
```

Если `sudo: command not found`, войдите под `root` и выполните команды без `sudo` либо настройте sudo.

### 2.2. Установить Go, если собираете из исходников

Попробуйте стандартный пакет:

```bash
sudo apt install -y golang-go
go version
```

**Проверка:** версия должна быть **1.23 или новее**. Если пакет вашего дистрибутива старее, установите актуальный Go по официальной инструкции https://go.dev/dl/ (для Linux x86-64 нужен архив `linux-amd64`, для ARM64 — `linux-arm64`). После установки снова проверьте `go version`. Наличие Docker для этого не требуется.

### 2.3. Скачать Storavix с GitHub

```bash
cd ~
git clone https://github.com/iPapik/Storavix.git
cd Storavix
```

Для **обновления уже установленной службы** лучше использовать единую команду из [UPDATE-RU.md](UPDATE-RU.md). Если нужны только исходники, выполните:

```bash
cd ~/Storavix
git pull --ff-only
```

### 2.4. Запустить мастер установки

```bash
sudo bash scripts/install-linux.sh
```

Установщик автоматически:

1. Соберёт сервер на Go или возьмёт готовый бинарник, если он лежит рядом с исходниками.
2. Скопирует программу в `/usr/local/bin/storavix`.
3. Создаст отдельного системного пользователя `storavix`.
4. Создаст защищённый каталог `/var/lib/storavix`.
5. **Спросит у вас логин администратора и пароль дважды** (пароль не отображается при вводе).
6. Сохранит защищённый хэш пароля, ключ шифрования и сетевой режим.
7. Создаст и запустит системную службу `storavix.service`.
8. Выведет адреса сервера для открытия панели из браузера.

Пример диалога:

```text
=== Storavix: установка без Docker ===
Придумайте логин администратора [admin]: operator
Придумайте пароль (от 12 символов):
Повторите пароль:
Storavix: создан администратор "operator".
Адрес для браузера: http://192.168.1.20:8080
```

**Требования к логину:** 3–64 символа; латинские буквы, цифры, `.`, `_`, `-`. Пароль: не менее 12 символов. Не используйте одинаковый пароль для разных систем.

Если Storavix уже был установлен, повторный запуск установщика **не сотрёт пароль или SMTP-конфигурацию**: существующие данные будут сохранены.

### 2.5. Убедиться, что служба работает

```bash
sudo systemctl status storavix --no-pager
curl -f http://127.0.0.1:8080/api/health
```

Ожидаемый результат API: JSON вида `{"status":"ok","version":"0.1.1-dev"}`. Посмотреть сообщения службы:

```bash
sudo journalctl -u storavix -n 100 --no-pager
sudo journalctl -u storavix -f
```

Команды управления:

```bash
sudo systemctl restart storavix
sudo systemctl stop storavix
sudo systemctl start storavix
sudo systemctl enable storavix
```

### 2.6. Разрешить соединения из локальной сети

На сервере узнайте адреса:

```bash
ip -4 addr show
```

Если включён UFW, разрешите TCP 8080 **только своей подсети** (пример, замените CIDR):

```bash
sudo ufw allow from 192.168.1.0/24 to any port 8080 proto tcp
sudo ufw status
```

Если используются `nftables`, `iptables`, внешний межсетевой экран или правила гипервизора, разрешите TCP 8080 там. Не открывайте порт в Интернет.

### 2.7. Войти через браузер

На другом ПК в той же сети откройте адрес вида:

```text
http://192.168.1.20:8080
```

Введите **логин и пароль, которые сами задали на шаге 2.4**. На главной странице появится тёмный интерфейс Storavix. Секции RAID/сети/Syslog пока пустые до установки сборщиков в следующих версиях.

## 3. Установка на Windows / Windows Server **без Docker**

### 3.1. Подготовить Windows

1. Войти под учётной записью с локальными административными правами.
2. Установить Git for Windows с https://git-scm.com/download/win или скачать исходники репозитория через **Code → Download ZIP**.
3. Если будете собирать программу из исходников, установить Go с https://go.dev/dl/ и открыть **новое** окно PowerShell.
4. Проверить компилятор:

```powershell
go version
```

Требуется **Go 1.23+**, если нет готового EXE. Можно скачать бинарник из **GitHub → Actions → CI and builds → успешный запуск → Artifacts → `storavix-windows-amd64`**. Положите файл `storavix-windows-amd64.exe` в папку `dist` внутри исходного репозитория, чтобы установщик использовал его без Go. Можно также переименовать его в `storavix.exe` и положить в корень скачанных исходников.

### 3.2. Скопировать исходники

Откройте PowerShell от имени администратора:

```powershell
cd C:\
git clone https://github.com/iPapik/Storavix.git
cd C:\Storavix
```

Если скачали ZIP с GitHub, распакуйте его в `C:\Storavix`, чтобы внутри находились `scripts`, `cmd`, `internal`, `README.md`.

### 3.3. Запустить установщик

```powershell
Set-ExecutionPolicy -Scope Process Bypass -Force
.\scripts\install-windows.ps1
```

Обход политики выполнения действует **только для текущего окна PowerShell**. Предварительно проверьте скачанный скрипт. Если политика организации запрещает запуск, согласуйте его с администратором безопасности.

Установщик:

1. Соберёт EXE (или использует готовый бинарник).
2. Установит `C:\Program Files\Storavix\storavix.exe`.
3. Создаст каталог `C:\ProgramData\Storavix` с ограниченными NTFS-правами.
4. Спросит **логин, пароль и повтор пароля**.
5. Настроит автозапуск в **Планировщике заданий** под встроенной учётной записью `SYSTEM`.
6. Добавит правило Windows Firewall: TCP 8080 **только из LocalSubnet** в профилях Domain/Private.
7. Запустит Storavix и выведет текущие IPv4-адреса.

Примечание: на этом этапе автозапуск Windows реализован **через Планировщик заданий**, а не как нативная служба Windows (`services.msc`). Для базовой версии это штатный автозапуск; нативную службу можно добавить позже.

### 3.4. Проверить запуск

```powershell
Get-ScheduledTask -TaskName Storavix
Invoke-RestMethod http://127.0.0.1:8080/api/health
Get-Content 'C:\ProgramData\Storavix\storavix.log' -Tail 60
```

Дополнительно посмотреть сетевые IP:

```powershell
Get-NetIPAddress -AddressFamily IPv4 |
  Where-Object {$_.IPAddress -ne '127.0.0.1' -and $_.IPAddress -notlike '169.254.*'} |
  Select-Object IPAddress,InterfaceAlias
```

В браузере локальной сети перейти на `http://IP-СЕРВЕРА:8080` и ввести созданный логин/пароль.

Управление:

```powershell
Stop-ScheduledTask -TaskName Storavix
Start-ScheduledTask -TaskName Storavix
Get-ScheduledTaskInfo -TaskName Storavix
```

> Учтите: Windows-пароль администратора не требуется вводить в Storavix, если вы создали отдельный логин. Пароль приложения независим от AD. В следующих версиях можно будет добавить LDAP/AD-интеграцию.

## 4. Первичная настройка панели после входа

1. Откройте вкладку **«Настройки»**.
2. В разделе **«Почтовые уведомления»** заполните SMTP: сервер, порт, режим безопасности, тип авторизации, отправитель, адреса получателей и пароль/токен.
3. Нажмите **«Сохранить настройки»**, затем **«Отправить тестовое письмо»**.
4. При необходимости выберите **«Создать тестовый алерт»** — он попадёт в журнал и попробует отправить письмо.
5. Во вкладке **«Безопасность»** можно изменить пароль администратора.

### Рекомендуемые настройки SMTP

| Почтовый сервер | Порт | Режим | Авторизация |
| --- | --- | --- | --- |
| SMTP-relay внутри LAN | 25 | Нет TLS | Нет |
| SMTP submission | 587 | STARTTLS | PLAIN/LOGIN/CRAM-MD5, в зависимости от сервера |
| SMTP SSL/TLS | 465 | TLS | PLAIN/LOGIN/CRAM-MD5, в зависимости от сервера |
| Сервер с готовым OAuth2 access token | 465/587 | TLS/STARTTLS | XOAUTH2 |

**Важные ограничения:** не все поставщики разрешают парольную SMTP-авторизацию; OAuth2 пока поддерживается только с вручную полученным токеном, без обновления срока действия. Сертификаты SMTP проверяются; если сервер использует собственный CA, укажите путь к доверенному PEM-файлу **на сервере Storavix**. Почтовые сообщения о настоящих отказах RAID пока не отправляются, так как сбор RAID-данных не реализован; работают тестовые отправки.

## 5. Альтернатива: Docker (не обязателен)

Docker — способ изолированного запуска в контейнере. **Для обычной установки Storavix он не нужен**, потому что выше есть прямая установка на Linux/Windows. Если хотите именно контейнер, сначала установите Docker Engine и Docker Compose plugin по официальным инструкциям:

- Debian: https://docs.docker.com/engine/install/debian/
- Ubuntu: https://docs.docker.com/engine/install/ubuntu/
- Compose plugin: https://docs.docker.com/compose/install/linux/

### 5.1. Установить Docker Engine и Compose, если они действительно нужны

Для чистого сервера **Debian или Ubuntu** можно установить Docker из официального APT-репозитория. Если Docker уже установлен, сначала ознакомьтесь с официальной документацией по совместимости пакетов — не удаляйте работающую инфраструктуру контейнеров вслепую.

```bash
sudo apt update
sudo apt install -y ca-certificates curl
. /etc/os-release
DISTRO="$ID"
CODENAME="${UBUNTU_CODENAME:-$VERSION_CODENAME}"
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL "https://download.docker.com/linux/${DISTRO}/gpg" -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
printf 'Types: deb\nURIs: https://download.docker.com/linux/%s\nSuites: %s\nComponents: stable\nArchitectures: %s\nSigned-By: /etc/apt/keyrings/docker.asc\n' "$DISTRO" "$CODENAME" "$(dpkg --print-architecture)" | sudo tee /etc/apt/sources.list.d/docker.sources
sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo systemctl enable --now docker
```

Эти команды предназначены только для Debian/Ubuntu: для других дистрибутивов потребуется инструкция их производителя. В некоторых системах предварительно приходится удалять конфликтующие пакеты `docker.io`, `containerd` и `runc` — проверьте наличие рабочих контейнеров перед изменением пакетов.

### 5.2. Запуск Storavix в Docker

После установки проверьте:

```bash
sudo docker version
sudo docker compose version
```

Затем:

```bash
git clone https://github.com/iPapik/Storavix.git
cd Storavix
cp .env.example .env
nano .env
```

Пример содержимого `.env`:

```dotenv
STORAVIX_ADMIN_USER=operator
STORAVIX_ADMIN_PASSWORD=замените_на_свой_сложный_пароль
STORAVIX_BIND_IP=127.0.0.1
STORAVIX_SECURE_COOKIES=false
```

Чтобы **явно** разрешить доступ по LAN, замените `STORAVIX_BIND_IP=127.0.0.1` на `STORAVIX_BIND_IP=0.0.0.0`, затем ограничьте доступ к порту правилами сетевого экрана на Docker-хосте/в `DOCKER-USER` (учтите, что публикация портов Docker может обходить обычные UFW-правила). Доступ к внешней сети защищайте через HTTPS reverse proxy.

```bash
chmod 600 .env
sudo docker compose up -d --build
sudo docker compose ps
sudo docker compose logs --tail=100 storavix
```

В Docker настройки входа задаются в `.env` **до первого запуска**, поэтому второй интерактивный мастер не нужен. После создания `state.json` изменение `.env` **не сменит** существующий пароль; используйте веб-страницу «Безопасность».

Остановить: `sudo docker compose down`. Данные продолжают храниться в Docker volume `storavix-data`. Не выполняйте `down -v`, если хотите сохранить данные.

## 6. Проверка сети и типичные ошибки

**Страница не открывается на другом ПК:**

- Проверьте локально API (`curl` или `Invoke-RestMethod`).
- Проверьте IPv4 сервера (`ip -4 addr` или `Get-NetIPAddress`).
- Проверьте межсетевой экран Windows/UFW/nftables, сетевые правила гипервизора и межсетевые ACL.
- Убедитесь, что открываете `http://РЕАЛЬНЫЙ-IP:8080`, а **не** `http://0.0.0.0:8080` или `http://127.0.0.1:8080` на другом ПК.
- Посмотрите журналы службы или `C:\ProgramData\Storavix\storavix.log`.

**Порт 8080 занят:**

Linux:

```bash
sudo ss -lntp | grep ':8080'
sudo /usr/local/bin/storavix configure --data-dir /var/lib/storavix --listen 0.0.0.0:8090
sudo systemctl restart storavix
```

Windows (PowerShell администратора):

```powershell
Get-NetTCPConnection -LocalPort 8080 -ErrorAction SilentlyContinue
& 'C:\Program Files\Storavix\storavix.exe' configure --data-dir 'C:\ProgramData\Storavix' --listen 0.0.0.0:8090
Stop-ScheduledTask -TaskName Storavix
Start-ScheduledTask -TaskName Storavix
```

Если меняете порт, отдельно обновите настройки сетевого экрана.

**Забыт логин/пароль:** не удаляйте `state.json` или `master.key`: там хранятся также SMTP-параметры и шифрование секретов. В версии 0.1.1-dev предусмотрена смена пароля через веб-интерфейс при наличии текущего пароля. Отдельный режим административного восстановления пока не реализован — потребуется согласованная процедура восстановления из резервной копии или доработка CLI.

**SMTP не проходит тест:** проверьте DNS, сетевую доступность исходящего порта, сервер/порт/режим (465 — обычно TLS, 587 — STARTTLS), правильность авторизации и доверие к CA. Смотрите ошибку в веб-интерфейсе и журнале.

## 7. Где хранятся данные и как обновляться

| Режим | Программа | Данные и настройки | Журналы |
| --- | --- | --- | --- |
| Linux напрямую | `/usr/local/bin/storavix` | `/var/lib/storavix` | `journalctl -u storavix` |
| Windows напрямую | `C:\Program Files\Storavix\storavix.exe` | `C:\ProgramData\Storavix` | `C:\ProgramData\Storavix\storavix.log` |
| Docker | внутри контейнера | Docker volume `storavix-data` | `docker compose logs` |

В данных находятся `state.json` (аккаунт и SMTP), `master.key` (ключ для расшифровки SMTP-секрета), `instance.json` (адрес прослушивания). **Копируйте вместе весь каталог данных и храните резервную копию вне GitHub, с ограниченным доступом.**

**Обновление Linux без удаления данных:**

```bash
cd ~/Storavix
git pull --ff-only
sudo bash scripts/install-linux.sh
```

**Обновление Windows:** обновите исходники/бинарник и снова запустите `install-windows.ps1` из PowerShell администратора. Скрипт обнаружит существующий каталог данных и не перезапишет учётную запись.

**Если установлен Docker:** `git pull --ff-only && sudo docker compose up -d --build` — volume сохраняется.

## 8. Безопасность — обязательно перед рабочей эксплуатацией

1. HTTP на 8080 допустим только в контролируемой LAN для предварительного тестирования. **Логины и пароли по HTTP передаются без шифрования.** Для доступа из недоверенных сегментов используйте HTTPS reverse proxy, VPN или SSH-туннель, не публикуйте 8080 напрямую.
2. Ограничивайте доступ к порту по подсетям и проверяйте разрешающие правила сетевых экранов.
3. Используйте отдельный длинный пароль Storavix и защищайте бэкап каталога данных.
4. При работе через HTTPS reverse proxy выставляйте `STORAVIX_SECURE_COOKIES=true` в окружении процесса Storavix. Не включайте его для обычного HTTP: браузер перестанет принимать cookie сессии.
5. Функции мониторинга железа, полноценные правила автоматических тревог, нативная служба Windows и управление несколькими пользователями запланированы, но не входят в эту сборку.

---

**Поддержка и исходники:** https://github.com/iPapik/Storavix  
**Задачи развития:** https://github.com/iPapik/Storavix/issues
