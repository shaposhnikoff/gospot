<script lang="ts">
  // admin/expert/edit_dashboard.php: /etc/gospot-css.ini, one table per
  // section, Apply saves every shown value; Factory Reset removes the file.
  import { onMount } from 'svelte';

  type Section = { name: string; keys: [string, string][] };
  let csrf = '';
  let revision = '';
  let sections = $state<Section[] | null>(null);
  let values = $state<Record<string, Record<string, string>>>({});
  let message = $state('');
  let error = $state('');

  async function load() {
    const r = await fetch('/manage/api/expert/css', { cache: 'no-store' });
    if (!r.ok) { error = (await r.text()).trim(); return; }
    const d: { csrf: string; revision: string; sections: Section[] } = await r.json();
    csrf = d.csrf; revision = d.revision; sections = d.sections;
    values = Object.fromEntries(d.sections.map(s => [s.name, Object.fromEntries(s.keys)]));
  }
  onMount(() => { void load(); });

  async function send(body: Record<string, unknown>, done: string) {
    message = ''; error = '';
    const r = await fetch('/manage/api/expert/css', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-GoSpot-CSRF': csrf },
      body: JSON.stringify({ revision, ...body }),
    }).catch(() => null);
    if (!r || !r.ok) { error = r ? (await r.text()).trim() : 'No response from the board'; return; }
    message = done;
    await load();
  }

  function apply(e: SubmitEvent) {
    e.preventDefault();
    void send({ action: 'save', values }, 'Changes applied.');
  }

  function factoryReset() {
    const typed = prompt('WARNING: This will reset these settings to factory defaults.\n\n'
      + 'Type RESET (uppercase) and press OK to proceed.\nPress Cancel to go back.', '');
    if (typed === null) return;
    if (typed !== 'RESET') { alert('Confirmation text did not match. Factory reset cancelled.'); return; }
    void send({ action: 'reset', confirm: typed }, 'Loading fresh configuration file(s)...');
  }
</script>

{#if message}<br /><table><tbody><tr><th>{message === 'Changes applied.' ? 'Configuration' : 'Factory Reset Config'}</th></tr><tr><td>{message}</td></tr></tbody></table>{/if}
{#if error}<br /><table><tbody><tr><th>Error</th></tr><tr><td style="background:#f33;">{error}</td></tr></tbody></table>{/if}
{#if sections}
  <form onsubmit={apply}>
    {#each sections as s (s.name)}
      <table>
        <tbody>
          <tr><th colspan="2">{s.name}</th></tr>
          {#each s.keys as [key] (key)}
            <tr>
              <td align="right" style="width:30%">{key}</td>
              <td align="left">
                {#if s.name === 'Lookup' && key === 'Service'}
                  <select bind:value={values[s.name][key]} aria-label={s.name + ' ' + key}>
                    <option value="RadioID">RadioID Callsign Lookup</option>
                    <option value="QRZ">QRZ Callsign Lookup</option>
                  </select>
                {:else}
                  <input type="text" bind:value={values[s.name][key]} aria-label={s.name + ' ' + key} />
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
      <input type="submit" value="Apply Changes" />
      <br /><br />
    {/each}
  </form>
  <br />
  if you took it all too far and now it makes you feel sick, click below to reset the changes made on this page, this will ONLY reset the CSS settings above and will not change any other settings or configuration.<br />
  <input type="button" value="Factory Reset" onclick={factoryReset} />
{:else if !error}
  Loading...
{/if}
