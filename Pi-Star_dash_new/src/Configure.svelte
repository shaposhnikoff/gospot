<script lang="ts">
  // admin/configure.php: the server sends every visible section as rows of
  // cells and controls (panel/configure_*.go); this page renders them in the
  // classic markup and posts the whole form on any "Apply Changes".
  import { onMount } from 'svelte';
  import PiStarFrame from './PiStarFrame.svelte';

  type Option = { value: string; label: string };
  type Control = { kind: string; name?: string; value?: string; text?: string; options?: Option[]; attrs?: Record<string, string> };
  type Cell = { header?: boolean; colspan?: number; align?: string; style?: string; label?: string; tooltip?: { title?: string; body?: string }; controls?: Control[] };
  type Section = { id: string; title: string; rows: { id?: string; cells: Cell[] }[]; apply: boolean; form?: string };

  let csrf = '';
  let revision = '';
  let sections = $state<Section[] | null>(null);
  let form = $state<Record<string, string>>({});
  let busy = $state(false);
  let message = $state('');
  let error = $state('');

  async function load() {
    const r = await fetch('/manage/api/configure', { cache: 'no-store' });
    if (!r.ok) { error = (await r.text()).trim(); return; }
    const d: { csrf: string; revision: string; sections: Section[] } = await r.json();
    csrf = d.csrf; revision = d.revision; sections = d.sections;
    const f: Record<string, string> = {};
    for (const s of d.sections) for (const row of s.rows) for (const c of row.cells) for (const ctl of c.controls ?? []) {
      if (ctl.name && !['static', 'button', 'link'].includes(ctl.kind) && !(ctl.name in f)) f[ctl.name] = ctl.value ?? '';
    }
    form = f;
  }
  onMount(() => { void load(); });

  async function apply(formName = 'config') {
    if (busy) return;
    busy = true; message = ''; error = '';
    const names = new Set<string>();
    for (const s of sections ?? []) if ((s.form ?? 'config') === formName) for (const row of s.rows) for (const c of row.cells) for (const ctl of c.controls ?? []) if (ctl.name) names.add(ctl.name);
    const posted = Object.fromEntries(Object.entries(form).filter(([k]) => names.has(k)));
    const r = await fetch('/manage/api/configure', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-PiStar-CSRF': csrf },
      body: JSON.stringify({ revision, form: posted }),
    }).catch(() => null);
    busy = false;
    if (!r || !r.ok) { error = r ? (await r.text()).trim() : 'No response from the board'; return; }
    message = 'Your configuration has been applied.';
    await load();
  }

  function toggled(name: string, e: Event) { form[name] = (e.currentTarget as HTMLInputElement).checked ? 'ON' : 'OFF'; }
</script>

