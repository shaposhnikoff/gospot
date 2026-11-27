# Мастер настройки GoSpot: установка на pistar3

Мастер доступен по HTTPS на `https://pistar3:8788/` и `https://192.168.210.194:8788/`. Порт слушает на всех интерфейсах; открывайте его только в доверенной сети управления. Существующая панель на `:8787` работает отдельно.

### Установка

Только координатор запускает скрипт после объединения кода мастера и `panel/helper/gospot-config`. Нужны Go на машине сборки, проверенный SSH-ключ узла `pistar3`, беспарольный `sudo` для установки, systemd ≥ 247 и OpenSSL на узле. Из каталога `panel/`:

```sh
ssh pistar3 'cat /proc/sys/kernel/random/boot_id; systemctl show mmdvmhost dmrgateway -p Id -p ActiveState -p MainPID -p InvocationID'
go test ./...
./deploy-wizard.sh
```

Повторите первую команду: идентификатор загрузки, PID и InvocationID радиослужб должны совпасть. Скрипт запускает только мастер, не вызывает `apply` или `rollback` и не меняет радиоконфигурацию.

Сервис работает под постоянной системной учётной записью `gospot-wizard` без входа. Он получает пароль и закрытый TLS-ключ через systemd `LoadCredential`. Через `sudo` ему разрешены только три точные команды: `/usr/local/libexec/gospot-config read`, `apply` и `rollback` (без дополнительных аргументов). Посмотреть фактические права: `ssh pistar3 'sudo -n -l -U gospot-wizard'`.

### Проверка сертификата и вход

Получите **публичный** сертификат через SSH с уже проверенным ключом узла. Сравните отпечаток SHA-256 с отпечатком в браузере до ввода пароля. Для проверки TLS из командной строки:

```sh
ssh -o StrictHostKeyChecking=yes pistar3 'cat /etc/gospot/wizard/tls-cert.pem' > pistar3.crt
openssl x509 -in pistar3.crt -noout -fingerprint -sha256 -dates -ext subjectAltName
curl --cacert pistar3.crt -sS -o /dev/null -w '%{http_code}\n' https://192.168.210.194:8788/
```

В SAN должны быть `DNS:pistar3` и `IP Address:192.168.210.194`. Код `401` для запроса без пароля совместим с успешной проверкой TLS. Не обходите проверку сертификата ключом `-k`.

Имя пользователя — `gospot` (в классическом Pi-Star было `pi-star`). Чтобы посмотреть пароль, выполните в личном терминале без записи сеанса:

```sh
ssh -o StrictHostKeyChecking=yes pistar3 'sudo -n cat /etc/gospot/wizard/admin-password'
```

Не помещайте пароль в URL, командную строку, журнал или отчёт. При повторной установке пароль и сертификат сохраняются.

### Откат

**Настройки радио.** Если нужно отменить ранее применённое изменение, используйте откат в мастере либо, при недоступности интерфейса, `ssh pistar3 'sudo -n /usr/local/libexec/gospot-config rollback'`. Это операция с рабочей конфигурацией; не используйте её для проверки установки.

**Установка мастера.** После неудачного обновления разверните предыдущую рабочую ревизию мастера и helper тем же скриптом. Чтобы удалить мастер после первой установки:

```sh
ssh pistar3 'sudo -n systemctl disable --now gospot-wizard.service && sudo -n rm -f /etc/sudoers.d/gospot-wizard /etc/systemd/system/gospot-wizard.service /usr/local/bin/gospot-wizard /usr/local/libexec/gospot-config && sudo -n systemctl daemon-reload'
```

Пароль и ключ остаются в `/etc/gospot/wizard/` с правами `root:root 0600`. При окончательном выводе мастера из эксплуатации удалите их по правилам хранения секретов узла. Удаление мастера само по себе не откатывает радионастройки.
