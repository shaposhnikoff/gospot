/** Operator-facing destinations from Pi-Star_DV_Dash. A path opens a page,
 * never a command. Use the containing page for conditional PHP includes. */
export interface ConsoleRoute {
  id: string;
  title: string;
  description: string;
  path?: string;
  externalPath?: string;
}

interface RouteGroup {
  id: string;
  title: string;
  routes: ConsoleRoute[];
}

const admin = '/admin/';
const config = '/admin/configure.php';
const expert = '/admin/expert/';
const page = (id: string, title: string, description: string, path?: string, externalPath?: string): ConsoleRoute =>
  ({ id, title, description, path, externalPath });
const quick = (name: string, title: string, description: string) =>
  page(`quick-${name}`, title, description, `${expert}edit_${name}.php`);
const full = (name: string, title: string, description: string) =>
  page(`full-${name}`, title, description, `${expert}fulledit_${name}.php`);

export const groups: RouteGroup[] = [
  { id: 'monitor', title: 'Наблюдение', routes: [
    page('dashboard', 'Эфир и состояние узла', 'Открытая панель: частоты RX/TX, модем, режимы MMDVMHost, FM, сети и состояние передачи. В DStarRepeater — параметры шлюза D-Star.', '/'),
    page('heard', 'Последние станции и локальные передачи', 'Таблицы Last Heard и Local TX в публичной панели. Состав и названия таблиц зависят от контроллера.', '/'),
    page('pages', 'Сообщения пейджеров', 'При включённом POCSAG: получатель RIC, текст DAPNET и декодирование Skyper.', '/#Pages'),
    page('dstar-links', 'Соединения D-Star', 'CCS и активные группы StarNet с участниками, если они настроены.', '/#cssConnects'),
    page('last-heard-api', 'Последние передачи в JSON', 'Публичный API MMDVMHost: позывной, источник, длительность, потери, BER и RSSI.', '/api/last_heard.php'),
    page('admin', 'Панель администратора', 'Состояние системы и управление соединениями включённых сетей.', admin),
    page('hardware', 'Состояние оборудования', 'Имя и IP-адрес, ядро, платформа, время работы, загрузка и температура CPU, состояние служб.', '/admin/#sysInfo'),
    page('sysinfo', 'Сведения о системе', 'Использование памяти RAM и дисковых разделов, версии установленного ПО и шлюзов.', '/admin/sysinfo.php'),
    page('log', 'Живой журнал модема', 'Просмотр новых строк Live Logs и скачивание полного журнала за сегодня.', '/admin/live_modem_log.php'),
    page('services', 'Службы и журнал PiStar-Keeper', 'Статус шести служб, управление рефлектором и скачивание журнала при работающем Keeper.', '/admin/admin.php'),
  ] },
  { id: 'networks', title: 'Управление сетями', routes: [
    page('dstar', 'Рефлекторы D-Star', 'Активные линки и выбор модуля, рефлектора, подключения или отключения в панели Admin.', '/admin/#refLinks'),
    page('bm', 'Группы BrandMeister', 'Подписки TS1/TS2, добавление и удаление статических TG, сброс динамических групп и завершение QSO. Для управления нужен ключ BM.', '/admin/#bmConnects'),
    page('tgif', 'Группы TGIF', 'Подключение и отключение разговорной группы на выбранном таймслоте в панели Admin.', admin),
    page('tgif-static', 'Статические группы TGIF', 'Список подписок, добавление и удаление TG на TS1 и TS2. Нужны Pi-Star 4.3.9+, сеть TGIF и токен.', '/admin/#tgifStaticConnects'),
    page('ysf', 'Рефлекторы YSF', 'Выбор узла System Fusion и подключение или отключение в панели Admin.', admin),
    page('p25', 'Группы P25', 'Выбор P25-хоста и управление соединением в панели Admin.', admin),
    page('nxdn', 'Группы NXDN', 'Выбор NXDN-хоста и управление соединением в панели Admin.', admin),
    page('m17', 'Рефлекторы M17', 'Выбор хоста и модуля M17, подключение и отключение в панели Admin.', admin),
  ] },
  { id: 'config', title: 'Настройки узла', routes: [
    page('configuration', 'Основная конфигурация', 'Позывной, имя узла, DMR и NXDN ID, модем, частоты, координаты, язык и часовой пояс.', config),
    page('radio', 'Контроллер и радиорежимы', 'MMDVMHost или DStarRepeater, симплекс или дуплекс, дисплей, время удержания режима и разрешённые протоколы.', `${config}#config`),
    page('cross', 'Межрежимные шлюзы', 'Мосты YSF2DMR, YSF2NXDN, YSF2P25, DMR2YSF и DMR2NXDN; стартовые группы и серверы.', config),
    page('dmr-config', 'Параметры DMR', 'Мастер, BrandMeister, DMR+, XLX, FreeDMR, ESSID, Color Code, Hotspot Security и параметры сети.', config),
    page('dstar-config', 'Параметры D-Star', 'Позывные RPT1/RPT2, рефлектор по умолчанию, ircDDB, маршрутизация, DPlus и голосовое объявление времени.', config),
    page('digital-config', 'Параметры YSF, P25, NXDN и M17', 'Стартовый хост, WIRES-X, код NAC для P25, RAN для NXDN и CAN для M17.', config),
    page('paging-config', 'Параметры POCSAG', 'Сервер DAPNET, позывной, частота, ключ авторизации и списки разрешённых или запрещённых RIC.', `${config}#pocsagFrequency`),
    page('location', 'Координаты, APRS и MobileGPS', 'Местоположение узла, передача координат в APRS, порт и скорость приёмника GPS.', `${config}#confLatitude`),
    page('access', 'Доступ и межсетевой экран', 'Публичный или частный узел, список DMR ID, доступ к панели, ircDDB и SSH, правила UPnP.', config),
    page('password', 'Пароль администратора', 'Смена пароля веб-панели и системной учётной записи pi-star. При обязательной смене заводского пароля узел сам откроет нужную форму.', `${config}#adminPassForm`),
  ] },
  { id: 'wifi', title: 'Беспроводная сеть', routes: [
    page('wifi-info', 'Состояние Wi-Fi', 'Интерфейс wlan0: IP, SSID, канал, сигнал, счётчики трафика и форма перезапуска адаптера.', '/admin/wifi.php?page=wlan0_info'),
    page('wifi-profiles', 'Сети и профили Wi-Fi', 'Поиск точек доступа, добавление и удаление SSID, ключ WPA и код страны.', '/admin/wifi.php?page=wpa_conf'),
    page('autoap', 'Резервная точка доступа Auto AP', 'Автоматическая точка доступа при потере Wi-Fi и смена её ключа PSK в конфигурации узла.', `${config}#autoApPassForm`),
  ] },
  { id: 'service', title: 'Обслуживание', routes: [
    page('backup', 'Резервная копия и восстановление', 'Скачать конфигурацию в ZIP или восстановить из архива. Настройки Wi-Fi входят в копию, системные пароли — нет.', '/admin/config_backup.php'),
    page('power', 'Питание узла', 'Перезагрузка и выключение через форму Power.', '/admin/power.php'),
    // Installed clone requires confirmation. On an unknown external release,
    // go through its menu: older update/upgrade pages can start work on GET.
    page('update', 'Обновление компонентов', 'Пункт Update в меню Admin: обновление компонентов и справочников Pi-Star.', '/admin/update.php', admin),
    page('upgrade', 'Обновление версии Pi-Star', 'Пункт Upgrade в разделе Expert: переход на новую версию системы.', `${expert}upgrade.php`, expert),
    page('calibration', 'Калибровка модема', 'MMDVMCal: частота приёма, смещение RX Offset, график и статистика ошибок BER.', '/admin/calibration.php'),
    page('firmware', 'Прошивка модема', 'В меню Expert выберите Tools → FW и модель модема. При входе в FW узел очищает предыдущий журнал прошивки.', expert),
    page('jitter', 'Проверка джиттера сети', 'Штатный jitter_test.php начинает измерение задержек при открытии. Запуск из каталога недоступен. Поддерживаются BrandMeister, DMR+ и HBLink.'),
    page('factory', 'Заводские настройки', 'Откройте Configuration и выберите Factory Reset в меню устройства.', config),
    page('ssh', 'Терминал SSH', 'Веб-терминал устройства при установленном Shell In A Box.', `${expert}ssh_access.php`),
  ] },
  { id: 'quick', title: 'Экспертные настройки', routes: [
    page('expert', 'Обзор редакторов Expert', 'Штатное меню Quick Edit, Full Edit и Tools.', expert),
    quick('mmdvmhost', 'Контроллер MMDVMHost', 'Секции конфигурации многорежимного контроллера.'),
    quick('dstarrepeater', 'Контроллер DStarRepeater', 'Параметры D-Star-репитера.'),
    quick('ircddbgateway', 'Шлюз ircDDBGateway', 'Параметры маршрутизации D-Star и шлюза.'),
    quick('timeserver', 'Служба TimeServer', 'Голосовые объявления времени в D-Star.'),
    quick('starnetserver', 'Сервер групп StarNet', 'Параметры групп D-Star StarNet в конфигурации StarNetServer.'),
    quick('dmrgateway', 'Шлюз DMRGateway', 'Структурированный редактор параметров DMR-сетей.'),
    quick('ysfgateway', 'Шлюз YSFGateway', 'Параметры шлюза Yaesu System Fusion.'),
    quick('p25gateway', 'Шлюз P25Gateway', 'Параметры шлюза P25.'),
    quick('nxdngateway', 'Шлюз NXDNGateway', 'Параметры шлюза NXDN.'),
    quick('dapnetgateway', 'Шлюз DAPNETGateway', 'Сетевые настройки пейджингового шлюза.'),
  ] },
  { id: 'files', title: 'Файлы и ключи', routes: [
    full('dmrgateway', 'Файл DMRGateway', 'Полная текстовая конфигурация DMR-шлюза, сетей и правил перезаписи.'),
    full('pistar-remote', 'Удалённое управление PiStar-Remote', 'Конфигурация команд дистанционного управления узлом.'),
    full('wpaconfig', 'Файл Wi-Fi', 'Текстовый редактор wpa_supplicant.conf.'),
    full('apikeys', 'Ключи внешних сетей', 'Единая страница API Keys для BrandMeister, DAPNET и TGIF.'),
    full('bmapikey', 'Ключ BrandMeister', 'Токен API для управления разговорными группами BM.'),
    full('dapnetapi', 'Учётная запись DAPNET', 'Имя пользователя, пароль и зона передатчиков TRXAREA.'),
    full('tgifapikey', 'Ключ статических TGIF TG', 'Токен для управления статическими группами на Pi-Star 4.3.9 и новее.'),
    full('cron', 'Системное расписание', 'Просмотр и изменение заданий System Cron.'),
    full('rssidat', 'Таблица RSSI', 'Калибровочные значения уровня принимаемого сигнала в RSSI.dat.'),
    page('appearance', 'Оформление панели', 'В меню Expert выберите Tools → CSS Tool для цветов, заголовка и сброса темы. При первом входе редактор создаёт файл стилей.', expert),
  ] },
];

