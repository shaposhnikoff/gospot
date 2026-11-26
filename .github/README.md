# Pi-Star Dashboard New

Панель оператора и HTTPS-админка для Pi-Star на Raspberry Pi OS Bookworm.
Готовые `.deb` и `SHA256SUMS` — в [Releases](https://github.com/shaposhnikoff/pi-star-dashboard-new/releases).
После каждого push GitHub Actions тестирует код и публикует отдельный релиз
для этого коммита. Повтор того же запуска обновляет его файлы.
Фронтенд — Svelte, сервер и помощники для радио-INI — в `panel/`.
Классическая PHP-панель собирается из закреплённой ревизии `Pi-Star_DV_Dash`
с исправлениями из `Pi-Star_dash_new/compat/`.

```sh
git clone --recurse-submodules git@github.com:shaposhnikoff/pi-star-dashboard-new.git
cd pi-star-dashboard-new/Pi-Star_dash_new
npm ci
scripts/build-deb
```

Нужны Linux, Node.js 22, Go (версия в `panel/go.mod`) и `dpkg-deb`.
Пакет для `armhf` и его SHA-256 выводятся в конце сборки. Расположение:
`build/release-*/pistar-dashboard_*_armhf.deb`. Пакет предназначен для узла
с установленными MMDVMHost и DMRGateway; радио-INI в пакет не входят.
В нативной админке доступны Configuration → DMR и Expert → DMRGateway.
Полное дерево PHP для совместимой классической системы включено отдельно;
наличие этих файлов не означает полного функционального паритета на Bookworm.

Установка опубликованной версии на пилотную `pistar3`:

```sh
cd Pi-Star_dash_new
scripts/install-release deb-НОМЕР-КОММИТ  # точный тег из GitHub Releases
```

Скрипт сверяет `SHA256SUMS` локально и после передачи по SSH, устанавливает
только `.deb` и проверяет состояние служб и неизменность радио-INI.
