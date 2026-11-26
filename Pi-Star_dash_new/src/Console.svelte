<script lang="ts">
  import { onMount } from 'svelte';
  import { groups, isOperatorPath, searchGroups, stockOrigin } from './routes';

  let { pi = false, classicPort = null }: { pi?: boolean, classicPort?: number|null } = $props();
  const storageKey = 'pistar.console.stock-origin.v1';
  let here = $state('');
  let operatorPath = $state(false);
  let saved = $state<string | null>(null);
  let draft = $state('');
  let message = $state('');
  let invalid = $state(false);
  let query = $state('');
  let groupId = $state('');
  const integrated = $derived(!pi && operatorPath && !!here);
  const target = $derived(integrated ? here : saved && saved !== here ? saved : null);
  const wizard = $derived(here ? `https://${new URL(here).hostname}:8788/` : '');
  const results = $derived(searchGroups(query, groupId));
  const count = $derived(results.reduce((sum, group) => sum + group.routes.length, 0));
  function nativeLink(id:string, path?:string): string | undefined {
    if (!pi || !here) return;
    if (classicPort && path?.startsWith('/admin/')) {
      return `https://${new URL(here).hostname}:${classicPort}${path}`;
    }
    const destinations:Record<string,string> = {
      dashboard: '#overview', heard: '/mmdvmhost/lh.php', 'last-heard-api':'/api/last_heard.php',
      admin:'/admin/', hardware:'/admin/sysinfo.php', sysinfo:'/admin/sysinfo.php', log:'/admin/live_modem_log.php',
      configuration:'/admin/configure.php', expert:'/admin/expert/', 'quick-dmrgateway':wizard+'manage/#expert-dmrgateway',
      'quick-mmdvmhost':wizard+'manage/#expert-mmdvmhost', backup:'/admin/config_backup.php', 'dmr-config':wizard+'manage/#services'
    };
    return destinations[id];
  }

  onMount(() => {
    here = stockOrigin(window.location.origin) ?? '';
    operatorPath = isOperatorPath(window.location.pathname);
    try {
      saved = stockOrigin(localStorage.getItem(storageKey) ?? '');
      if (saved === here) saved = null;
      draft = saved ?? '';
    } catch { /* Local-only navigation still works with storage disabled. */ }
  });

  function save(event: SubmitEvent) {
    event.preventDefault();
    const origin = stockOrigin(draft);
    invalid = !origin || origin === here;
    if (!origin) {
      message = 'Введите адрес вида http://pi-star.local или https://имя:порт без логина, пароля, пути и параметров.';
      return;
    }
    if (origin === here) {
      message = 'Это адрес открытой консоли. Укажите адрес штатной PHP-панели Pi-Star.';
      return;
    }
    saved = origin;
    draft = origin;
    try {
      localStorage.setItem(storageKey, origin);
      message = 'Адрес сохранён в этом браузере.';
    } catch {
      message = 'Адрес выбран до закрытия консоли: хранилище браузера недоступно.';
    }
  }

  function forget() {
    saved = null;
    draft = '';
    invalid = false;
    try {
      localStorage.removeItem(storageKey);
      message = 'Адрес удалён.';
    } catch {
      message = 'Адрес сброшен для этой вкладки. Хранилище браузера недоступно.';
    }
  }
</script>

