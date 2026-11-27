<script lang="ts">
  // admin/expert/edit_<file>.php: one table per section, Apply per table.
  import { onMount } from 'svelte';

  let { file }: { file: string } = $props();
  type Section = { name: string; keys: [string, string][] };
  let csrf = '';
  let revision = '';
  let sections = $state<Section[] | null>(null);
  let values = $state<Record<string, Record<string, string>>>({});
  let message = $state('');
  let error = $state('');
  const button = $derived(file === 'timeserver' ? 'Save Changes' : 'Apply Changes');

  async function load() {
    const r = await fetch(`/manage/api/expert/${file}`, { cache: 'no-store' });
    if (!r.ok) { error = (await r.text()).trim(); return; }
    const d: { csrf: string; revision: string; sections: Section[] } = await r.json();
    csrf = d.csrf; revision = d.revision; sections = d.sections;
    values = Object.fromEntries(d.sections.map(s => [s.name, Object.fromEntries(s.keys)]));
  }
  onMount(() => { void load(); });

  async function apply(e: SubmitEvent) {
    e.preventDefault();
    message = ''; error = '';
    const r = await fetch(`/manage/api/expert/${file}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-PiStar-CSRF': csrf },
      body: JSON.stringify({ revision, values }),
    }).catch(() => null);
    if (!r || !r.ok) { error = r ? (await r.text()).trim() : 'No response from the board'; return; }
    message = 'Changes applied.';
    await load();
  }
</script>

{#if error}<table><tbody><tr><td style="background:#f33;">{error}</td></tr></tbody></table><br />{/if}
{#if message}<table><tbody><tr><td style="background:#1d1;">{message}</td></tr></tbody></table><br />{/if}
{#if sections}
  <form onsubmit={apply}>
    {#each sections as s (s.name)}
      <table>
        <tbody>
          <tr><th colspan="2">{s.name}</th></tr>
          {#each s.keys as [key]}
            <tr><td align="right" style="width:30%">{key}</td><td align="left"><input type="text" bind:value={values[s.name][key]} aria-label={s.name + ' ' + key} /></td></tr>
          {/each}
        </tbody>
      </table>
      <input type="submit" value={button} />
      <br />
    {/each}
  </form>
{/if}