{#snippet tooltip(cell: Cell)}
  {#if cell.tooltip}
    <a class="tooltip2" href="#top" onclick={e => e.preventDefault()}>{cell.label}<span>{#if cell.tooltip.title}<b>{cell.tooltip.title}</b>{/if}{#each (cell.tooltip.body ?? '').split('\n') as line, i}{#if i}<br />{/if}{line}{/each}</span></a>
  {:else}{cell.label ?? ''}{/if}
{/snippet}

{#snippet control(ctl: Control, s: Section)}
  {#if ctl.kind === 'text' || ctl.kind === 'password'}
    <input type={ctl.kind} bind:value={form[ctl.name!]} size={ctl.attrs?.size ? Number(ctl.attrs.size) : undefined}
      maxlength={ctl.attrs?.maxlength ? Number(ctl.attrs.maxlength) : undefined} placeholder={ctl.attrs?.placeholder}
      style={ctl.attrs?.style} id={ctl.attrs?.id} aria-label={ctl.attrs?.label ?? ctl.name} autocomplete="off" />
  {:else if ctl.kind === 'select'}
    <select bind:value={form[ctl.name!]} style={ctl.attrs?.style} aria-label={ctl.attrs?.label ?? ctl.name}>
      {#each ctl.options ?? [] as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
    </select>
  {:else if ctl.kind === 'radio'}
    {#each ctl.options ?? [] as o (o.value)}
      <label><input type="radio" name={ctl.name} value={o.value} bind:group={form[ctl.name!]}
        onchange={() => { if (ctl.attrs?.alert && o.value === ctl.attrs.alertValue) alert(ctl.attrs.alert); }} />{o.label}</label>
    {/each}
  {:else if ctl.kind === 'toggle'}
    <div class="switch">
      <input id={'toggle-' + ctl.name} class="toggle toggle-round-flat" type="checkbox" checked={form[ctl.name!] === 'ON'} onchange={e => toggled(ctl.name!, e)} aria-label={ctl.attrs?.label ?? ctl.name} />
      <label for={'toggle-' + ctl.name}></label>
    </div>
    {#if ctl.text}{ctl.text}{/if}
  {:else if ctl.kind === 'link' || (ctl.kind === 'static' && ctl.attrs?.href)}
    <a href={ctl.attrs?.href} target={ctl.attrs?.target} rel={ctl.attrs?.target ? 'noopener noreferrer' : undefined} style={ctl.attrs?.style}>{ctl.text}</a>
  {:else if ctl.kind === 'static'}
    <span class="static">{ctl.text}</span>
  {:else if ctl.kind === 'button'}
    <input type="button" value={ctl.text} disabled={busy} onclick={() => apply(s.form ?? 'config')} />
  {/if}
{/snippet}

<PiStarFrame title="Configuration" links={['Dashboard', 'Admin', 'Expert', 'Power', 'Update', 'Backup/Restore']}>
  {#if message}<table><tbody><tr><td style="background:#1d1;">{message}</td></tr></tbody></table><br />{/if}
  {#if error}<table><tbody><tr><td style="background:#f33;">{error}</td></tr></tbody></table><br />{/if}
  {#if !sections}
    {#if !error}Loading configuration...{/if}
  {:else}
    {#each sections as s (s.id)}
      <div class="section" id={'cfg-' + s.id}>
        <h2>{s.title}</h2>
        <table>
          <tbody>
            {#each s.rows as row, ri (row.id ?? ri)}
              <tr id={row.id}>
                {#each row.cells as cell}
                  {#if cell.header}
                    <th colspan={cell.colspan} style={cell.style}>{@render tooltip(cell)}</th>
                  {:else}
                    <td colspan={cell.colspan} align={(cell.align ?? 'left') as 'left'} style={cell.style}>
                      {#if cell.label}{@render tooltip(cell)}{/if}
                      {#each cell.controls ?? [] as ctl}{@render control(ctl, s)}{' '}{/each}
                    </td>
                  {/if}
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
        {#if s.apply}
          <div><input type="button" value="Apply Changes" disabled={busy} onclick={() => apply(s.form ?? 'config')} /></div>
        {/if}
      </div>
    {/each}
  {/if}
</PiStarFrame>

<style>
  .section :global(h2) { color: var(--ps-text, #000000); font: 1em verdana, arial, sans-serif; text-align: center; font-weight: bold; padding: 0; margin: 8px 0 0 0; }
  .static { white-space: pre-line; }
  /* pistar-css.php "CSS Toggle Code" */
  .toggle { position: absolute; margin-left: -9999px; visibility: hidden; }
  .toggle + label { display: block; position: relative; cursor: pointer; outline: none; }
  input.toggle-round-flat + label { padding: 1px; width: 33px; height: 18px; background-color: #dddddd; border-radius: 10px; transition: background 0.4s; }
  input.toggle-round-flat + label:before, input.toggle-round-flat + label:after { display: block; position: absolute; content: ""; }
  input.toggle-round-flat + label:before { top: 1px; left: 1px; bottom: 1px; right: 1px; background-color: #fff; border-radius: 10px; transition: background 0.4s; }
  input.toggle-round-flat + label:after { top: 2px; left: 2px; bottom: 2px; width: 16px; background-color: #dddddd; border-radius: 12px; transition: margin 0.4s, background 0.4s; }
  input.toggle-round-flat:checked + label { background-color: var(--ps-banners, #dd4b39); }
  input.toggle-round-flat:checked + label:after { margin-left: 14px; background-color: var(--ps-banners, #dd4b39); }
  .switch { display: inline-block; vertical-align: middle; }
  :global(a.tooltip2) { position: relative; font-weight: bold; color: #000000 !important; text-decoration: none; }
  :global(a.tooltip2 span) { display: none; }
  :global(a.tooltip2:hover span) { display: block; position: absolute; top: 20px; left: 0; width: 202px; z-index: 100; color: #000000; border: 1px solid #000000; background: #f7f7f7; font: 12px Verdana, sans-serif; text-align: left; white-space: normal; }
  :global(a.tooltip2 span b) { display: block; margin: 0; padding: 0; font-size: 12px; font-weight: bold; border-bottom: 1px solid black; background: #d0d0d0; }
</style>
