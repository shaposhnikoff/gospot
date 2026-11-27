# GoSpot

Панель и радиостек цифрового голосового хотспота на Raspberry Pi: раскладка
файлов совместима с Pi-Star, PHP в системе нет.

English version: [README.md](README.md) · Установка:
[docs/INSTALL.ru.md](docs/INSTALL.ru.md)

## Что такое GoSpot

- **Бэкенд на Go** ([`panel/`](panel)): панель, HTTPS-админка и все записи в
  конфигурацию, плюс небольшой набор ограниченных root-помощников на Python и
  bash ([`panel/helper/gospot-*`](panel/helper)).
- **Интерфейс на Svelte 5 и TypeScript**
  ([`Pi-Star_dash_new/src`](Pi-Star_dash_new/src)): та же разметка, те же цвета
  и то же поведение, что у классической панели Pi-Star.
- **Радиопакет** ([`radio/`](radio)): MMDVMHost, DMRGateway, шлюзы
  YSF/P25/NXDN/M17/DAPNET/DGId/APRS, межрежимные мосты, демоны D-Star,
  MMDVMCal и обработчики служб — всё в раскладке Pi-Star.
- **Образ Raspberry Pi OS Trixie (Debian 13) armhf** ([`image/`](image)):
  пакеты ставятся внутри образа штатно, через `apt`, а не копированием файлов.

На выходе два пакета Debian: `gospot` (панель) и `gospot-radio` (радиослужбы),
версия 0.2.0-x, архитектура armhf. Службы: `gospot-panel` (сборщик данных,
внутренний, порт 8787), `gospot-wizard` (HTTPS-админка, 8788) и `gospot-stage`
(операторская панель, 8789). Радиоюниты ставятся **выключенными** и остаются
такими, пока оператор их не включит.

## Чем GoSpot не является

- **Это не PHP.** PHP нет ни в исходниках, ни в пакетах, ни во время работы:
  ни PHP-FPM, ни nginx, ни `/var/www/dashboard`. Ничего не проксируется в
  старую панель и не открывается в iframe.
- **Это не Pi-Star.** Имя продукта, имена пакетов, служебные учётные записи и
  пути `gospot-*` — собственные. Проект Pi-Star не поддерживает GoSpot и не
  имеет к нему отношения.
- **Это не форк интерфейса Pi-Star.** Панель написана заново на Svelte и Go;
  классическая панель служила только эталоном поведения.

## Совместимость с Pi-Star

**Раскладка системы совместима**: конфигурации без суффикса `.ini`
(`/etc/mmdvmhost`, `/etc/dmrgateway`, `/etc/ysfgateway`, …), бинарники в
`/usr/local/bin`, скрипты в `/usr/local/sbin`, справочники и базы позывных в
`/usr/local/etc`, маркеры контроллера `/etc/dstar-radio.*`. GoSpot читает и
пишет те же самые файлы, что и оригинал, поэтому готовая настройка радиочасти
продолжает работать. **Классические резервные копии тоже совместимы**:
Backup/Restore принимает классический архив `Pi-Star_Config_*.zip` и
переименовывает те файлы, которые надо (`pistar-remote` → `gospot-remote`,
`pistar-css.ini` → `gospot-css.ini`).

**Имена несовместимы намеренно**: `gospot` и `gospot-radio` заменяют
`pistar-dashboard` и `pistar-radio` (через `Conflicts`/`Replaces`/`Provides`),
файл настроек — `/etc/gospot/gospot.conf`, журналы — в `/var/log/gospot`.
Плата, обновлённая с пакетов Pi-Star, переносится автоматически — см.
[docs/INSTALL.ru.md](docs/INSTALL.ru.md).

## Что уже работает

**Операторская панель** — `http://<плата>:8789/`, без входа:

- Классическая главная страница: состояние узла и модема, частоты RX/TX,
  включённые режимы MMDVMHost, состояние сетей, Last Heard и Local RF
  Activity, сообщения POCSAG, а при контроллере DStarRepeater — панели D-Star
  (настройки шлюза, активные линки рефлекторов, соединения CCS, группы
  StarNet).
- **Менеджер BrandMeister**: подписки TS1/TS2, статические группы, сброс
  динамических, завершение QSO (нужен ключ API BM); **менеджер TGIF**:
  подключение и отключение, а с токеном TGIF — ещё и статические группы.
- Менеджеры соединений YSF, P25, NXDN и M17; отдельный **D-Star Link Manager**
  с журналом GoSpot-Keeper (линк и отключение через `remotecontrold`).
- **Плеер BrandMeister Hoseline** под Local RF Activity: только прослушивание,
  одна разговорная группа, звук ретранслирует сама плата.
- `/kiosk` — полноэкранное табло для монитора или телевизора рядом с
  хотспотом (1920×1080, 1280×720 и портрет 1080×1920): текущий вызов с именем
  оператора и названием TG либо `Listening — last call N min ago`, ниже —
  последние десять вызовов.
- `/api/dashboard` и `/api/last_heard.php` — для всего, что хочет данные.

**Админка** — `https://<плата>:8788/manage/`, логин `gospot`:

- **Configuration** со всеми разделами: Gateway Hardware Information, Control
  Software (контроллер, симплекс/дуплекс, включение режимов), MMDVMHost
  Configuration, General Configuration, DMR, D-Star, Yaesu System Fusion, P25,
  NXDN, M17, POCSAG, Mobile GPS, Firewall, Wireless и Remote Access Password —
  плюс **Factory Reset**.
