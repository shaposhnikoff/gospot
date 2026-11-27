# Установка GoSpot

English version: [INSTALL.md](INSTALL.md) · Обзор: [../README.ru.md](../README.ru.md)

Есть два пути:

1. **[Записать образ GoSpot](#1-запись-образа)** — готовая система Raspberry Pi
   OS Lite Trixie (Debian 13) armhf, оба пакета уже установлены.
2. **[Поставить два пакета](#2-установка-пакетов-на-готовую-систему)** на уже
   работающую Raspberry Pi OS (Bookworm или Trixie), в том числе на
   существующую плату Pi-Star.

Дальше пройдите **[чек-лист после установки](#3-после-установки)**.

Любой путь ниже — это настройка из файла
[`Pi-Star_dash_new/gospot.conf`](../Pi-Star_dash_new/gospot.conf), на плате это
`/etc/gospot/gospot.conf`. Если вы меняли настройку, подставляйте своё
значение, а не напечатанное здесь по умолчанию.

---

## 1. Запись образа

### Где лежит файл образа

Образ собирается только локально; требования к машине и запуск
`./image/build.sh` описаны в [`image/README.md`](../image/README.md). Каждый
запуск создаёт новый каталог внутри `IMAGE_BUILD_PARENT` (по умолчанию
`/var/tmp`), вне репозитория:

```
/var/tmp/gospot-image.XXXXXXXXXX/pi-gen/deploy/
├── image_<дата>-gospot-trixie-armhf.img.xz           # сам образ
├── image_<дата>-gospot-trixie-armhf.img.xz.sha256    # контрольная сумма
└── image_<дата>-gospot-trixie-armhf.manifest         # коммит pi-gen, коммит исходников, версии пакетов
```

`gospot-trixie-armhf` — это `IMAGE_NAME`, `<дата>` подставляет pi-gen. В релизе
лежат те же три файла. Перед записью сверьте сумму:

```sh
cd /var/tmp/gospot-image.*/pi-gen/deploy
sha256sum -c image_*-gospot-trixie-armhf.img.xz.sha256
```

Храните `.manifest` и `.sha256` вместе с образом: закреплённые коммиты
фиксируют входные данные сборки, но пакеты Raspberry Pi OS, ядро и прошивка
берутся из живого архива, поэтому две сборки одних и тех же исходников не
совпадают побайтово.

### Запись на карту

**Raspberry Pi Imager** — *Choose OS* → *Use custom*, выбрать файл `.img.xz`,
выбрать карту, записать. Экран настройки Imager (имя узла, пользователь,
Wi-Fi, ключ SSH) работает: в образе оставлен cloud-init из Raspberry Pi OS.
Если ничего не настраивать, применятся значения самого образа.

**`dd`** — вместо `/dev/sdX` подставьте устройство карты и проверьте его
дважды (`lsblk`): `dd` ничего не переспросит.

```sh
xz -dc image_<дата>-gospot-trixie-armhf.img.xz \
  | sudo dd of=/dev/sdX bs=4M conv=fsync oflag=direct status=progress
sync
```

### Первая загрузка

Ни один секрет не общий у плат, записанных с одного образа. При первой загрузке,
до запуска службы администрирования:

- **Ключи хоста SSH** генерирует сама Raspberry Pi OS (в образе их нет).
- **`gospot-firstboot.service`** (`/usr/local/sbin/gospot-firstboot`) удаляет
  всё из `/etc/gospot/wizard`, затем создаёт приватный ключ TLS и
  самоподписанный сертификат именно этой платы (`CN=gospot`, в SAN — имя узла и
  текущие адреса) и задаёт заводской пароль веб-админки. Маркер
  `/var/lib/gospot-firstboot/done` пишется последним, поэтому прерванная первая
  загрузка просто повторится при следующей.
- Запускаются службы панели (`gospot-panel`, `gospot-wizard`, `gospot-stage`) и
  суточный таймер справочников `gospot-hostfiles.timer`. **Радиоюниты остаются
  выключенными.**

Через минуту-две:

| Куда | Адрес | Пользователь | Пароль |
| --- | --- | --- | --- |
| SSH и консоль | `gospot.local` | `gospot` | `raspberry` |
| Операторская панель | `http://gospot.local:8789/` | — | — |
| Настенное табло | `http://gospot.local:8789/kiosk` | — | — |
| Админка | `https://gospot.local:8788/manage/` | `gospot` | `raspberry` |

Если mDNS в вашей сети не работает, подставьте IP-адрес платы вместо
`gospot.local`.

```sh
ssh gospot@gospot.local        # пароль: raspberry
```

Сертификат админки самоподписанный, браузер предупредит. Сверьте отпечаток на
самой плате, прежде чем доверять:

```sh
openssl x509 -in /etc/gospot/wizard/tls-cert.pem -noout -fingerprint -sha256
```

### Сразу смените пароль

Откройте **Configuration → Remote Access Password**, введите пароль и нажмите
*Set Password*. Одна форма меняет и пароль веб-админки, и пароль системной
учётной записи `gospot`. Заводской `raspberry` не принимается, как и в
Pi-Star; HTTPS-служба перезапустится через пару секунд, и браузер спросит
новые данные.

### Включите радиослужбы

Радиоюниты специально поставлены выключенными: ненастроенный хотспот не должен
выходить в эфир. Включайте только нужные режимы, уже задав позывной и частоту:

```sh
sudo systemctl enable --now mmdvmhost.service     # многорежимный контроллер
sudo systemctl enable --now dmrgateway.service    # сети DMR (BrandMeister, DMR+, ...)
sudo systemctl enable --now ysfgateway.service    # и p25gateway, nxdngateway, m17gateway, ...
systemctl status mmdvmhost.service
```

Включённые юниты страница Configuration дальше перезапускает сама.

### Модем и UART

Образ уже подготовил последовательный порт GPIO для модема-платы:
`enable_uart=1` в `/boot/firmware/config.txt`, в
`/boot/firmware/cmdline.txt` нет `console=serial0`, а
`serial-getty@ttyAMA0.service` и `serial-getty@serial0.service` замаскированы.
Для MMDVM_HS или ZUMspot больше ничего не нужно — только выбрать верный
**Radio/Modem Type** в Configuration, который заодно задаёт порт
(`/dev/ttyAMA0` для платы на GPIO, `/dev/ttyUSB0` или `/dev/ttyACM0` для
USB-модема). `sudo gospot-findmodem` покажет, что он нашёл.

---

## 2. Установка пакетов на готовую систему

Raspberry Pi OS Bookworm или Trixie, armhf, Raspberry Pi 2 Model B или новее.
Скопируйте оба файла `.deb` на плату и установите их одной командой, чтобы
`apt` разрешил зависимости из архива Raspberry Pi OS:

```sh
sudo apt update
sudo apt install ./gospot_0.2.0-1_armhf.deb ./gospot-radio_0.2.0-1_armhf.deb
```

Именно одной командой: `gospot-radio` создаёт раскладку, которую настраивает
`gospot`, а на плате Pi-Star одна транзакция сразу заменяет оба старых пакета.

### Что создают пакеты

**`gospot`** (панель):

- `/etc/gospot/gospot.conf` — файл настроек, conffile dpkg: ваши правки
  переживают обновление. Пакет с новыми настройками допишет их в изменённую
  копию, не трогая существующие строки. После правки вручную перезапустите
  `gospot-panel`, `gospot-wizard` и `gospot-stage`.
- `/usr/lib/gospot/` — Go-бинарник `panel`, ограниченные помощники
  (`gospot-config`, `gospot-configure`, `gospot-power`, `gospot-native-update`,
  `gospot-modem`, `gospot-calibrate`, `gospot-jitter`) и средства миграции.
- `/usr/share/gospot/ui/` — веб-интерфейс.
- `/etc/sudoers.d/gospot` (права 0440, при установке проверяется `visudo -c`) —
  точный список вызовов помощников, которые службы вправе выполнить от root.
- `/lib/systemd/system/gospot-{panel,wizard,stage}.service` — включены и
  запущены; три непривилегированные системные учётные записи `gospot-panel`,
  `gospot-wizard`, `gospot-stage`.
- `/etc/gospot/wizard/` — пароль админки, ключ TLS и самоподписанный
  сертификат этой платы; создаются при установке, если их нет.
- `/var/lib/gospot` и `/var/lib/gospot-bm` — состояние помощников и кэш
  BrandMeister.

**При чистой установке пароль админки случайный**, а не `raspberry`. Прочитайте
его один раз и смените в Configuration:

```sh
sudo cat /etc/gospot/wizard/admin-password
```

**`gospot-radio`** (радиослужбы):

- Радиобинарники в `/usr/local/bin` (MMDVMHost, DMRGateway, шлюзы
  YSF/P25/NXDN/M17/DAPNET/DGId/APRS, межрежимные мосты, «попугаи», MMDVMCal,
  MobileGPS, демоны D-Star), инструменты `gospot-*` и обработчики служб в
  `/usr/local/sbin`.
- Разделяемые библиотеки, которых нет в Debian (`libwiringPi`,
  `libArduiPi_OLED`), в `/usr/local/lib` с регистрацией через `ldconfig`.
- Системная учётная запись `mmdvm` (группы `dialout` и `tty`), каталог
  `/var/log/gospot` (`root:mmdvm`, права 0775) и `/usr/local/etc` для
  справочников и баз позывных.
- По юниту systemd на каждую службу в `/lib/systemd/system`, **все
  выключены**, и включённый `gospot-hostfiles.timer`: через десять минут после
  загрузки и затем раз в сутки (со случайной задержкой до двух часов) он
  обновляет списки серверов, списки групп и `DMRIds.dat`.
- `/etc/gospot-release`, если его ещё нет. Отсутствующие конфигурации
  радиослужб создаются из настроек Pi-Star по умолчанию, переписанных под
  раскладку GoSpot; **существующие конфигурации не трогаются.**

### Перевод существующей платы Pi-Star

Пакеты объявляют `Conflicts`/`Replaces`/`Provides` на `pistar-dashboard` и
`pistar-radio`, поэтому `apt` удаляет старые пакеты и ставит эти вместо них.
Миграция выполняется сама, из скриптов `postinst`:

- **Настройки** — `/etc/pistar-dashboard/pistar.conf` превращается в
  `/etc/gospot/gospot.conf`: берётся новый файл из пакета, а в него
  подставляются ваши значения для всех ключей, которые ещё существуют
  (`PISTAR_*` → `GOSPOT_*`). Значение, совпадавшее со старым значением по
  умолчанию, заменяется новым; настройки, зашитые в сборку (пути установки,
  порты, имена юнитов), всегда берут новое значение.
- **Пароли и состояние** — пароль админки, ключ и сертификат TLS, каталог
  состояния помощников и кэш BrandMeister копируются с правами и временами;
  файлы, принадлежавшие старым служебным учётным записям, переходят к новым.
  Файлы ключей API меняют владельца. Старые юниты `pistar-panel`,
  `pistar-wizard` и `psdash-stage` останавливаются и отключаются, чтобы службы
  GoSpot заняли их порты.
- **Журналы** — `/var/log/pi-star/*` переезжает в `/var/log/gospot` (файлы
  именно перемещаются, без символьных ссылок), и **пути журналов внутри
  конфигураций радиослужб переписываются** под новый каталог. Если журналы или
  конфигурации переехали, активные радиоюниты перезапускаются: работающий демон
  иначе продолжал бы писать по старому пути.
- **Остальные файлы** — `/etc/pistar-release` → `/etc/gospot-release`
  (`[Pi-Star]` → `[GoSpot]`), `/etc/pistar-css.ini` → `/etc/gospot-css.ini`,
  `/etc/pistar-remote` → `/etc/gospot-remote`, журнал Keeper, генератор MOTD и
  файл кода страны Wi-Fi для modprobe.
- **Включённость юнитов** — если `pistar-watchdog`, `pistar-remote` или
  `pistar-hostfiles` были включены, включается соответствующий юнит `gospot-*`;
  работавший останавливается, и запускается его аналог GoSpot.

**Логин админки становится `gospot`, пароль остаётся прежним.** Старого имени
`pi-star` больше нет, пароль не меняется.

Обе миграции идемпотентны и **ничего не удаляют**: старый файл, у которого
новое место уже занято, остаётся там же и упоминается в журнале установки.
Маркер в `/var/lib/gospot` делает повторные запуски пустыми, поэтому ваши
правки нового файла настроек никогда не перезаписываются. Если что-то выглядит
не так, прочитайте вывод установки или
`journalctl -u gospot-panel -u gospot-wizard`.

### Модем и UART на готовой системе

Если модем — плата на GPIO, а система раньше не работала под Pi-Star,
освободите последовательный порт сами: пакеты не правят загрузочные файлы.

```sh
# убрать последовательную консоль из командной строки ядра
sudo sed -i -E 's/(^| )console=(serial0|ttyAMA0|ttyS0)(,[^ ]*)?( |$)/\1/g' /boot/firmware/cmdline.txt
# включить UART на GPIO
grep -q '^enable_uart=1' /boot/firmware/config.txt \
  || printf '\n[all]\nenable_uart=1\n' | sudo tee -a /boot/firmware/config.txt
sudo systemctl mask serial-getty@ttyAMA0.service serial-getty@serial0.service
sudo reboot
```

На Bookworm и более старых раскладках эти два файла могут называться
`/boot/cmdline.txt` и `/boot/config.txt`. Для USB-модемов (`/dev/ttyUSB0`,
`/dev/ttyACM0`) ничего из этого не нужно.

---

## 3. После установки

Откройте **`https://<плата>:8788/manage/`** и пройдите список сверху вниз.
Каждый раздел по кнопке *Apply* записывает свои файлы и перезапускает свои
службы.

1. **Пароль** — Configuration → *Remote Access Password*, если вы ещё не
   меняли его.
2. **Позывной и ID** — Configuration → *General Configuration*: *Node
   Callsign* и *CCS7/DMR ID*. Без них ни одна сеть не заработает.
3. **Частота** — тот же раздел: *Radio Frequency* (в дуплексе — *Radio
   Frequency RX* и *TX*). Частота должна быть разрешена вашей лицензией.
4. **Модем** — тот же раздел: *Radio/Modem Type*. Он задаёт порт и скорость
   модема. Если не знаете, что подключено, поможет `sudo gospot-findmodem`.
5. **Режимы** — Configuration → *Control Software*: контроллер (MMDVMHost или
   DStarRepeater), симплекс или дуплекс и переключатели DMR, D-Star, YSF, P25,
   NXDN, M17 и POCSAG.
6. **Мастер DMR** — Configuration → *DMR Configuration*: *DMR Master*
   (BrandMeister, DMR+, XLX, FreeDMR, …), *DMR ESSID* и *DMR Color Code*. Для
   управления группами BrandMeister добавьте ключ API в Expert → *API Keys*.
7. **Включите службы** для выбранных режимов (см.
   [выше](#включите-радиослужбы)) и посмотрите на **Live Logs** и операторскую
   панель: мастер должен подняться, а модем — отозваться.
8. **Сделайте резервную копию** — Backup/Restore выгружает в ZIP конфигурации
   радиослужб, маркеры контроллера, ключи API, файл оформления, `RSSI.dat`,
   `hosts`/`hostname` и `wpa_supplicant.conf`. Системных паролей в архиве нет.

Калибровка, CSS Tool, проверка джиттера и настенное табло `/kiosk` доступны в
любой момент.
