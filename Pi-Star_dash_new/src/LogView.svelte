<script lang="ts">
  import { onMount } from 'svelte';

  let { demo = false }: { demo?: boolean } = $props();
  type Row = { at: string; source: 'MMDVMHost' | 'DMRGateway'; level: string; message: string };
  const endpoint = '/manage/api/log';
  const clock = new Intl.DateTimeFormat('ru-RU', { dateStyle: 'short', timeStyle: 'medium' });
  const example: Row[] = [
    { at: '2026-01-24T12:04:25Z', source: 'MMDVMHost', level: 'message', message: 'DMR Slot 2, RF end of voice transmission, 2.4 seconds, BER: 0.1%' },
    { at: '2026-01-24T12:04:22Z', source: 'MMDVMHost', level: 'message', message: 'DMR Slot 2, received RF voice header' },
    { at: '2026-01-24T12:00:05Z', source: 'DMRGateway', level: 'message', message: 'Logged into the master successfully' }
  ];
  function isRow(v: unknown): v is Row {
    if (!v || typeof v !== 'object') return false;
    const row = v as Partial<Row>;
    return (row.source === 'DMRGateway' || row.source === 'MMDVMHost') &&
      typeof row.at === 'string' && Number.isFinite(Date.parse(row.at)) &&
      typeof row.level === 'string' && typeof row.message === 'string';
  }

  let rows = $state<Row[]>([]);
  let source = $state('all');
  let filter = $state('');
  let pending = $state(false);
  let paused = $state(false);
  let error = $state('');
  let updated = $state('');
  const visible = $derived(rows.filter(r => (source === 'all' || r.source === source) &&
    `${r.level} ${r.message}`.toLowerCase().includes(filter.trim().toLowerCase())));
  const demoDownload = 'data:text/plain;charset=utf-8,' + encodeURIComponent(example.map(r =>
    `${r.at}\t${r.source}\t${r.level}\t${r.message}\n`).join(''));
  const lifetime = new AbortController();

  async function refresh() {
    if (pending) return;
    pending = true;
    try {
      if (demo) {
        rows = example;
      } else {
        if (location.protocol !== 'https:' || !location.pathname.startsWith('/manage/')) throw new Error();
        const r = await fetch(endpoint, { mode: 'same-origin', credentials: 'same-origin', cache: 'no-store',
          redirect: 'error', headers: { Accept: 'application/json' },
          signal: AbortSignal.any([lifetime.signal, AbortSignal.timeout(8000)]) });
        if (r.status === 401 || r.status === 403) {
          rows = []; updated = ''; paused = true;
          error = 'Требуется вход администратора. Нажмите «Обновить», чтобы повторить запрос.';
          return;
        }
        if (!r.ok) throw new Error();
        const data = await r.json();
        if (!Array.isArray(data?.rows) || data.rows.length > 64 || !data.rows.every(isRow)) throw new Error();
        rows = data.rows;
      }
      updated = clock.format(new Date());
      error = '';
    } catch {
      if (!lifetime.signal.aborted) error = 'Не удалось прочитать журнал. Проверьте подключение к HTTPS-мастеру.';
    } finally { pending = false; }
  }
  function togglePause() {
    paused = !paused;
    if (!paused) void refresh();
  }
  onMount(() => {
    void refresh();
    const timer = window.setInterval(() => { if (!paused && !document.hidden) void refresh(); }, 5000);
    return () => { clearInterval(timer); lifetime.abort(); };
  });
</script>

