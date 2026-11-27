# GoSpot

GoSpot — панель оператора и HTTPS-админка цифрового голосового хотспота
(совместима с Pi-Star) на Raspberry Pi OS Bookworm.
[Бэклог функций и критерии приёмки](../Pi-Star_dash_new/docs/BACKLOG.md).
Готовые `.deb` и `SHA256SUMS` — в [Releases](https://github.com/shaposhnikoff/gospot/releases).
После каждого push GitHub Actions тестирует код и публикует отдельный релиз
для этого коммита. Повтор того же запуска обновляет его файлы.
Фронтенд — Svelte, сервер и помощники для радио-INI — в `panel/`.
Классическая PHP-панель собирается из закреплённой ревизии `Pi-Star_DV_Dash`
с исправлениями из `Pi-Star_dash_new/compat/`.

```sh
git clone --recurse-submodules git@github.com:shaposhnikoff/gospot.git
cd gospot/Pi-Star_dash_new
npm ci
scripts/build-deb
```

Нужны Linux, Node.js 22, Go (версия в `panel/go.mod`) и `dpkg-deb`.
Пакет для `armhf` и его SHA-256 выводятся в конце сборки. Расположение:
`build/release-*/gospot_*_armhf.deb`. Пакет предназначен для узла
с пакетом `gospot-radio` (или установленными MMDVMHost и DMRGateway); радио-INI
в пакет не входят. Пакеты `gospot` и `gospot-radio` заменяют прежние
`pistar-dashboard` и `pistar-radio` и переносят их настройки и состояние.
В панели оператора нажмите **Настроить MMDVMHost**. Отметьте нужный протокол:
сразу откроются его радиопараметры и сетевые настройки. Если уже включено
несколько режимов, кнопка **Настроить** переключает редактор без отключения
остальных. Изменения записываются кнопкой **Сохранить изменения**.
[Мобильный экран](../Pi-Star_dash_new/docs/screenshots/protocol-ysf-390px.png) ·
[Настольный экран](../Pi-Star_dash_new/docs/screenshots/protocol-ysf-1366px.png)
— Chromium, YSF рядом с включённым DMR на тестовой копии с пакетом `ci.7`.
Правила TG шлюза доступны отдельно через **Expert → DMRGateway**.
Полное дерево PHP для совместимой классической системы включено отдельно;
наличие этих файлов не означает полного функционального паритета на Bookworm.

Установка опубликованной версии на пилотную `pistar3`:

```sh
cd Pi-Star_dash_new
scripts/install-release deb-НОМЕР-КОММИТ  # точный тег из GitHub Releases
```

Скрипт сверяет `SHA256SUMS` локально и после передачи по SSH, устанавливает
только `.deb` и проверяет состояние служб и неизменность радио-INI.