- **Expert**: Quick Edit для MMDVMHost, DMRGateway, шлюзов YSF/P25/NXDN/DAPNET,
  ircDDBGateway, TimeServer и DStarRepeater; Full Edit для файла DMRGateway,
  GoSpot-Remote, файла Wi-Fi, System Cron и RSSI.dat; **API Keys**
  (BrandMeister, DAPNET, TGIF); **CSS Tool**; **FW** — прошивка модема с живым
  журналом; **SSH Access** — веб-терминал shellinabox, если он установлен; и
  **проверка джиттера** для BrandMeister, DMR+ и HBLink.
- **Wi-Fi**: состояние и перезапуск адаптера, поиск сетей, добавление и
  удаление профилей, код страны — через NetworkManager или классический
  `wpa_supplicant`, выбор одной настройкой.
- **Calibration**: MMDVMCal на модеме платы с живым графиком, статистикой BER
  и кнопкой *Save Offset*.
- **Live Logs**, **Power** (перезагрузка и выключение), **Update** (справочники,
  базы позывных и пакеты ОС), **Backup/Restore**.

Любая запись идёт через ограниченный помощник, проверяется, сверяется с
ревизией формы и откатывается, если служба не поднялась.

## Поддерживаемое оборудование и ОС

| | Поддержка |
| --- | --- |
| Платы | Raspberry Pi 2 Model B и новее |
| Архитектура | сейчас armhf (ARMv7+); arm64 запланирован |
| Образ ОС | Raspberry Pi OS Lite Trixie (Debian 13), armhf |
| Готовые платы | Raspberry Pi OS Bookworm и Trixie |

## Быстрый старт

Либо записать образ, либо поставить два пакета на уже работающую Raspberry Pi
OS. Оба пути, включая перевод существующей платы с Pi-Star, описаны в
**[docs/INSTALL.ru.md](docs/INSTALL.ru.md)** ([English](docs/INSTALL.md)).

## Структура репозитория

| Путь | Содержимое |
| --- | --- |
| [`panel/`](panel) | Бэкенд на Go: панель, служба администрирования, запись конфигураций, root-помощники в `panel/helper/` |
| [`Pi-Star_dash_new/`](Pi-Star_dash_new) | Интерфейс на Svelte (`src/`), файл настроек `gospot.conf`, пакет `gospot` (`debian/`, `scripts/`) и стенды Chromium (`qa/`) |
| [`radio/`](radio) | Пакет `gospot-radio`: радиобинарники, пересборка D-Star, обработчики служб на Python 3, таблица переименований, перевод платы |
| [`image/`](image) | Локальная сборка образа Trixie armhf (Docker + pi-gen), юнит первой загрузки, офлайн-проверка, запуск в QEMU |
| [`Pi-Star_DV_Dash/`](Pi-Star_DV_Dash) | Классическая панель, закреплённая как эталон поведения, только для чтения |
| [`docs/`](docs) | Документация верхнего уровня на двух языках |

Все пути, порты, имена юнитов и служебных учётных записей лежат в **одном**
файле — [`Pi-Star_dash_new/gospot.conf`](Pi-Star_dash_new/gospot.conf), на
плате это `/etc/gospot/gospot.conf`. Код, помощники, упаковка и тесты читают
его; в коде путей нет.

## Как это проверено

- Юнит-тесты: Go (`go test ./...` в `panel/`), Python-помощники
  (`panel/helper/test_*.py`, `radio/test_*.py`) и интерфейс (`npm test`).
- 13 сценариев в Chromium на изолированном стенде с настоящим Go-бэкендом,
  настоящими помощниками и тестовыми юнитами systemd —
  `Pi-Star_dash_new/qa/real-write/run.sh` (`--configure`, `--expert`,
  `--services`, `--backup`, `--css`, `--factory`, `--wifi`, `--calibration`,
  `--jitter`, `--dstar`, `--tgif`, `--hoseline`) и `qa/kiosk_screens.py`.
  Каждый сценарий открывает страницу, пишет, читает заново, побайтово сверяет
  файлы на диске и проверяет откат.
- 81 офлайн-проверка готового образа (`image/verify-artifact.sh`): пакеты и
  файлы, включённые и выключенные юниты, отсутствие PHP, nginx и php-fpm,
  отсутствие секретов платы, сверка упакованного файла настроек с
  репозиторным, `visudo -c`, раскладка Pi-Star, разрешение библиотек,
  настройки UART. Затем `image/qemu-boot.sh` поднимает две «платы» и
  проверяет, что сертификаты TLS и ключи хоста SSH у них разные.

## Куда сообщать о проблемах

Ошибки, вопросы и пожелания — <https://github.com/shaposhnikoff/gospot/issues>;
релизы (оба пакета `.deb`, образ, контрольные суммы) —
<https://github.com/shaposhnikoff/gospot/releases>.

## Лицензии и авторство

Радиопрограммы, которые поставляет GoSpot, — работа **Джонатана Нейлора
G4KLX**: MMDVMHost, DMRGateway, шлюзы YSF/P25/NXDN/M17/DAPNET, ircDDBGateway,
DStarRepeater, MMDVMCal и межрежимные мосты. Они остаются под своими
лицензиями.

**Pi-Star** Энди Тейлора **MW0MWZ** — эталон, за которым GoSpot повторяет
раскладку системы, формат конфигураций и поведение панели. GoSpot —
независимый проект: это не Pi-Star и это не одобрено Pi-Star.

Собственный код GoSpot пока без файла лицензии.

## Что запланировано

В бэклоге, не в поставке: пакеты и образ arm64, интерфейс командной строки
`gospot` и собственное зеркало справочников (сейчас таймер
`gospot-hostfiles.timer` раз в сутки качает списки и базы позывных с
`pistar.uk`).
