# Установка Storavix Agent на Windows Server (v0.2.5)

**Для работающего агента нужно не только установить файлы, но и настроить подключение.** После ввода URL и токена обязательно должно появиться сообщение **Storavix Agent is running**, а в Планировщике заданий — задача `StoravixAgent` со статусом `Running`.

## 1. Скачать автономный EXE или MSI

Откройте https://github.com/iPapik/Storavix/releases/latest на ПК с доступом в интернет и скачайте **один** из файлов:

- `StoravixAgent-Setup-windows-amd64.exe` — обычный установщик с мастером.
- `StoravixAgent-windows-amd64.msi` — MSI для ручной установки или GPO.

Сверьте SHA-256 с `SHA256SUMS` из того же релиза. Перенесите файл на Windows Server через SMB, RDP либо USB. На конечном сервере **не нужен доступ к GitHub, Go или Docker**.

## 2. Получить токен

На центральном сервере Linux Storavix:

```bash
sudo runuser -u storavix -- /usr/local/bin/storavix agent-token --data-dir /var/lib/storavix
```

Храните выданный токен в секрете. На сервере с HTTP он передаётся без TLS, поэтому используйте HTTPS либо доверенную изолированную сеть.

## 3A. Установка EXE

1. Запустите `StoravixAgent-Setup-windows-amd64.exe` от имени администратора.
2. Пройдите мастер. После установки откроется **Storavix Agent - Initial configuration**.
3. Укажите URL с протоколом и портом, например `http://192.168.111.130:8080`.
4. Введите регистрационный токен.
5. Нажмите **Save and start** и дождитесь сообщения **Storavix Agent is running**.

## 3B. Установка MSI

1. Запустите `StoravixAgent-windows-amd64.msi` от имени администратора.
2. На последнем экране оставьте включённым пункт **Configure and start Storavix Agent** и завершите мастер.
3. Введите URL и токен; дождитесь подтверждения запуска задачи.
4. Если мастер не открылся, запустите из меню «Пуск» **Storavix Agent → Configure Storavix Agent** либо:

```powershell
& "$env:ProgramFiles\Storavix Agent\configure-agent.cmd"
```

Если настройка завершилась ошибкой, она отображается во всплывающем окне и записывается в `C:\ProgramData\StoravixAgent\setup.log`.

## 4. Проверка результата

Откройте PowerShell администратора:

```powershell
Get-ScheduledTask -TaskName StoravixAgent | Select-Object TaskName, State
Get-ScheduledTaskInfo -TaskName StoravixAgent | Select-Object LastRunTime, LastTaskResult
Get-Content "$env:ProgramData\StoravixAgent\setup.log" -Tail 40
Test-NetConnection 192.168.111.130 -Port 8080
```

Задача должна находиться в состоянии `Running`. Через 30–60 секунд компьютер должен появиться в разделе **Все системы** центральной панели. Если в логе есть `ERROR`, сохраните его и сообщите диагностическое сообщение.

## 5. Восстановление уже установленного MSI (без переустановки)

Если MSI установлен, но `StoravixAgent` отсутствует, запустите **от имени администратора**:

```powershell
& "$env:ProgramFiles\Storavix Agent\configure-agent.cmd"
```

В новой версии ошибки сохраняются и показываются, вместо закрытия окна без объяснений.

## 6. Тихая установка MSI / GPO

```powershell
msiexec /i "C:\Install\StoravixAgent-windows-amd64.msi" /qn /norestart /l*v "C:\Install\storavix-msi.log"
```

При `/qn` интерактивное окно не запускается, следовательно, для первой регистрации агента требуется отдельный защищённый этап настройки. Не указывайте токен в MSI-параметрах, в скриптах SYSVOL или открытых GPO. Альтернатива для заранее защищённого файла с токеном (пример под локальным администратором):

```powershell
& "$env:ProgramFiles\Storavix Agent\configure-agent-windows.ps1" -Console -ServerUrl "https://storavix.example.local" -RegistrationTokenFile "C:\Secure\storavix-agent-token.txt"
```

Файл с токеном должен быть доступен только администраторам и SYSTEM и удалён после настройки. Токен по умолчанию сохраняется в `C:\ProgramData\StoravixAgent\token` с ограниченным ACL.

## 7. Обновление

Скачайте новую версию с GitHub Releases на другом ПК, перенесите на сервер и запустите установщик **того же типа** (MSI или EXE). Конфигурация и ID агента лежат в `ProgramData`, поэтому сохраняются при обновлении.
