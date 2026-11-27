<script lang="ts">
  // admin/expert/fulledit_apikeys.php: BrandMeister, DAPNET and TGIF keys.
  import { onMount } from 'svelte';
  type Target = { id: string; title: string; sections: [string, [string, string][]][] };
  let csrf = '';
  let targets = $state<Target[]>([]);
  let values = $state<Record<string, Record<string, Record<string, string>>>>({});
  let saved = $state(''); let failed = $state('');

  async function load() {
    const r = await fetch('/manage/api/expert/keys', { cache: 'no-store' });
    if (!r.ok) { failed = (await r.text()).trim(); return; }
    const d: { csrf: string; targets: Target[] } = await r.json();
    csrf = d.csrf; targets = d.targets;
    values = Object.fromEntries(d.targets.map(t => [t.id, Object.fromEntries(t.sections.map(([s, keys]) => [s, Object.fromEntries(keys)]))]));
  }
  onMount(() => { void load(); });

  async function save(t: Target, e: SubmitEvent) {
    e.preventDefault(); saved = ''; failed = '';
    const r = await fetch('/manage/api/expert/keys', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-PiStar-CSRF': csrf },
      body: JSON.stringify({ target: t.id, values: values[t.id] }),
    }).catch(() => null);
    if (r?.ok) { saved = t.title; await load(); return; }
    const reason = r ? (await r.text()).trim() : 'no response from the board';
    failed = `${t.title} could not be saved` + (reason ? ` (${reason})` : '');
  }
</script>

{#if saved}<table><tbody><tr><td><b role="status">{saved} saved.</b></td></tr></tbody></table><br />{/if}
{#if failed}<table><tbody><tr><td><b role="alert">{failed}.</b></td></tr></tbody></table><br />{/if}
{#each targets as t (t.id)}
  <form onsubmit={e => save(t, e)}>
    <table>
      <tbody>
        <tr><th colspan="2">{t.title}</th></tr>
        {#each t.sections as [section, keys]}
          {#each keys as [key]}
            <tr><td align="right" style="width:30%">{key}</td><td align="left"><input type="text" size="72" bind:value={values[t.id][section][key]} aria-label={t.title + ' ' + key} /></td></tr>
          {/each}
        {/each}
      </tbody>
    </table>
    <input type="submit" value="Apply Changes" />
  </form>
  <br />
{/each}