<section class="logs" data-testid="log-view" aria-labelledby="log-title">
  <header><p class="kicker">ДИАГНОСТИКА GOSPOT</p><h1 id="log-title">Журнал радиомодема</h1>
    <p>{demo ? 'Демонстрационные данные.' : 'Текущая загрузка станции.'} Радиособытия из последних 64 записей MMDVMHost и DMRGateway, новые сверху.</p>
  </header>
  <div class="card">
    <div class="tools">
      <label>Источник<select bind:value={source} data-testid="log-source"><option value="all">Все источники</option>
        <option>MMDVMHost</option><option>DMRGateway</option></select></label>
      <label>Фильтр<input type="search" bind:value={filter} maxlength="128" placeholder="Уровень или текст события" data-testid="log-filter" /></label>
      <button type="button" onclick={togglePause} aria-pressed={paused} data-testid="log-pause">{paused ? 'Возобновить' : 'Пауза'}</button>
      <button type="button" disabled={pending} onclick={refresh} data-testid="log-refresh">Обновить</button>
      <a href={demo ? demoDownload : endpoint + '/download'} download={demo ? 'gospot-radio-demo.txt' : 'gospot-radio-log.txt'} data-testid="log-download">Скачать TXT</a>
    </div>
    <p class="meta">{paused ? 'Автообновление на паузе' : 'Автообновление каждые 5 с'}{updated ? ` · Снимок: ${updated}` : ''} · Показано: {visible.length} из {rows.length}</p>
    {#if error}<p class="error" role="alert">{error}{updated ? ' Показан последний полученный снимок.' : ''}</p>{/if}
    {#if visible.length}
      <div class="scroll"><table><caption>Отобранные события · время местное</caption>
        <thead><tr><th>Дата и время</th><th>Источник</th><th>Уровень</th><th>Событие</th></tr></thead>
        <tbody>{#each visible as row}<tr><td><time datetime={row.at}>{clock.format(new Date(row.at))}</time></td>
          <td>{row.source}</td><td>{row.level}</td><td>{row.message}</td></tr>{/each}</tbody>
      </table></div>
    {:else if pending && !updated}<p role="status">Читаем журнал…</p>
    {:else if updated}<p role="status">{rows.length ? 'Нет событий по выбранному фильтру.' : 'В последних записях нет распознанных радиособытий.'}</p>{/if}
    <p class="meta">TXT содержит свежий снимок обоих источников без экранного фильтра. Время в файле — UTC.</p>
  </div>
</section>

<style>
  .logs { max-width: 72rem; margin: 0 auto 2rem; color: #292533; }
  header { margin-bottom: 1rem; }
  h1 { font-size: clamp(1.8rem, 5vw, 2.6rem); margin: .25rem 0 .5rem; letter-spacing: -.035em; }
  p { line-height: 1.5; margin: .4rem 0; }
  .kicker { font-size: .7rem; font-weight: 750; letter-spacing: .12em; color: #962152; }
  .card { background: white; border: 1px solid #e4dfe7; border-radius: 1rem; padding: clamp(.75rem, 3vw, 1.5rem); }
  .tools { display: flex; flex-wrap: wrap; gap: .6rem; align-items: end; }
  label { display: grid; gap: .3rem; font-size: .85rem; font-weight: 650; max-width: 100%; }
  input, select, button, a { box-sizing: border-box; padding: .55rem .7rem; min-height: 42px; max-width: 100%; border: 1px solid #bcb3c2; border-radius: .5rem; background: white; font: inherit; color: inherit; }
  button, a { font-weight: 620; cursor: pointer; }
  a { display: inline-flex; align-items: center; color: #932153; }
  :is(input, select, button, a):focus-visible { outline: 3px solid #bb3975; outline-offset: 2px; }
  button[aria-pressed="true"] { background: #f6e6ed; }
  .meta, caption { font-size: .84rem; color: #605968; margin-top: .9rem; }
  .error { color: #842338; background: #ffebee; padding: .7rem; border-radius: .5rem; }
  .scroll { max-width: 100%; overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; font-size: .85rem; }
  caption { text-align: left; padding-bottom: .4rem; }
  th, td { text-align: left; padding: .55rem; border-bottom: 1px solid #e4dfe7; }
  td { font-family: var(--mono, monospace); }
  td:first-child { white-space: nowrap; }
</style>
