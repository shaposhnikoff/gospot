<script lang="ts">
  let { demo = false }: { demo?: boolean } = $props();

  type Usage = { totalBytes: number; usedBytes: number; availableBytes: number };
  const stateLabels = {
    active: 'Работает', inactive: 'Остановлена', failed: 'Ошибка', activating: 'Запускается',
    deactivating: 'Останавливается', reloading: 'Перезагружается', masked: 'Заблокирована',
    'not-found': 'Не установлена', unknown: 'Нет данных'
  };
  type Service = { name: string; state: keyof typeof stateLabels };
  type Snapshot = {
    at: string; hostname: string; model: string; kernel: string;
    uptimeSeconds: number | null; loadAverage: number[] | null; cpuTempC: number | null;
    memory: Usage | null; swap: Usage | null; rootDisk: Usage | null; services: Service[];
  };
  const example: Snapshot = {
    at: '2026-02-12T09:30:00Z', hostname: 'gospot-demo', model: 'Raspberry Pi 4 Model B Rev 1.5',
    kernel: '6.12.47+rpt-rpi-v8', uptimeSeconds: 279641, loadAverage: [0.18, 0.12, 0.09], cpuTempC: 46.7,
    memory: { totalBytes: 1958735872, usedBytes: 320786432, availableBytes: 1637949440 },
    swap: { totalBytes: 104857600, usedBytes: 0, availableBytes: 104857600 },
    rootDisk: { totalBytes: 30333206528, usedBytes: 3591372800, availableBytes: 25196232704 },
    services: [
      { name: 'MMDVMHost', state: 'active' }, { name: 'DMRGateway', state: 'active' },
      { name: 'GoSpot Panel', state: 'active' }, { name: 'DStarRepeater', state: 'not-found' },
      { name: 'ircDDBGateway', state: 'not-found' }, { name: 'TimeServer', state: 'not-found' },
      { name: 'GoSpot Watchdog', state: 'not-found' }, { name: 'GoSpot Remote', state: 'not-found' }
    ]
  };
  const missing = 'Нет данных';
  const finite = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
  const count = (v: unknown): v is number => finite(v) && Number.isSafeInteger(v) && v >= 0;
  const record = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object';
  function usage(v: unknown): v is Usage | null {
    return v === null || (record(v) && count(v.totalBytes) && count(v.usedBytes) && count(v.availableBytes)
      && v.availableBytes <= v.totalBytes - v.usedBytes);
  }
  function snapshot(v: unknown): v is Snapshot {
    return record(v) && typeof v.at === 'string' && Number.isFinite(Date.parse(v.at))
      && ['hostname', 'model', 'kernel'].every(k => typeof v[k] === 'string')
      && (v.uptimeSeconds === null || count(v.uptimeSeconds)) && (v.cpuTempC === null || finite(v.cpuTempC))
      && (v.loadAverage === null || (Array.isArray(v.loadAverage) && v.loadAverage.length === 3 && v.loadAverage.every(n => finite(n) && n >= 0)))
      && usage(v.memory) && usage(v.swap) && usage(v.rootDisk)
      && Array.isArray(v.services) && v.services.every(s => record(s) && typeof s.name === 'string'
        && typeof s.state === 'string' && Object.hasOwn(stateLabels, s.state));
  }

  let live = $state<Snapshot | null>(null);
  let failed = $state(false);
  let auto = $state(true);
  let data = $derived(demo ? example : live);
  $effect(() => {
    live = null;
    failed = false;
    if (demo) return;
    const ctrl = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function update() {
      try {
        const res = await fetch('/manage/api/system', {
          headers: { Accept: 'application/json' }, cache: 'no-store', mode: 'same-origin',
          signal: AbortSignal.any([ctrl.signal, AbortSignal.timeout(8000)])
        });
        if (!res.ok) throw new Error();
        const json: unknown = await res.json();
        if (!snapshot(json)) throw new Error();
        if (!ctrl.signal.aborted) { live = json; failed = false; }
      } catch {
        if (!ctrl.signal.aborted) failed = true;
      } finally {
        if (!ctrl.signal.aborted) schedule();
      }
    }
    function schedule() {
      timer = setTimeout(() => { if (auto) void update(); else schedule(); }, 15000);
    }
    void update();
    return () => { ctrl.abort(); clearTimeout(timer); };
  });
  const number = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 1 });
  function bytes(n: number) {
    const units = ['Б', 'КиБ', 'МиБ', 'ГиБ', 'ТиБ'];
    let i = 0;
    while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
    return `${number.format(n)} ${units[i]}`;
  }
  function uptime(n: number | null) {
    if (n === null) return missing;
    if (n < 60) return `${n} с`;
    return `${Math.floor(n / 86400)} д ${Math.floor(n / 3600) % 24} ч ${Math.floor(n / 60) % 60} мин`;
  }
  const resources = [['memory', 'Оперативная память'], ['swap', 'Подкачка'], ['rootDisk', 'Корневой раздел (/)']] as const;
