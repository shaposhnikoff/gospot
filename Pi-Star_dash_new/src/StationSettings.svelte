<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { readJSON } from './runtime';

  const fields = [
    { key: 'callsign', label: 'Позывной', hint: '1–10 заглавных латинских букв и цифр.' },
    { key: 'radio_id', label: 'DMR ID', hint: 'От 1 до 16 777 215.' },
    { key: 'frequency', label: 'Частота RX/TX, Гц', hint: 'Мастер включает симплекс: одна частота для приёма и передачи, 1–1300 МГц.' },
    { key: 'uart', label: 'Устройство UART', hint: 'GPIO: /dev/ttyAMA0. USB: ttyUSB, ttyACM или путь /dev/serial/by-id/.' },
    { key: 'claim', label: 'ID подключения (claim)', hint: 'От 1 до 4 294 967 295.' },
    { key: 'master', label: 'Мастер-сервер', hint: 'Имя узла или IPv4-адрес без порта.' },
    { key: 'port', label: 'Порт', hint: 'От 1 до 65 535.' }
  ] as const;
  type Key = typeof fields[number]['key'];
  type Config = { callsign: string; radio_id: number; frequency: number; uart: string; claim: number; master: string; port: number };
  type Draft = Record<Key, string>;
  const keys = fields.map(f => f.key);
  const limits = { radio_id: [1, 16777215], frequency: [1000000, 1300000000], claim: [1, 4294967295], port: [1, 65535] } as const;
  const scope = 'pi-star.station-settings';
  function object(v: unknown): v is Record<string, unknown> {
    return typeof v === 'object' && v !== null && !Array.isArray(v);
  }
  function exact(v: Record<string, unknown>, names: readonly string[]) {
    return Object.keys(v).length === names.length && names.every(k => Object.hasOwn(v, k));
  }
  function numeric(k: Key): k is keyof typeof limits { return Object.hasOwn(limits, k); }
  function validHost(host: string) {
    const labels = host.split('.');
    if (/^\d+$/.test(labels[labels.length - 1])) {
      return labels.length === 4 && labels.every(s => /^(0|[1-9]\d{0,2})$/.test(s) && Number(s) <= 255);
    }
    return host.length <= 253 && labels.every(s => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(s));
  }
  function validValue(k: Key, v: unknown) {
    if (numeric(k)) return typeof v === 'number' && Number.isInteger(v) && v >= limits[k][0] && v <= limits[k][1];
    if (typeof v !== 'string' || /[^ -~]/.test(v)) return false;
    if (k === 'callsign') return /^[A-Z0-9]{1,10}$/.test(v);
    if (k === 'master') return validHost(v);
    return v.length <= 1024 && /^\/dev\/(?:ttyAMA0|tty(?:USB|ACM)\d+|serial\/by-id\/[A-Za-z0-9][A-Za-z0-9_.:+-]{0,254})$/.test(v);
  }
  function checked(v: unknown): Config {
    if (!object(v) || !exact(v, keys)) throw new Error('Нужны ровно семь открытых параметров станции.');
    for (const f of fields) if (!validValue(f.key, v[f.key])) throw new Error(`Проверьте поле «${f.label}». ${f.hint}`);
    return v as Config;
  }
  function toDraft(cfg?: Config): Draft {
    return Object.fromEntries(keys.map(k => [k, cfg ? String(cfg[k]) : ''])) as Draft;
  }
  function fromDraft(d: Draft) {
    return checked(Object.fromEntries(keys.map(k => [k, numeric(k) && /^\d+$/.test(d[k]) ? Number(d[k]) : d[k]])));
  }
  function decodeBackup(text: string) {
    let v: unknown;
    try { v = JSON.parse(text); } catch { throw new Error('Не удалось прочитать JSON. Проверьте формат файла.'); }
    if (!object(v) || !exact(v, ['scope', 'version', 'config']) || v.scope !== scope || v.version !== 1)
      throw new Error('Нужна копия параметров станции Pi-Star версии 1.');
    return checked(v.config);
  }

  let saved = $state<Config | null>(null);
  let draft = $state<Draft>(toDraft());
  let preview = $state<Config | null>(null);
  let csrf = $state('');
  let password = $state('');
  let busy = $state(true);
  let confirmRollback = $state(false);
  let notice = $state('Читаем настройки станции…');
  let error = $state('');
  let fileInput = $state<HTMLInputElement>();
  let form = $state<HTMLFormElement>();
  const lifetime = new AbortController();
  const request = { mode: 'same-origin', credentials: 'same-origin', cache: 'no-store', signal: lifetime.signal } as const;
  const ready = $derived(!!saved && !!csrf && !busy);
  const dirty = $derived(!!saved && (keys.some(k => draft[k] !== String(saved![k])) || !!password));

  class WizardError extends Error {
    readonly status: number;
    constructor(status: number) {
      super(({
        400: 'Станция отклонила параметры. Проверьте значения полей.',
        401: 'Требуется вход. Повторите чтение и введите учётные данные в окне браузера.',
        403: 'Сеанс изменился. Прочитайте настройки заново.',
        502: 'Мастер не выполнил операцию. Проверьте настройки и журнал службы.',
        503: 'Мастер временно недоступен или занят. Попробуйте позже.'
      } as Record<number, string>)[status] ?? `Мастер вернул ошибку HTTP ${status}.`);
      this.status = status;
    }
  }
  async function readConfig() {
    const r = await fetch('/manage/api/config', { ...request, redirect: 'error', headers: { Accept: 'application/json' } });
    if (!r.ok) throw new WizardError(r.status);
    const data: unknown = await r.json();
    if (!object(data) || typeof data.csrf !== 'string' || !data.csrf) throw new Error();
    const cfg = checked(data.config);
    csrf = data.csrf;
    saved = cfg;
    draft = toDraft(cfg);
    preview = null;
    password = '';
    confirmRollback = false;
    return cfg;
  }
  async function load() {
    busy = true;
    csrf = ''; error = ''; notice = 'Читаем настройки станции…';
    try {
      if (location.protocol !== 'https:') {
        const runtime = await readJSON('/runtime.json');
        if (!['localhost', '127.0.0.1', '[::1]'].includes(location.hostname) || !object(runtime) || runtime.preview !== true)
          throw new Error();
      }
      await readConfig();
      notice = 'Получены текущие настройки станции.';
    } catch (e) {
      notice = '';
      error = e instanceof WizardError ? e.message : 'Не удалось прочитать настройки. Проверьте HTTPS-подключение к мастеру станции.';
    } finally { busy = false; }
  }
  onMount(() => { void load(); return () => lifetime.abort(); });

  async function submit(action: 'apply' | 'rollback') {
    if (!ready || preview || (action === 'rollback' && !confirmRollback)) return;
    error = ''; notice = '';
    const body = new URLSearchParams({ action, csrf });
    let expected: Config | undefined;
    if (action === 'apply') {
      try { expected = fromDraft(draft); } catch (e) { error = (e as Error).message; return; }
      if (password.length > 128 || /[^ -~]/.test(password)) { error = 'Пароль сети: до 128 печатных символов ASCII.'; return; }
      for (const k of keys) body.set(k, String(expected[k]));
      body.set('password', password);
    }
    busy = true; confirmRollback = false; password = '';
    notice = action === 'apply' ? 'Применяем параметры…' : 'Восстанавливаем предыдущие настройки…';
    let acknowledged = false;
    try {
      const r = await fetch('/', { ...request, method: 'POST', redirect: 'manual',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body });
      if (r.type !== 'opaqueredirect' && !(r.status === 303 && r.headers.get('Location') === `/?done=${action}`)) {
        if (!r.ok) throw new WizardError(r.status);
        throw new Error();
      }
      acknowledged = true;
      const actual = await readConfig();
      if (expected && keys.some(k => actual[k] !== expected![k])) {
        error = 'Параметры на станции отличаются от отправленных. В форме показаны фактические значения.';
        notice = ''; return;
      }
      notice = action === 'apply' ? 'Настройки применены и перечитаны.' : 'Предыдущие настройки восстановлены и перечитаны.';
    } catch (e) {
      notice = '';
      if (acknowledged || !(e instanceof WizardError) || e.status === 401 || e.status === 403) csrf = '';
      error = acknowledged ? 'Мастер принял запрос, но перечитать параметры не удалось. Проверьте текущее состояние станции.'
        : e instanceof WizardError ? e.message : 'Связь прервалась. Результат неизвестен: прочитайте настройки перед повторной записью.';
    } finally { busy = false; }
  }

  function exportBackup() {
    if (!ready || !saved) return;
    const config = Object.fromEntries(keys.map(k => [k, saved![k]]));
    const blob = new Blob([JSON.stringify({ scope, version: 1, config }, null, 2) + '\n'], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = `pi-star-${saved.callsign}.json`; a.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  async function importBackup(input: HTMLInputElement) {
    const file = input.files?.[0];
    input.value = '';
    if (!ready || !file) return;
    preview = null; confirmRollback = false; busy = true; error = ''; notice = '';
    try {
      if (file.size > 16384) throw new Error('Файл слишком большой. Максимум — 16 КиБ.');
      preview = decodeBackup(await file.text());
      notice = 'Копия проверена. Сравните значения перед переносом.';
    } catch (e) { error = e instanceof Error ? e.message : 'Не удалось прочитать файл.'; }
    finally { busy = false; }
  }
  async function usePreview() {
    if (!preview) return;
    draft = toDraft(preview); preview = null; password = '';
    notice = 'Значения перенесены в форму. Для записи нажмите «Применить настройки».';
    await tick();
    form?.querySelector<HTMLInputElement>('[name="callsign"]')?.focus();
  }
</script>

<section class="settings" data-testid="station-settings" aria-labelledby="station-title">
  <header><p class="kicker">КОНФИГУРАЦИЯ PI-STAR</p><h1 id="station-title">Настройки станции</h1>
    <p>Параметры радиомодема и подключения к сети DMR.</p></header>
  <div class="messages">
    <p role="status" data-testid="settings-status">{notice}</p>
    {#if error}<p role="alert" data-testid="settings-error">{error}</p>{/if}
    {#if !csrf && !busy}<button type="button" data-testid="settings-reload" onclick={load}>Прочитать настройки заново</button>{/if}
  </div>
  {#if saved}
    <form bind:this={form} method="post" action="/" autocomplete="off" onsubmit={(e) => { e.preventDefault(); void submit('apply'); }}>
      {#each ['Радиостанция', 'Сеть DMR'] as group, i}
        <fieldset disabled={!ready || !!preview || confirmRollback}><legend>{group}</legend>
          <div class="grid">
            {#each (i === 0 ? fields.slice(0, 4) : fields.slice(4)) as field}
              <label><span>{field.label}</span>
                <input required name={field.key} data-testid={'settings-' + field.key}
                  inputmode={numeric(field.key) ? 'numeric' : 'text'} spellcheck="false"
                  aria-describedby={'hint-' + field.key} bind:value={draft[field.key]} />
                <small id={'hint-' + field.key}>{field.hint}</small>
              </label>
            {/each}
            {#if i === 1}<label><span>Новый пароль сети</span>
              <input type="password" name="password" data-testid="settings-password" maxlength="128" autocomplete="new-password" bind:value={password} aria-describedby="password-hint" />
              <small id="password-hint">Оставьте пустым, чтобы сохранить текущий пароль.</small>
            </label>{/if}
          </div>
        </fieldset>
      {/each}
      <p class="restart"><strong>Перерыв связи.</strong> При применении или откате изменённых настроек мастер перезапустит затронутые службы MMDVMHost и DMRGateway. Дождитесь окончания передачи.</p>
      <div class="actions"><button class="primary" type="submit" data-testid="settings-apply" disabled={!ready || !!preview || confirmRollback}>Применить настройки</button>
        {#if dirty}<span>Есть несохранённые изменения</span>{/if}</div>
    </form>

    <section class="card" aria-labelledby="copy-title">
      <p class="kicker">ПЕРЕНОС ПАРАМЕТРОВ</p><h2 id="copy-title">Копия настроек</h2>
      <p>В JSON входят семь сохранённых параметров станции. Пароль сети в файл не записывается.</p>
      <div class="actions"><button type="button" disabled={!ready} data-testid="settings-export" onclick={exportBackup}>Скачать JSON</button>
        <button type="button" disabled={!ready || confirmRollback} data-testid="settings-import" onclick={() => fileInput?.click()}>Открыть JSON…</button>
        <input type="file" accept=".json,application/json" hidden bind:this={fileInput} data-testid="settings-import-file" onchange={e => void importBackup(e.currentTarget)} /></div>
      {#if preview}
        <div class="preview" data-testid="settings-preview"><h3>Предпросмотр копии</h3>
          <table><caption>Сравнение с сохранёнными параметрами станции</caption>
            <thead><tr><th scope="col">Параметр</th><th scope="col">Сейчас</th><th scope="col">Из файла</th></tr></thead>
            <tbody>{#each fields as f}<tr class:different={preview[f.key] !== saved[f.key]}><th scope="row">{f.label}</th><td>{saved[f.key]}</td><td>{preview[f.key]}</td></tr>{/each}</tbody>
          </table>
          <p>Перенос заменит значения в форме. На станции ничего не изменится до применения.</p>
          <div class="actions"><button type="button" data-testid="settings-use-preview" onclick={usePreview}>Перенести в форму</button>
            <button type="button" onclick={() => { preview = null; notice = 'Импорт отменён.'; }}>Отмена</button></div>
        </div>
      {/if}
    </section>
    <section class="card" aria-labelledby="rollback-title"><p class="kicker">ВОССТАНОВЛЕНИЕ</p><h2 id="rollback-title">Откат изменений</h2>
      <p>Вернуть состояние перед последней записью на станции. После восстановления снимок отката больше не доступен.</p>
      {#if confirmRollback}
        <p class="restart">Восстановить предыдущие настройки? Черновик будет сброшен, связь может прерваться.</p>
        <div class="actions"><button type="button" data-testid="settings-confirm-rollback" onclick={() => void submit('rollback')}>Подтвердить откат</button>
          <button type="button" onclick={() => confirmRollback = false}>Отмена</button></div>
      {:else}
        <button type="button" data-testid="settings-rollback" disabled={!ready || !!preview} onclick={() => confirmRollback = true}>Восстановить предыдущие настройки…</button>
      {/if}
    </section>
  {/if}
</section>

<style>
  .settings { max-width: 60rem; margin: 0 auto; padding: 0 0 2rem; color: #292533; }
  .settings header, form, .card { margin-bottom: 1rem; }
  .settings h1, .settings h2, .settings h3 { margin: .25rem 0 .5rem; letter-spacing: -.035em; }
  .settings h1 { font-size: clamp(1.8rem, 5vw, 2.6rem); }
  .settings h2 { font-size: 1.3rem; }
  .settings p { margin: .4rem 0; line-height: 1.5; }
  .settings .kicker { font-weight: 750; letter-spacing: .12em; font-size: .68rem; color: #962152; }
  form, .card { border: 1px solid #e4dfe7; border-radius: 1rem; padding: clamp(1rem, 3vw, 1.65rem); background: #fff; }
  fieldset { padding: 0; border: 0; min-width: 0; margin: 0 0 1.5rem; }
  legend { padding: 0; margin-bottom: .8rem; font-weight: 700; font-size: 1.16rem; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 18rem), 1fr)); gap: .9rem 1.2rem; }
  label { display: grid; gap: .35rem; align-content: start; font-size: .92rem; font-weight: 620; }
  input:not([hidden]) { box-sizing: border-box; padding: .65rem .75rem; border-radius: .5rem; border: 1px solid #aaa3b1; width: 100%; min-width: 0; color: inherit; background: #fff; font: 1rem var(--mono, monospace); }
  small, .actions span, caption { font-weight: 400; font-size: .82rem; line-height: 1.45; color: #605968; }
  .settings button { min-height: 42px; border: 1px solid #c1b9c6; background: #fff; color: #403845; border-radius: .6rem; padding: .55rem .9rem; font: inherit; font-weight: 620; cursor: pointer; }
  .settings .primary { background: #a82459; color: white; border-color: #a82459; }
  .settings button:disabled { opacity: .55; cursor: default; }
  .settings :is(input, button):focus-visible { outline: 3px solid #bb3975; outline-offset: 2px; }
  .actions { display: flex; align-items: center; flex-wrap: wrap; gap: .55rem .85rem; margin-top: .8rem; }
  .restart, .messages [role="alert"] { border-radius: .6rem; padding: .7rem .9rem; background: #fff4d9; color: #694616; font-size: .9rem; }
  .messages { margin-bottom: 1rem; }
  .messages [role="status"] { color: #564e60; }
  .messages [role="alert"] { background: #ffebee; color: #842338; }
  .preview { margin-top: 1rem; padding-top: .6rem; border-top: 1px solid #dfd9e3; }
  .preview table { table-layout: fixed; width: 100%; border-collapse: collapse; }
  .preview caption { text-align: left; padding-bottom: .5rem; }
  .preview th, .preview td { text-align: left; white-space: normal; overflow-wrap: anywhere; padding: .5rem .35rem; font-size: .84rem; border-bottom: 1px solid #e4dfe7; }
  .different { background: #fff4fa; }
</style>
