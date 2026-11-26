<script lang="ts">
  import { onMount, tick } from 'svelte';
  import Console from './Console.svelte';
  import LiveHeard from './LiveHeard.svelte';
  import { asSnapshot, detectRuntime, readJSON } from './runtime';
  import { fetchSnapshot, scenarios, type Scenario } from './data';
  import './app.css';

  type Snapshot = Awaited<ReturnType<typeof fetchSnapshot>>;
  type Filter = 'all' | 'RF' | 'Net';

  let scenario = $state<Scenario>('online');
  let snapshot = $state<Snapshot | null>(null);
  let pending = $state(false);
  let offline = $state(false);
  let automatic = $state(true);
  let filter = $state<Filter>('all');
  let generation = 0;
  const installed = location.pathname === '/operator' || location.pathname.startsWith('/operator/');
  let runtime = $state<'pi'|'demo'|'pending'|'error'>(installed ? 'pi' : 'pending');
  let classicPort = $state<number|null>(null);
  function currentView() { return installed || location.hash === '#console' ? 'console' : 'overview'; }
  let view = $state(currentView());
  const filters: { id: Filter; label: string }[] = [
    { id: 'all', label: 'Все' }, { id: 'RF', label: 'RF' }, { id: 'Net', label: 'Сеть' }
  ];
  const visibleHeard = $derived((snapshot?.Heard ?? []).filter((entry) => filter === 'all' || entry.Source === filter));
  const statusPanels = $derived([
    { id: 'modem', title: 'Модем', icon: 'chip', value: snapshot?.Modem },
    { id: 'master', title: 'Мастер-сервер', icon: 'server', value: snapshot?.Master }
  ]);
  const clock = new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
  const calendar = new Intl.DateTimeFormat('ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric' });
  const time = (iso?: string) => iso && Number.isFinite(Date.parse(iso)) ? clock.format(new Date(iso)) : '—';
  const date = (iso?: string) => iso && Number.isFinite(Date.parse(iso)) ? calendar.format(new Date(iso)) : '—';
  const metric = (n?: number, suffix = '') => typeof n === 'number' && Number.isFinite(n) ? `${n.toLocaleString('ru-RU')}${suffix}` : '—';
  const frequency = (v?: string | number) => {
    if (v === undefined || v === null || v === '') return '—';
    const n = Number(v);
    if (!Number.isFinite(n)) return String(v);
    return `${(n >= 1e6 ? n / 1e6 : n).toFixed(5)} МГц`;
  };

  async function refresh() {
    const id = ++generation;
    pending = true;
    try {
      if(runtime === 'error' || runtime === 'pending') {
        const config = await detectRuntime();
        runtime = config.mode;
        classicPort = config.classicPort;
      }
      const result = runtime === 'pi' ? asSnapshot(await readJSON('/api/status')) : await fetchSnapshot(scenario);
      if (id !== generation) return;
      snapshot = result;
      offline = false;
    } catch {
      if (id === generation) {offline = true; if(runtime === 'pending') runtime = 'error';}
    } finally {
      if (id === generation) pending = false;
    }
  }

  onMount(() => {
    const sync = () => {
      view = currentView();
      if (!installed && !snapshot && !pending) void refresh();
      if (['#heard-all','#heard-rf'].includes(location.hash)) {
        filter = location.hash==='#heard-rf'?'RF':'all';
        void tick().then(()=>document.getElementById('heard-title')?.scrollIntoView({block:'start'}));
      }
    };
    sync();
    window.addEventListener('hashchange', sync);
    const timer = window.setInterval(() => { if (view === 'overview' && automatic && !pending) void refresh(); }, 5000);
    return () => { generation++; window.clearInterval(timer); window.removeEventListener('hashchange', sync); };
  });

  const paths: Record<string, string> = {
    signal: 'M3 19v-3m5 3v-7m5 7V8m5 11V4',
    radio: 'M9 21l3-12 3 12m-5-4h4M8 6a6 6 0 0 0 0 8m8-8a6 6 0 0 1 0 8M5 3a10 10 0 0 0 0 14M19 3a10 10 0 0 1 0 14',
    refresh: 'M20 7v5h-5M4 17v-5h5m10.3-1A7.5 7.5 0 0 0 6 6.5m-1.3 6.6A7.5 7.5 0 0 0 18 17.5',
    chip: 'M7 7h10v10H7zM10 3v4m4-4v4m-4 10v4m4-4v4M3 10h4m-4 4h4m10-4h4m-4 4h4',
    server: 'M4 4h16v6H4zM4 14h16v6H4zM7 7h1m-1 10h1',
    activity: 'M2 12h5l3-8 4 16 3-8h5',
    settings: 'M3 7h9m5 0h4M3 17h3m5 0h10M12 4h5v6h-5zM6 14h5v6H6z',
    chevron: 'm7 10 5 5 5-5',
    clock: 'M12 3a9 9 0 1 0 .01 0M12 7v5l3 2',
    layers: 'm3 8 9-5 9 5-9 5zM3 12l9 5 9-5M3 16l9 5 9-5',
    network: 'M3 7h14m-4-4 4 4-4 4M21 17H7m4-4-4 4 4 4',
    alert: 'm12 3 10 17H2zM12 9v4m0 3v.2',
    flask: 'M9 3h6m-5 0v7L4.5 19a1 1 0 0 0 1 2h13a1 1 0 0 0 1-2L14 10V3M8 15h8'
  };
