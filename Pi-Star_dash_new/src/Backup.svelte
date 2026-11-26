<script lang="ts">
  import { onMount } from 'svelte';

  const names = ['MMDVM-Host.ini', 'DMRGateway.ini'] as const;
  const maxBytes = 512 * 1024 + 1024;
  const lifetime = new AbortController();
  const request = { credentials: 'same-origin', mode: 'same-origin', cache: 'no-store', redirect: 'error', signal: lifetime.signal } as const;
  type Preview = { receipt: string; changed: string[] };
  let csrf = $state('');
  let selected = $state<File | null>(null);
  let preview = $state<Preview | null>(null);
  let busy = $state(true);
  let notice = $state('');
  let error = $state('');

  function message(status: number) {
    return ({
      401: 'Требуется вход в мастер станции.',
      403: 'Сеанс изменился. Обновите страницу и повторите проверку.',
      413: 'Файл слишком большой для архива станции.',
      422: 'Архив отклонён. Нужна неизменённая копия с этой станции. Если настройки изменились, повторите проверку.',
      502: 'Операция не выполнена. Проверьте конфигурацию и состояние служб перед повтором.',
      503: 'Мастер занят или временно недоступен. Попробуйте позже.'
    } as Record<number, string>)[status] ?? 'Мастер отклонил запрос.';
  }

  async function load() {
    busy = true; csrf = ''; error = '';
    try {
      if (location.protocol !== 'https:') throw new Error('Откройте мастер станции по HTTPS.');
      const r = await fetch('/manage/api/config', request);
      if (!r.ok) throw new Error(message(r.status));
      const data = await r.json();
      if (typeof data?.csrf !== 'string' || !/^[a-f0-9]{64}$/.test(data.csrf)) throw new Error('Не удалось открыть сеанс мастера.');
      csrf = data.csrf;
    } catch (e) {
      error = e instanceof Error && !(e instanceof TypeError || e instanceof SyntaxError) ? e.message : 'Нет связи с мастером станции.';
    } finally { busy = false; }
  }
  onMount(() => { void load(); return () => lifetime.abort(); });

  function choose(input: HTMLInputElement) {
    const f = input.files?.[0];
    input.value = '';
    preview = null; selected = null; error = ''; notice = '';
    if (!f) return;
    if (f.size === 0 || f.size > maxBytes) { error = 'Нужен непустой ZIP размером не более 513 КиБ.'; return; }
    selected = f;
  }

  async function submit(action: 'preview' | 'apply') {
    if (busy || !csrf || !selected || (action === 'apply' && !preview?.changed.length)) return;
    const file = selected;
    const headers: Record<string, string> = { 'Content-Type': 'application/zip', 'X-PiStar-Action': action, 'X-PiStar-CSRF': csrf };
    if (action === 'apply' && preview) headers['X-PiStar-Receipt'] = preview.receipt;
    preview = null; error = ''; busy = true;
    notice = action === 'preview' ? 'Проверяем подпись и состав архива…' : 'Восстанавливаем файлы и перезапускаем службы…';
    try {
      const r = await fetch('/manage/api/restore', { ...request, method: 'POST', headers, body: file });
      if (r.status === 401 || r.status === 403) csrf = '';
      if (!r.ok) { error = message(r.status); notice = ''; return; }
      const data = await r.json();
      if (action === 'preview') {
        if (!data || typeof data.receipt !== 'string' || !/^[a-f0-9]{64}$/.test(data.receipt) ||
          !Array.isArray(data.changed) || !data.changed.every((n: string) => names.some(allowed => n === allowed))) throw new Error();
        preview = { receipt: data.receipt, changed: data.changed };
        notice = 'Архив проверен. Файлы станции ещё не изменены.';
      } else {
        if (data?.ok !== true) throw new Error();
        notice = 'Файлы восстановлены. Затронутые службы перезапущены.';
        selected = null;
      }
    } catch {
      notice = '';
      error = action === 'apply' ? 'Ответ не получен. Проверьте состояние станции перед повторной попыткой.' : 'Не удалось проверить архив. Проверьте связь с мастером.';
    } finally { busy = false; }
  }
</script>

<section class="archive" aria-labelledby="archive-title" data-testid="station-backup">
  <p class="kicker">КОНФИГУРАЦИЯ PI-STAR</p>
  <h1 id="archive-title">Резервная копия станции</h1>
  <article>
    <h2>Скачать архив PRIVATE</h2>
    <p>В ZIP войдут полные файлы <code>/etc/MMDVM-Host.ini</code> и <code>/etc/DMRGateway.ini</code>, включая пароль сети DMR.</p>
    <p><strong>Архив не зашифрован.</strong> Храните его в защищённом месте. Для переноса открытых параметров используйте JSON в настройках станции.</p>
    <a class="button" href="/manage/api/backup" download="Pi-Star-PRIVATE.zip">Скачать PRIVATE ZIP</a>
  </article>
  <article>
    <h2>Восстановить из архива</h2>
    <p>Подойдёт только неизменённый ZIP, подписанный этой станцией. После замены ключа старые архивы не принимаются.</p>
    <label>Выберите PRIVATE ZIP
      <input type="file" accept=".zip,application/zip" disabled={!csrf || busy} onchange={e => choose(e.currentTarget)} />
    </label>
    {#if selected}<p>Выбран файл: {selected.name}</p>{/if}
    <button type="button" disabled={!csrf || !selected || busy} onclick={() => void submit('preview')}>Проверить архив</button>
    {#if preview}
      <h3>Предпросмотр восстановления</h3>
      <ul>{#each names as name}
        <li><code>{name}</code> — {preview.changed.includes(name) ? 'будет заменён' : 'без изменений'}</li>
      {/each}</ul>
      {#if preview.changed.length}
        <p><strong>Подтвердите замену.</strong> Мастер сохранит текущие файлы и перезапустит затронутые службы. Дождитесь окончания радиопередачи.</p>
        <button class="primary" type="button" onclick={() => void submit('apply')}>Подтвердить восстановление</button>
      {:else}
        <p>Архив совпадает с текущими файлами. Восстановление не требуется.</p>
      {/if}
      <button type="button" onclick={() => { preview = null; selected = null; notice = 'Восстановление отменено.'; }}>Закрыть предпросмотр</button>
    {/if}
    <p role="status">{notice}</p>
    {#if error}<p role="alert">{error}</p>{/if}
    {#if !csrf && !busy}<button type="button" onclick={() => void load()}>Обновить сеанс</button>{/if}
  </article>
</section>

<style>
  .archive { max-width: 60rem; margin: 0 auto; color: #292533; }
  article { margin: 1rem 0; padding: clamp(1rem, 3vw, 1.65rem); border: 1px solid #e4dfe7; border-radius: 1rem; background: white; }
  h1, h2, h3 { margin: .25rem 0 .5rem; }
  .kicker { font-weight: 750; letter-spacing: .12em; font-size: .68rem; color: #962152; }
  p, li { line-height: 1.5; overflow-wrap: anywhere; }
  label { display: grid; gap: .45rem; margin: 1rem 0; }
  button, .button { display: inline-block; border: 1px solid #b5adba; padding: .65rem .9rem; margin: .25rem .25rem .25rem 0; border-radius: .6rem; background: white; color: inherit; font: inherit; text-decoration: none; cursor: pointer; }
  .primary { color: white; background: #a82459; }
  button:disabled { opacity: .55; cursor: default; }
  [role="alert"] { color: #842338; }
</style>