</script>

<section class="system" aria-label="Сведения о системе">
  <div class="heading">
    <h2>Сведения о системе</h2>
    {#if demo}<strong class="demo">Демо · пример данных</strong>
    {:else}<label><input type="checkbox" bind:checked={auto} /> Обновлять каждые 15 с</label>{/if}
  </div>
  <p role="status">{failed && !demo ? `Не удалось получить данные станции.${live ? ' Показан последний полученный снимок.' : ''}` : !data ? 'Получаем данные станции…' : ''}</p>
  {#if data}
    <p class="stamp">{demo ? 'Время демоснимка' : 'Снимок станции'}:
      <time datetime={data.at}>{new Date(data.at).toLocaleString('ru-RU')}</time>
    </p>
    <h3>Устройство</h3>
    <dl class="metrics">
      <div><dt>Имя узла</dt><dd>{data.hostname || missing}</dd></div>
      <div><dt>Модель</dt><dd>{data.model || missing}</dd></div>
      <div><dt>Ядро Linux</dt><dd>{data.kernel || missing}</dd></div>
      <div><dt>Время работы</dt><dd>{uptime(data.uptimeSeconds)}</dd></div>
      <div><dt>Средняя нагрузка · 1 / 5 / 15 мин</dt><dd>{data.loadAverage?.map(n => n.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })).join(' / ') ?? missing}</dd></div>
      <div><dt>Температура процессора</dt><dd>{data.cpuTempC === null ? missing : `${number.format(data.cpuTempC)} °C / ${number.format(data.cpuTempC * 9 / 5 + 32)} °F`}</dd></div>
    </dl>
    <h3>Память и хранилище</h3>
    <dl class="metrics">
      {#each resources as [key, label]}
        {@const value = data[key]}
        <div><dt>{label}</dt><dd>
          {#if !value}{missing}
          {:else if key === 'swap' && value.totalBytes === 0}Не используется
          {:else}
            {bytes(value.usedBytes)} из {bytes(value.totalBytes)}
            <small>Занято: {number.format(100 * value.usedBytes / value.totalBytes)} % · Доступно: {bytes(value.availableBytes)}</small>
          {/if}
        </dd></div>
      {/each}
    </dl>
    <h3>Службы</h3>
    <dl class="services">
      {#each data.services as svc}
        <div><dt>{svc.name}</dt><dd class:active={svc.state === 'active'} class:bad={svc.state === 'failed'}>{stateLabels[svc.state]}</dd></div>
      {/each}
    </dl>
  {/if}
</section>

<style>
  .system { padding: clamp(1rem, 3vw, 1.75rem); margin-block: 1rem; color: #282532; background: white; border: 1px solid #ddd9e2; border-radius: .85rem; font: 1rem/1.5 system-ui, sans-serif; overflow-wrap: anywhere; }
  .heading { display: flex; align-items: center; justify-content: space-between; gap: .6rem; flex-wrap: wrap; }
  h2, h3, p, dl, dd { margin: 0; }
  h2 { font-size: 1.45rem; } h3 { font-size: 1.05rem; margin: 1.1rem 0 .45rem; }
  .demo, label, .stamp, dt, small { font-size: .85rem; }
  .demo { border-radius: .4rem; padding: .2rem .65rem; background: #f5e6ed; color: #7a1945; }
  label { display: inline-flex; gap: .4rem; align-items: center; cursor: pointer; }
  input:focus-visible { outline: 2px solid #852551; outline-offset: 3px; }
  .stamp, small, .metrics dt { color: #595661; }
  p:not(:empty) { margin-top: .5rem; }
  dl { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 14rem), 1fr)); gap: .45rem; }
  dl div { border-radius: .45rem; padding: .65rem .8rem; background: #f7f6f8; }
  dd { font-weight: 600; font-variant-numeric: tabular-nums; } small { display: block; font-weight: 400; }
  .services div { display: flex; gap: .5rem; align-items: baseline; justify-content: space-between; flex-wrap: wrap; }
  .services dd { font-size: .85rem; } .active { color: #176041; } .bad { color: #a12439; }
</style>