<section class="console" aria-labelledby="console-title" lang="ru" data-testid="console">
  <header class="console-heading">
    <p class="eyebrow"><span></span>РАБОЧЕЕ МЕСТО ОПЕРАТОРА</p>
    <h1 id="console-title">Консоль Pi-Star</h1>
    <p>Найдите нужную функцию и откройте её на устройстве.</p>
  </header>

  {#if pi}
    <section class="device-box" aria-label="Инструменты этого узла">
      <strong>Этот узел</strong>
      <div class="device-links">
        <a href="#overview" data-testid="console-link-overview">Обзор узла <span aria-hidden="true">→</span></a>
        {#if wizard}
          <a href={wizard + 'manage/'} target="_blank" rel="noopener noreferrer" data-testid="console-link-native">Параметры станции · новый интерфейс ↗</a>
          <a href={wizard + 'manage/#services'}>Сервисы · Настройка DMR ↗</a>
          <a href={wizard} target="_blank" rel="noopener noreferrer" data-testid="console-link-wizard">Мастер настройки <span aria-hidden="true">↗</span></a>
        {/if}
      </div>
    </section>
  {/if}

  <section class="backend" aria-labelledby="backend-title">
    <div>
      <p class="kicker">ШТАТНАЯ ПАНЕЛЬ PI-STAR</p>
      <h2 id="backend-title">{integrated ? 'Этот сервер' : 'Адрес устройства'}</h2>
      {#if !integrated}
        <p>{pi ? 'Перенесённые разделы открываются на этом узле. Для остальных можно указать отдельное устройство со штатной панелью Pi-Star.' : 'Для перехода из демо укажите адрес существующей PHP-панели Pi-Star.'}</p>
      {/if}
      {#if target}
        <p class="destination" data-testid="console-target">Ссылки ведут на <code>{target}</code>.</p>
      {/if}
    </div>
    {#if !integrated}
      <form class="origin-form" onsubmit={save} novalidate>
        <label for="console-origin">Адрес штатной панели</label>
        <div class="input-row">
          <input id="console-origin" data-testid="console-origin" type="url" inputmode="url" autocomplete="off" spellcheck="false"
            aria-invalid={invalid} aria-describedby="origin-help origin-message" placeholder="http://pi-star.local" bind:value={draft}
            oninput={() => { invalid = false; message = ''; }} />
          <button class="apply" type="submit" data-testid="console-origin-save">Сохранить адрес</button>
          {#if saved}
            <button type="button" data-testid="console-origin-clear" onclick={forget}>Забыть</button>
          {/if}
        </div>
        <small id="origin-help">Только адрес, без пароля. Доступность узла проверяется при открытии ссылки.</small>
        <p id="origin-message" class:error={invalid} role="status">{message}</p>
      </form>
    {/if}
  </section>

  <section class="catalog" aria-labelledby="catalog-title">
    <div class="catalog-head">
      <div>
        <p class="kicker">НАВИГАЦИЯ</p>
        <h2 id="catalog-title">Каталог функций</h2>
      </div>
      <label class="search">
        <span>Поиск по каталогу</span>
        <input type="search" data-testid="console-search" placeholder="Например, Wi-Fi, BER или рефлектор" bind:value={query} />
      </label>
    </div>
    <div class="group-controls" role="group" aria-label="Разделы каталога">
      <button type="button" aria-pressed={groupId === ''} data-testid="console-filter-all" onclick={() => groupId = ''}>Все разделы</button>
      {#each groups as group (group.id)}
        <button type="button" aria-pressed={groupId === group.id} data-testid={'console-filter-' + group.id}
          onclick={() => groupId = group.id}>{group.title}</button>
      {/each}
    </div>
    <p class="info">Набор функций на устройстве зависит от версии Pi-Star, включённых режимов и прав доступа.</p>
    <p class="count" role="status">Найдено: {count}</p>
    {#each results as group (group.id)}
      <section class="route-group" data-testid={'console-group-' + group.id} aria-labelledby={'console-heading-' + group.id}>
        <h3 id={'console-heading-' + group.id}>{group.title} <span>{group.routes.length}</span></h3>
        <ul>
          {#each group.routes as route (route.id)}
            {@const path = integrated ? route.path : route.externalPath ?? route.path}
            <li data-testid={'console-item-' + route.id}>
               {#if nativeLink(route.id, route.externalPath ?? route.path)}
                 <a href={nativeLink(route.id, route.externalPath ?? route.path)} class="route-title" data-testid={'console-native-'+route.id}>{route.title} <span aria-hidden="true">→</span></a>
               {:else if target && path}
                <a href={target + path} class="route-title" target="_blank" rel="noopener noreferrer"
                  data-testid={'console-link-' + route.id}>{route.title} <span aria-hidden="true">↗</span></a>
              {:else}
                <strong class="route-title">{route.title}</strong>
              {/if}
              <p>{pi && route.id==='dmr-config' ? 'MMDVMHost: Color Code и таймслоты. DMRGateway: мастер сети 1 и существующие правила TG.' : route.description}</p>
              {#if !path}
                <small>Только на устройстве</small>
              {:else if !target && !nativeLink(route.id, route.externalPath ?? route.path)}
                <small>Нужна штатная панель Pi-Star</small>
              {/if}
            </li>
          {/each}
        </ul>
      </section>
    {:else}
      <div class="no-results">
        <strong>Ничего не найдено</strong>
        <p>Измените запрос или выберите другой раздел.</p>
        <button type="button" onclick={() => { query = ''; groupId = ''; }}>Сбросить поиск и фильтр</button>
      </div>
    {/each}
  </section>
</section>

<style>
  .console { min-width: 0; padding-bottom: 1.2rem; color: #252334; }
  .console-heading { margin-bottom: 1.2rem; }
  .console-heading > p:last-child, .backend p:not(.kicker), .info, li p, .no-results p { margin: .4rem 0 0; color: #635e6b; line-height: 1.5; font-size: .88rem; }
  .kicker { color: #9c2455; margin: 0 0 .25rem; font-size: .66rem; font-weight: 720; letter-spacing: .11em; }
  h2, h3 { margin: 0; line-height: 1.3; }
  h2 { font-size: 1.25rem; letter-spacing: -.025em; }
  .device-box, .backend, .catalog { border: 1px solid #e1dce4; border-radius: 14px; background: white; padding: clamp(.9rem, 2.2vw, 1.5rem); }
  .backend, .device-box { margin-bottom: .9rem; background: #fcf9fb; }
  .backend { display: flex; flex-wrap: wrap; align-items: start; gap: 1rem 2rem; }
  .backend > div { flex: 1 1 16rem; min-width: 0; }
  .origin-form { flex: 1.4 1 22rem; min-width: 0; }
  .device-box, .device-links, .input-row, .catalog-head, .group-controls { display: flex; align-items: center; flex-wrap: wrap; gap: .5rem; }
  .device-box, .catalog-head { justify-content: space-between; gap: .75rem 1.5rem; }
  .device-links { gap: .8rem 1.5rem; }
  label, .search span { display: block; color: #514a59; font-size: .78rem; font-weight: 650; }
  input, button { font: inherit; }
  input { min-width: 0; width: 100%; border: 1px solid #bcb5c1; border-radius: 8px; background: white; padding: .57rem .65rem; font-size: .86rem; color: #252334; }
  input[aria-invalid="true"] { border-color: #a0233e; }
  button { border: 1px solid #d5cdd8; border-radius: 8px; background: white; color: #514a59; padding: .57rem .75rem; cursor: pointer; font-size: .8rem; }
  .input-row, .search input { margin-top: .35rem; }
  .input-row input { flex: 1 1 12rem; }
  button.apply, .group-controls button[aria-pressed="true"] { color: white; background: #912153; border-color: #912153; }
  button:hover { border-color: #912153; }
  .console :is(input, a, button):focus-visible { outline: 3px solid #ba4676; outline-offset: 2px; }
  small { display: block; color: #67616d; font-size: .75rem; line-height: 1.4; margin-top: .42rem; }
  #origin-message:empty { margin: 0; }
  #origin-message.error { color: #9c263f; }
  code { overflow-wrap: anywhere; color: #57233e; font-weight: 600; }
  .search { flex: 0 1 25rem; min-width: 0; }
  .group-controls { margin-top: 1.1rem; }
  .group-controls button { border-radius: 1.5rem; padding: .42rem .72rem; }
  .info { margin-top: .75rem; font-size: .8rem; }
  .count { margin: .35rem 0 0; color: #655965; font-size: .78rem; }
  .route-group { margin-top: 1.5rem; }
  h3 { font-size: 1.1rem; }
  h3 span { display: inline-block; vertical-align: middle; margin-left: .23rem; padding: .12rem .46rem; border-radius: 10px; background: #f1eaf0; color: #715068; font: 600 .72rem/1.2 var(--mono, monospace); }
  ul { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 16rem), 1fr)); gap: .55rem; padding: 0; margin: .6rem 0 0; list-style: none; }
  li { min-width: 0; border: 1px solid #e5e0e7; padding: .83rem .94rem; border-radius: 10px; }
  a { color: #962452; text-underline-offset: .18em; }
  .route-title { font-size: .94rem; font-weight: 680; overflow-wrap: anywhere; }
  li p { font-size: .81rem; }
  li small { font-size: .69rem; color: #79556b; }
  .no-results { margin-top: 1rem; padding: 1rem; border: 1px dashed #d5cdd8; border-radius: 10px; }
  .no-results button { margin-top: .8rem; }
</style>
