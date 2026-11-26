# Проверка записи через сохранённые формы PHP

```bash
cd /home/max/pi-star-new/Pi-Star_dash_new
root=$(npm run package:stock | sed -n 's/^Stock Pi-Star docroot.*: //p')
qa/classic-write/run.sh "$root"
```

Нужны Docker, Chromium/ChromeDriver, Python 3, socat и OpenSSL. Второй аргумент выбирает сценарий: `wifi`, `expert`, `backup` или `configure`. Без него выполняются Wi-Fi и Expert. Тест принимает только локальный собранный `build/release-*/docroot`; адрес рабочей станции или Raspberry он не принимает.

Настоящий PHP 8.2 исполняет **код из пакета**, Chromium отправляет формы с CSRF-токеном, `www-data` через `sudo` запускает разрешённые операции над копиями в `/etc` **внутри контейнера**. Каталог сайта подключён только для чтения, стенд доступен тестовому браузеру по loopback, сеть контейнера изолирована. Remount, переключение `wlan0` и рестарт `dmrgateway` только регистрируются в `/work/effects`. Работа с точкой доступа или радиосервером этим тестом не проверяется.

- **Wi-Fi:** в браузере добавляется WPA-профиль, проверяются новый ключ (PBKDF2), повторный GET, права `root:root 0600`, старый профиль, `freq_list`, `bssid`, `priority`, независимые проводные и DMR-настройки.
- **Expert:** в браузере редактируется `General/RptPort: 62032 → 62132`. После отдельного GET файл должен точно совпасть с входным, кроме этих пяти цифр. В фикстуре есть дублирующиеся строки `TGRewrite`, комментарии, кавычки и `Debug=off`.
- **`full-dmrgateway`:** в текстовом редакторе Full Edit изменить целевую группу второго `TGRewrite` с `92` на `310`. Сверяются полный POST с CSRF, новый GET, независимое чтение всех байтов `/etc/dmrgateway` с исходными переводами строк и порядок запросов перезапуска MMDVMHost → DMRGateway.
- **`full-wifi`:** в Expert → Full Edit → Wi-Fi изменить `priority=47` на `48` у первого из двух профилей. Проверяются POST с CSRF, новый GET, все байты файла, `root:root 0600` и журнал привилегированных команд: одна установка между `remount,rw` и `remount,ro`, без перезапуска служб и интерфейса. PSK и содержимое формы в протокол не выводятся.
- **`full-pistar-remote`:** в Expert → Full Edit → PiStar-Remote изменить `[keeper] callsign` с `N0DEMO` на `N0TEST`. Проверяются POST с CSRF, новый GET, все байты `/etc/pistar-remote` с исходными переводами строк, `root:root 0644`, одна установка и один запрос `restart pistar-remote.service` после возврата в read-only.
- **`full-rssidat`:** в Expert → Full Edit → RSSI.dat изменить калибровку для отсчёта `2000` с `-100` на `-103` dBm. Проверяются POST с CSRF, новый GET, все байты `/usr/local/etc/RSSI.dat`, `root:root 0644` и одна установка между remount rw/ro без запроса на перезапуск служб.
- **`full-cron`:** без демона cron изменить расписание одной тестовой записи `/bin/true` с `04:15` на `04:25`. Проверяются подтверждение `Crontab saved.`, POST с CSRF, новый GET, все байты `/etc/crontab`, `root:root 0644` и одна запись через `sed -i` между моделируемыми remount rw/ro без перезапуска служб.
- **`full-bmapikey`:** из Expert → API Keys открыть отдельный редактор BrandMeister. При изначально отсутствующем `/etc/bmapi.key` сохранить случайный фиктивный ключ. Проверяются POST с CSRF, новый GET, точные байты INI, `www-data:www-data 0600` и журнал команд. Значения ключа и CSRF в протокол не выводятся.
- **`full-dapnetapi`:** из Expert → API Keys открыть отдельный редактор DAPNET. Сохранить тестовые `USER`, `PASS`, `TRXAREA`; проверить три значения на новом GET, байты `/etc/dapnetapi.key`, владельца и права `www-data:www-data 0600`, единственную установку и отсутствие рестартов. Случайный пароль не выводится.
- **`full-tgifapikey`:** на фикстуре Pi-Star 4.3.9 открыть из API Keys отдельный редактор TGIF, сохранить случайный фиктивный токен в формате TGIFSTG1. Проверить POST с CSRF, чтение новым GET, все байты `/etc/tgifapi.key`, `www-data:www-data 0600` и единственный `install` без рестартов. Токен в протокол не выводится.
- **`full-apikeys`, `full-apikeys-bm`, `full-apikeys-dapnet`:** на общей странице с заполненными секциями BM, DAPNET и TGIF заменить соответственно TGIF-токен, BM-ключ или три поля DAPNET (`USER`, `PASS`, `TRXAREA`). В отдельном контейнере на сценарий проверить адрес и поля POST, сообщение об успехе, новый GET, точные байты и права целевого файла, неизменность двух соседних файлов и журнал команд. Все данные синтетические; секреты не выводятся.
- **`expert-nxdn`:** переход из Expert по ссылке NXDN GW, один Apply `Network/InactivityTimeout: 10 → 17`, новый GET, сравнение всего `/etc/nxdngateway`, `root:root 0644` и точного журнала с единственным запросом `restart nxdngateway.service`.
- **`expert-ysf`:** переход из Expert по ссылке YSF GW, один Apply `YSF Network/ReloadTime: 60 → 75`, новый GET и побайтовое сравнение `/etc/ysfgateway`, включая кавычки `Info/Description`. Проверяются `root:root 0644` и единственный запрос `restart ysfgateway.service`.
- **`expert-p25`:** через Expert → P25 GW изменить `Network/InactivityTimeout: 10 → 25`. После Apply проверить новый GET, побайтовое совпадение `/etc/p25gateway` с ожидаемой заменой (включая сохранность комментария к `HostsFile2`), `root:root 0644` и единственный запрос `restart p25gateway.service`.
- **`expert-dapnet`:** через Expert → DAPNET GW изменить `DAPNET/SuppressTimeWhenBusy: 1 → 0`. Проверить новый GET, побайтовое совпадение `/etc/dapnetgateway` с ожидаемой заменой, включая `AuthKey` и комментарий к `RptPort`, права `root:root 0644` и единственный запрос `restart dapnetgateway.service`.
- **`expert-mmdvmhost`:** через Expert → MMDVMHost изменить только `DMR/ColorCode: 3 → 12`. После Apply проверить новый GET, побайтовое совпадение `/etc/mmdvmhost` (включая обе секции AX.25), `root:root 0644` и единственный запрос `restart mmdvmhost.service`.
- **`expert-dstarrepeater`:** через Expert → DStarRepeater изменить только `beaconText: N0DEMO B → QA Beacon B`. Сверить новый GET и все байты плоского `/etc/dstarrepeater`, включая комментарий и отсутствие заголовка `[dstarrepeater]`. Проверить `root:root 0644` и единственный запрос `restart dstarrepeater.service`.
- **`expert-ircddbgateway`:** через Expert → ircDDBGateway изменить только `description1: Demo → North Pier`. Новый GET и побайтовая сверка `/etc/ircddbgateway` должны подтвердить изменение описания при сохранении `reflector1=NONE` и пробелов в конце `dplusLogin`. Журнал должен содержать только один запрос `restart ircddbgateway.service`.
- **`expert-timeserver`:** через Expert → TimeServer изменить `sendB: 1 → 0` кнопкой `Save Changes`. Новый GET должен показать `0`, а `/etc/timeserver` — отличаться ровно одним байтом при сохранении комментария, остальных строк и прав `root:root 0644`. В журнале должен быть один запрос `restart timeserver.service`.
- **`expert-starnetserver`:** на странице Expert найти StarNetServer через «Поиск функций», изменить только `ircddbUsername: N0DEMO → N0TEST` и нажать Apply. Проверяются CSRF, новый GET, все байты `/etc/starnetserver` (включая прежний `callsign` и комментарий), `root:root 0644` и единственный запрос `restart starnetserver.service`.
- **Сессия:** проверяется корреляция `GET → POST → GET` для одного PHP session ID и соответствие отправленного токена сессии. Данные Basic Auth, токены и пароли не включаются в отчёт. Стенд удаляется при выходе.