</script>

{#snippet icon(name: string, size = 18)}
  <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d={paths[name]} /></svg>
{/snippet}

<svelte:head><title>Pi-Star · Пульт оператора</title><meta name="theme-color" content="#22202e" /></svelte:head>

<header class="masthead"><div class="mast-inner">
  <div class="brand"><span class="brand-mark">{@render icon('radio', 23)}</span><strong>Pi-Star</strong><span class="brand-divider"></span><span class="brand-sub">Пульт оператора</span></div>
  <span class="demo-badge"><i></i>{installed ? 'Pi-Star' : runtime === 'pi' ? `В эфире · ${snapshot?.Node || 'Raspberry Pi'}` : runtime === 'demo' ? 'Локальный прототип' : 'Подключение'}</span>
</div></header>

<main>
  <nav class="workspace-tabs" aria-label="Рабочая область">
    {#if installed}<a href="/">Статус узла</a>{:else}<a href="#overview" aria-current={view === 'overview' ? 'page' : undefined}>Обзор узла</a>{/if}
    {#if installed || runtime==='pi'}<a href="/admin/configure.php" data-testid="open-radio-settings">Настроить MMDVMHost</a>{/if}
    <a href="#console" aria-current={view === 'console' ? 'page' : undefined}>Все функции Pi-Star</a>
    <a href="./hoseline.html">Слушать TG · Hoseline</a>
  </nav>
  {#if view === 'console'}
    <Console pi={!installed && runtime === 'pi'} {classicPort} />
    {#if installed}<LiveHeard />{/if}
  {:else}
  <div class="view-heading">
    <div><div class="eyebrow"><span></span> РАДИОМОНИТОРИНГ</div><h1>Обзор узла</h1></div>
    <div class="refresh-controls">
      <button class="auto" role="switch" aria-checked={automatic} data-testid="auto-refresh" onclick={() => automatic = !automatic}><span class:enabled={automatic} class="switch"><i></i></span>Авто · 5 с</button>
      <button class="manual" data-testid="refresh" onclick={() => void refresh()}><span class:spinning={pending}>{@render icon('refresh', 16)}</span>Обновить</button>
    </div>
  </div>

  {#if offline}
    <div class="warning" role="alert" data-testid="stale-banner">{@render icon('alert')}<div><strong>{runtime === 'demo' ? 'Демо-API недоступен.' : 'API узла недоступен.'}</strong> {snapshot ? `Показаны последние полученные данные: ${date(snapshot.UpdatedAt)}, ${time(snapshot.UpdatedAt)}.` : 'Получить первый снимок не удалось.'}</div></div>
  {/if}

  <section class="overview" aria-label="Состояние узла">
    <div class="hero">
      <svg class="hero-art" viewBox="0 0 335 200" fill="none" aria-hidden="true">
        <defs>
          <pattern id="dots" width="10" height="10" patternUnits="userSpaceOnUse"><circle cx="1" cy="1" r=".8" fill="#fff" opacity=".32"/></pattern>
          <linearGradient id="haze" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#f89ec4" stop-opacity=".5"/><stop offset="1" stop-color="#f89ec4" stop-opacity=".02"/></linearGradient>
        </defs>
        <path d="M0 0h335v200H0z" fill="url(#dots)"/>
        <g stroke="white" opacity=".13"><circle cx="223" cy="67" r="37"/><circle cx="223" cy="67" r="61"/><circle cx="223" cy="67" r="88"/><circle cx="223" cy="67" r="117"/></g>
        <path d="m0 183 58-20 27 2 28-14 45 4 30-16 31 4 17-6 30 4 69-19v78H0z" fill="url(#haze)"/>
        <path d="m0 197 46-13 28 2 56-14 28 4 50-15 46 5 48-17 33 4v47H0z" fill="#691137" opacity=".65"/>
        <g stroke="#ffdbeb" stroke-width="2.1" stroke-linejoin="round"><path d="m196 173 27-103 28 105M203 147l40 1-34-24h27l-20-23h14m-18 23 18-23m-27 46 33-23m-40 49 47-25m-40-1 48 28"/><path d="M223 70V53m-13 3h26m-10-6h7"/></g>
        <circle cx="223" cy="65" r="4" fill="#fff"/>
        <g stroke="#ffc9e0" stroke-width="2" stroke-linecap="round"><path d="M213 56q-8 9 0 18m20-18q8 9 0 18m-27-25q-15 16 0 32m34-32q15 16 0 32"/></g>
        <path d="m0 198 52-7 51 3 66-13 39-9 33 3 32-4 62 8v21H0z" fill="#53132e" opacity=".7"/>
      </svg>
      <div class="hero-content"><div class="hero-label">{@render icon('radio', 16)} РАДИОУЗЕЛ</div>
        <h2 data-testid="callsign">{snapshot?.Radio.Callsign || '—'}</h2>
        <div class="hero-meta">DMR ID <b>{snapshot?.Radio.ID || '—'}</b></div>
        <div class="modes">{#each snapshot?.Radio.Modes ?? [] as mode}<span>{mode}</span>{/each}</div>
      </div>
    </div>
    <div class="states">{#each statusPanels as panel}
      <article class="state-card" data-testid={'status-' + panel.id} data-state={panel.value?.State ?? 'unknown'}>
        <div class="state-head"><span class="tile-icon">{@render icon(panel.icon)}</span><h2>{panel.title}</h2><span class={'pill ' + (panel.value?.State ?? 'unknown')}><i></i>{panel.value?.State === 'up' ? 'На связи' : panel.value?.State === 'down' ? 'Нет связи' : 'Неизвестно'}</span></div>
        <p>{panel.value?.Detail || 'Нет данных'}</p>
        <div class="state-foot"><span>{panel.value?.Source || '—'}</span><time datetime={panel.value?.At} title={date(panel.value?.At)}>{@render icon('clock', 12)}{time(panel.value?.At)}</time></div>
      </article>
    {/each}</div>
  </section>

  <div class="details">
    <section class="panel journal" aria-labelledby="heard-title">
      <div class="panel-head"><div class="section-label">ЭФИРНАЯ АКТИВНОСТЬ</div><div class="journal-heading"><h2 id="heard-title">Последние передачи</h2>
        <div class="filters" aria-label="Источник передач">{#each filters as item}<button data-testid={'filter-' + item.id} class:active={filter === item.id} aria-pressed={filter === item.id} onclick={() => filter = item.id}>{item.label}</button>{/each}</div>
      </div></div>
      {#if snapshot?.HeardAvailable && visibleHeard.length}
        <div class="table-scroll"><table data-testid="heard-table"><thead><tr><th>Время</th><th>Источник</th><th>Откуда / Куда</th><th>Слот</th><th>Длит.</th><th>BER</th><th>Потери</th></tr></thead>
          <tbody>{#each visibleHeard as row}<tr data-testid="heard-row" data-source={row.Source}><td><time datetime={row.At} title={date(row.At)}>{time(row.At)}</time></td><td><span class:net={row.Source === 'Net'} class="source">{@render icon(row.Source === 'RF' ? 'signal' : 'network', 12)}{row.Source === 'RF' ? 'RF' : 'Сеть'}</span></td><td class="route"><strong>{row.From || '—'}</strong><small>→ {row.To || '—'}</small></td><td>{row.Slot || '—'}</td><td>{metric(row.Duration, ' с')}</td><td>{metric(row.BER, '%')}</td><td>{metric(row.Loss, '%')}</td></tr>{/each}</tbody>
        </table></div>
      {:else}
        <div class="empty" data-testid="heard-empty">{@render icon('activity', 26)}<strong>{!snapshot ? (pending ? 'Загрузка журнала…' : 'Нет данных') : !snapshot.HeardAvailable ? 'Журнал недоступен' : 'Передач пока нет'}</strong><span>{snapshot && !snapshot.HeardAvailable ? 'Не удалось прочитать журнал активности.' : snapshot && filter !== 'all' ? 'Для выбранного источника записей нет.' : 'Здесь появятся записи о передачах.'}</span></div>
      {/if}
    </section>
    <section class="panel parameters" aria-labelledby="params-title"><div class="panel-head"><div class="section-label">КОНФИГУРАЦИЯ</div><h2 id="params-title">Параметры узла</h2></div>
      <dl>{#each [
        ['Узел', snapshot?.Node], ['Режим работы', snapshot?.Radio.Operation], ['Приём · RX', frequency(snapshot?.Radio.RX)], ['Передача · TX', frequency(snapshot?.Radio.TX)], ['Цветовой код', snapshot?.Radio.CC], ['Таймслоты', snapshot?.Radio.Slots]
      ] as pair}<div><dt>{pair[0]}</dt><dd>{pair[1] ?? '—'}</dd></div>{/each}</dl>
    </section>
  </div>
  {#if runtime === 'demo'}<section class="simulation" aria-label="Демонстрация"><div class="sim-info"><span class="tile-icon">{@render icon('flask', 19)}</span><div><strong>Симулятор узла</strong><p>Локальные демоданные · без подключения к эфиру</p></div></div>
    <label for="scenario">Сценарий</label><div class="select-wrap"><select id="scenario" data-testid="scenario" value={scenario} onchange={(e) => { scenario = e.currentTarget.value as Scenario; void refresh(); }}>{#each scenarios as item}<option value={item.id}>{item.label}</option>{/each}</select>{@render icon('chevron', 15)}</div>
  </section>{/if}
  <footer><span>Pi-Star <b>/</b> Пульт оператора</span><span data-testid="last-success">{@render icon('clock', 13)}Последнее обновление: <time datetime={snapshot?.UpdatedAt} title={date(snapshot?.UpdatedAt)}>{time(snapshot?.UpdatedAt)}</time></span></footer>
  {/if}
</main>
