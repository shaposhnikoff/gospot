# Бэклог дашборда

Статусы и результаты ведутся только в [GitHub Issues](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues).

- [#1 — Полный перенос без PHP](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/1)
- [#2 — Нативная сборка и пакет](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/2)
- [#3 — Инвентаризация операций](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/3)
- [#4 — Configuration, включая CFG-01](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/4)
- [#5 — Expert Quick Edit](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/5)
- [#6 — Full Edit и ключи](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/6)
- [#7 — Мониторинг и сети, включая BM-01](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/7)
- [#8 — Wi-Fi и Auto AP](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/8)
- [#9 — Диагностика и архивы](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/9)
- [#10 — Tools и обслуживание](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/10)
- [#11 — Полная приёмка и пилот](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/11)

### CFG-01

В **Configuration → MMDVMHost** оператор отмечает используемые протоколы.
Выбор сразу открывает поля соответствующего протокола, снятие выбора скрывает
их. До нажатия «Сохранить» файл и службы не меняются. Набор режимов берётся
из возможностей установленного MMDVMHost и конфигурации узла; параметры
выключенного режима сохраняются для следующего включения.

**Приёмка:** в Chromium на настольном и мобильном экранах включить на стенде
YSF при уже включённом DMR, изменить параметр YSF, сохранить и заново открыть
Configuration через меню. Независимо сверить `/etc/MMDVM-Host.ini`, включая
RF- и Network-секции, права файла и неизменность DMR. Отдельно проверить
отключение YSF и отсутствие записи при уходе со страницы без сохранения.

### BM-01

В меню нужна отдельная страница **BrandMeister**, также доступная из
конфигуратора DMR. На ней оператор видит ID текущего пира, выбирает мастер
по ID, стране или адресу и находит нужную TG по номеру или названию.

| Справочник | Источник |
|---|---|
| Мастера | <https://api.brandmeister.network/v2/master> |
| Разговорные группы | <https://api.brandmeister.network/v2/talkgroup> |

- Сохранение мастера меняет настройки DMRGateway на узле. **«Добавить группу
  пиру»** создаёт статическую подписку в сети BrandMeister для выбранного
  пира и слота. Изменение локального `TGRewrite` эту операцию не заменяет.
- Показывать текущие подписки; разрешить добавить TG и удалить выбранную
  подписку, не затрагивая другие группы и слоты. Для симплексного пира
  использовать слот `0`, для дуплексного — выбранный `1` или `2`.
- Для чтения публичных справочников ключ не нужен. Для изменения подписок
  предусмотреть ввод **BM API key** в HTTPS-админке. Это не пароль Hotspot
  Security. Не возвращать сохранённый ключ браузеру и не выводить его в
  журналы, репозиторий или тестовые протоколы. Реальный ключ понадобится
  только при согласованной проверке на реальном пире; для стенда — тестовый.

**Приёмка:** на локальном HTTPS API выбрать TG из справочника, добавить её
пиру, перечитать профиль, удалить ту же подписку и снова перечитать профиль.
Для каждого действия сверить CSRF, ID пира, слот, TG и ровно один
авторизованный запрос. Обновление страницы не должно повторять команду.
Проверить отказ при отсутствии/неверном ключе и HTTP 503: оператор видит
причину ошибки, ложного сообщения об успехе нет, подписки не изменены.