### Сохранение позывного (`admin/configure.php`)

`qa/classic-write/run.sh "$root" configure-modem` начинает с пустого маркера MMDVMHost и активной модели этой службы. Chromium принимает предупреждение, выбирает `ZUMspot - USB Stick` через видимый список модемов и нажимает Apply. На новом GET проверяются выбранная модель и отсутствие предупреждения, затем — байты маркера, `Port` и `UARTPort`, прочие файлы и журнал служб.

`qa/classic-write/run.sh "$root" configure-controller-first-default` удаляет оба маркера в фикстуре и моделирует обе службы неактивными. На первом GET MMDVMHost уже выбран. Chromium нажимает Apply без редактирования полей; проверяются полный POST, новый GET с переключателями MMDVM, единственный созданный маркер и единственный запуск `mmdvmhost.service`.

`qa/classic-write/run.sh "$root" configure-controller-first` начинает без обоих файлов-маркеров и без активного контроллера. Из первоначальной формы выбирается только D-Star Repeater. Проверяются свежий GET, создание единственного маркера, содержимое и метаданные остальных конфигов и команды служб. Аппаратный модем в этой проверке не выбирается.

`qa/classic-write/run.sh "$root" configure-language` меняет только `dashboardLanguage` (`english_uk` → `french_fr`). После полного Apply проверяются выбранный язык и четыре переведённые подписи на новом GET, точная замена присваивания в `config/language.php`, отсутствие иных файловых изменений и вызовов `systemctl`.