/** Require an authority-only HTTP(S) URL before letting URL canonicalize it.
 * This rejects userinfo (including empty userinfo), slash/backslash tricks,
 * query strings, fragments and paths that would normalize away. */
export function stockOrigin(value: string): string | null {
  const input = value.trim();
  if (!/^https?:\/\/[^\s\\/@?#%\u0000-\u001f\u007f]+\/?$/i.test(input)) return null;
  try {
    const url = new URL(input);
    return url.hostname && !url.username && !url.password ? url.origin : null;
  } catch {
    return null;
  }
}

export function isOperatorPath(pathname: string): boolean {
  return ['/operator/', '/operator/index.html', '/operator'].includes(pathname);
}

const normalize = (text: string) => text.toLowerCase().replace(/ё/g, 'е').replace(/[-‑–]/g, '');

export function searchGroups(query: string, groupId: string): RouteGroup[] {
  const tokens = normalize(query.trim()).split(/\s+/).filter(Boolean);
  return groups.filter(group => !groupId || groupId === group.id).map(group => ({
    ...group,
    routes: group.routes.filter(route => {
      const text = normalize(`${group.title} ${route.title} ${route.description} ${route.path ?? ''}`);
      return tokens.every(token => text.includes(token));
    }),
  })).filter(group => group.routes.length > 0);
}
