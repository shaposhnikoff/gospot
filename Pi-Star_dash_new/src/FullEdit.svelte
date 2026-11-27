<script lang="ts">
  // admin/expert/fulledit_*.php: the whole file in a textarea.
  import { onMount } from 'svelte';

  let { file }: { file: string } = $props();
  let csrf = '';
  let revision = '';
  let text = $state('');
  let loaded = $state(false);
  let exists = $state(true);
  let saved = $state(false);
  let blocked = $state<string[]>([]);
  let error = $state('');

  async function load() {
    const r = await fetch(`/manage/api/expert/full/${file}`, { cache: 'no-store' });
    if (!r.ok) { error = (await r.text()).trim(); return; }
    const d: { csrf: string; revision: string; text: string; exists: boolean } = await r.json();
    csrf = d.csrf; revision = d.revision; text = d.text; exists = d.exists; loaded = true;
  }
  onMount(() => { void load(); });

  async function save(e: SubmitEvent) {
    e.preventDefault();
    saved = false; blocked = []; error = '';
    const r = await fetch(`/manage/api/expert/full/${file}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-PiStar-CSRF': csrf },
      body: JSON.stringify({ revision, text }),
    }).catch(() => null);
    if (!r || !r.ok) { error = r ? (await r.text()).trim() : 'No response from the board'; return; }
    const d: { ok: boolean; blocked?: string[] } = await r.json();
    if (!d.ok) { blocked = d.blocked ?? []; return; }
    saved = true;
    const keep = text;
    await load();
    if (file === 'cron') setTimeout(() => { saved = false; }, 5000);
    if (text !== keep) error = 'The saved file differs from what was submitted.';
  }
</script>

{#if blocked.length}
  <div style="background-color: #ff9090; color: #f01010; padding: 10px; margin: 0 0 10px 0; text-align: left;">
    <b>Crontab not saved &mdash; blocked line(s):</b>
    <ul>{#each blocked as b}<li>{b}</li>{/each}</ul>
    <p style="margin: 8px 0 0 0;">Edit the offending line(s) and re-submit, or revert your changes.
    If you have a legitimate need to schedule one of these patterns, use SSH and edit
    <code>/etc/crontab</code> directly &mdash; the dashboard editor enforces these checks
    to limit the damage of stolen dashboard credentials.</p>
  </div>
{/if}
{#if error}
  <div style="background-color: #ff9090; color: #f01010; padding: 10px; margin: 0 0 10px 0;"><b>{error}</b></div>
{/if}
{#if saved}
  <div style="background-color: #c0f0c0; color: #106010; padding: 10px; margin: 0 0 10px 0;"><b>{file === 'cron' ? 'Crontab saved.' : 'Changes applied.'}</b></div>
{/if}
{#if loaded}
  {#if !exists}<p>This file does not exist yet; saving will create it.</p>{/if}
  <form onsubmit={save}>
    <textarea cols="80" rows="45" bind:value={text} aria-label="File contents"></textarea><br />
    <input type="submit" value="Apply Changes" />
  </form>
{/if}