`qa/classic-write/run.sh "$root" configure-controller-reverse` начинает с маркера и активной модели службы D-Star Repeater. Chromium открывает форму D-Star, выбирает MMDVMHost и нажимает Apply. Тест сверяет новый GET, inode и байты маркера, остальное дерево конфигурации и точный порядок остановки старого и запуска нового контроллера.

`qa/classic-write/run.sh "$root" configure-controller` выбирает D-Star Repeater вместо MMDVMHost и принимает штатное предупреждение о необходимости выключить и включить питание. После Apply проверяются новый GET, оба маркера и байты настроек модема, остальное дерево конфигурации (включая сторонний `/etc/dstar-radio.qa-keep`) и порядок команд остановки старого и запуска нового контроллера. Это проверка запросов служб, не запуска радио или перезагрузки платы.

Для переключения действующей станции используется `sudo mv -nT -- /etc/dstar-radio.mmdvmhost /etc/dstar-radio.dstarrepeater` (при обратном переходе или откате — та же пара в обратном порядке). На узлах с ограниченным sudoers нужны именно эти две команды. Переключение с одновременным изменением параметров первоначальной настройки оборудования отклоняется до записи.

`qa/classic-write/run.sh "$root" configure-timezone` меняет только `System TimeZone`: `UTC` → `Europe/Berlin`. Сверяются новый GET, байты `/etc/timezone` и `config/config.php`, ссылка `/etc/localtime`, остальные настройки и точный журнал операций. Модель `timedatectl` меняет ссылку только внутри `/stand` на реальные данные `zoneinfo`; системные часы и D-Bus хоста не используются.

`qa/classic-write/run.sh "$root" configure-location` проверяет полный Apply: Chromium меняет только `Gateway Town` (`Demo` → `North Ridge`). Тест требует POST на `/admin/configure.php` без параметров выбора обработчика, свежий GET, восемь ожидаемых правок в семи файлах, сохранность остальных байтов и правил `TGRewrite0`. На `release-7ltLHq` тест упал; [на `release-c1zSEz` прошёл](../evidence/classic-configure-location-fixed.txt). Для формирования снимка исходных полей формы пакету нужен PHP DOM (`php-xml` в Debian).

`qa/classic-write/run.sh "$root" configure-color-zero` меняет **только** DMR Color Code с 3 на 0. Проверяются отправленные поля, свежий GET, байты всего `/etc/mmdvmhost` с единственной заменой `ColorCode=3` → `ColorCode=0` в `[DMR]`, неизменность `/etc/dmrgateway` с дублирующимися правилами и только один запрос состояния и рестарта `mmdvmhost.service`.

Для одновременной смены позывного и DMR Color Code запустите `qa/classic-write/run.sh "$root" configure-color`. Сценарий выбирает `3 → 12` в штатном списке, проверяет оба поля на новом GET и сравнивает обе строки `TGRewrite0` секции `[DMR Network 1]`, весь файл `/etc/dmrgateway` и журнал вызовов служб.

Запуск: `qa/classic-write/run.sh "$root" configure`. На одноразовой станции в режиме MMDVMHost Chromium открывает форму с Basic Auth, редактирует поле `confCallsign` (`N0DEMO` → `QA7TEST`) и нажимает штатную кнопку Apply. Новый GET выполняется с отключённым кэшем. У PHP и у независимой проверки одна закрытая копия файлов станции в `/stand`. Команды управления службами фиксируются в `/work/effects`; системный менеджер и радиооборудование хоста не используются.

`configure_oracle.py` независимо задаёт ожидаемые строки (включая D-Star-модуль и APRS-пароль) и сверяет конфиги с исходными байтами. В инвентаризацию входят `/etc`, `/root`, `/usr/local/etc`, `/usr/local/sbin`, `/usr/lib/systemd/system` и `config/` сайта. Повторная установка даже идентичного постороннего файла считается ошибкой. Ожидаются 14 запросов состояния, установка только файлов с изменениями и один запрос `restart mmdvmhost.service` после возврата в read-only. Для отдельной проверки обработки ошибок: `bash compat/classic-configure/transaction.test.sh`.

### Backup / Restore (`admin/config_backup.php`)

`qa/classic-write/run.sh "$root" backup` — отдельный сценарий для PHP `ZipArchive`, `fileinfo` и системной утилиты `zip`. В контейнере используется закрытая копия корневой ФС (`chroot`): даже `/etc/hosts`, `/etc/hostname` и два PHP-конфига являются обычными восстанавливаемыми файлами, а не файлами хоста или доступной на запись рабочей копией проекта. PHP исполняется как `www-data`.

Chromium отправляет форму скачивания, проверяет состав и CRC архива, а затем **тем же скачанным ZIP** восстанавливает три изменённых конфигурации. Проверяются HTTP-сессия/CSRF обеих операций, байты всех архивных файлов, сохранность контрольных файлов снаружи и инвентаризация остальных объектов в `/etc`, `/root`, `/usr/local/etc` и каталоге конфигов сайта. Временные файлы, cookies, сессии PHP и служебные логи не относятся к этому сравнению. Системные действия (управление питанием, службами и часовым поясом, перемонтирование) не выполняются на хосте; файловые операции внутри стенда — настоящие.
# Bookworm: native service INIs from the classic menus

`bash qa/classic-write/run.sh build/release-ctX2Mh/docroot menu-bookworm-dmr`
runs both actions on one disposable Debian 12 station. `bookworm/pistar3-sanitized.json`
retains the pilot's section/key layout, initial Color Code and numeric TG rules;
other values and comments are synthetic. To recapture, run
`ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes pistar3 'test "$(hostname -s)" = pistar3 && sudo -n python3 -' < qa/classic-write/bookworm/capture.py > /tmp/opencode/pistar3-bookworm-sanitized.json`
and pass the path via `QA_BOOKWORM_SNAPSHOT`. The capture does not export live credentials.

The packaged PHP and operator assets are unchanged. The fixture connector regenerates
the Color Code on a classic-form template and the complete Expert gateway read view
on entry; it hands only the selected DMR field/rule change to the repository's
`pistar-config services-apply` with a revision check. Only that helper commits the
native INIs. The oracle reads both fixed `ExecStart` paths as `mmdvm`, not the PHP
views, after each menu reopen. Service commands are recorded inside the chroot.
The connector models these two operations, not a general PHP/Bookworm adapter.
The installed pilot helper lacks `services-read` and `services-apply`.
