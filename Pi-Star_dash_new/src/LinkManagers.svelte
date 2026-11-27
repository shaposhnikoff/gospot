<script lang="ts">
  // Admin-only blocks of the classic index.php: bm_links.php, bm_manager.php,
  // ysf_manager.php, p25_manager.php, nxdn_manager.php, m17_manager.php.
  import { onMount } from 'svelte';

  type Option = { value: string; label: string };
  type Manager = { mode: string; selected: string; room?: string; options: Option[] };
  type BM = { master: string; id: string; static: string[] | null; dynamic: string[] | null; manager: boolean; simplex: boolean; error?: string };
  type Reply = { csrf: string; bm: BM | null; managers: Manager[] | null };

  let data = $state<Reply | null>(null);
  let output = $state<{ title: string; text: string } | null>(null);
  const choice = $state<Record<string, { target: string; link: string; room: string }>>({});
  const bmForm = $state({ tg: '', slot: '2', mode: 'add' });
  const rooms = ['A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K'];
  const titles: Record<string, string> = { YSF: 'YSF Link Manager', P25: 'P25 Link Manager', NXDN: 'NXDN Link Manager', M17: 'M17 Link Manager' };

  async function load() {
    try {
      const r = await fetch('/manage/api/links', { cache: 'no-store' });
      if (!r.ok) return;
      data = await r.json();
      for (const m of data?.managers ?? []) {
        choice[m.mode] ??= { target: m.selected, link: 'link', room: m.room || 'A' };
      }
    } catch { /* keep the previous view */ }
  }

  onMount(() => {
    void load();
    const t = setInterval(load, 180000); // bm_links.php refresh cadence
    return () => clearInterval(t);
  });

  async function send(title: string, url: string, body: object, back: number) {
    const r = await fetch(url, {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-PiStar-CSRF': data?.csrf ?? '' }, body: JSON.stringify(body),
    }).catch(() => null);
    let text = 'No response from the board';
    if (r) text = r.ok ? (await r.json()).output : (await r.text()).trim();
    output = { title, text };
    setTimeout(() => { output = null; void load(); }, back);
  }

  const tgList = (l: string[] | null) => l && l.length ? l.join(' ') : 'None';
</script>

{#if output}
  <b>{output.title}</b>
  <table><tbody><tr><th>Command Output</th></tr><tr><td>{output.text}</td></tr></tbody></table>
  <br />
{:else if data}
  {#if data.bm}
    <b>Active BrandMeister Connections</b>
    <table>
      <tbody>
        <tr>
          <th><a class="tooltip" href="#top">BM Master<span><b>Connected Master</b></span></a></th>
          <th><a class="tooltip" href="#top">Repeater ID<span><b>The ID for this Repeater/Hotspot</b></span></a></th>
          <th><a class="tooltip" href="#top">Static TGs<span><b>Statically linked talkgroups</b></span></a></th>
          <th><a class="tooltip" href="#top">Dynamic TGs<span><b>Dynamically linked talkgroups</b></span></a></th>
        </tr>
        <tr>
          <td>{data.bm.master}</td><td>{data.bm.id}</td>
          <td class="wrap">{data.bm.error ? data.bm.error : tgList(data.bm.static)}</td>
          <td class="wrap">{data.bm.error ? '' : tgList(data.bm.dynamic)}</td>
        </tr>
      </tbody>
    </table>
    <br />
    {#if data.bm.manager}
      <b>BrandMeister Manager</b>
      <form onsubmit={e => { e.preventDefault(); void send('BrandMeister Manager', '/manage/api/links/bm', { action: bmForm.mode, tg: bmForm.tg, slot: bmForm.slot }, 3000); }}>
        <table>
          <tbody>
            <tr>
              <th style="width:25%;"><a class="tooltip" href="#top">Talkgroup<span><b>Enter the Talkgroup number</b></span></a></th>
              <th style="width:25%;"><a class="tooltip" href="#top">Slot<span><b>Where to link/unlink</b></span></a></th>
              <th style="width:25%;"><a class="tooltip" href="#top">Add / Remove<span><b>Add or Remove</b></span></a></th>
              <th><a class="tooltip" href="#top">Action<span><b>Take Action</b></span></a></th>
            </tr>
            <tr>
              <td><input type="text" size="10" maxlength="7" bind:value={bmForm.tg} aria-label="Talkgroup" /></td>
              <td>
                <label><input type="radio" value="1" bind:group={bmForm.slot} />TS1</label>
                <label><input type="radio" value="2" bind:group={bmForm.slot} />TS2</label>
              </td>
              <td>
                <label><input type="radio" value="add" bind:group={bmForm.mode} />Add</label>
                <label><input type="radio" value="delete" bind:group={bmForm.mode} />Delete</label>
              </td>
              <td><input type="submit" value="Set Static" /></td>
            </tr>
            <tr>
              <td colspan="4" style="background: #ffffff;">
                <input type="button" value="Drop QSO" onclick={() => send('BrandMeister Manager', '/manage/api/links/bm', { action: 'drop-qso', slot: bmForm.slot }, 3000)} />
                <input type="button" value="Drop All Dynamic" onclick={() => send('BrandMeister Manager', '/manage/api/links/bm', { action: 'drop-dynamic', slot: bmForm.slot }, 3000)} />
              </td>
            </tr>
          </tbody>
        </table>
      </form>
      <br />
    {/if}
  {/if}

  {#each data.managers ?? [] as m (m.mode)}
    {#if choice[m.mode]}
      <b>{titles[m.mode]}</b>
      <form onsubmit={e => { e.preventDefault(); void send(titles[m.mode], '/manage/api/links', { mode: m.mode, action: choice[m.mode].link, target: choice[m.mode].target, room: choice[m.mode].room }, 2000); }}>
        <table>
          <tbody>
            <tr>
              <th style="width:150px"><a class="tooltip" href="#top">Reflector<span><b>Reflector</b></span></a></th>
              <th style="width:150px"><a class="tooltip" href="#top">Link / Un-Link<span><b>Link / Un-Link</b></span></a></th>
              <th style="width:150px"><a class="tooltip" href="#top">Action<span><b>Action</b></span></a></th>
            </tr>
            <tr>
              <td>
                <select bind:value={choice[m.mode].target} aria-label="Reflector">
                  {#each m.options as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
                </select>
                {#if m.mode === 'M17'}
                  <select bind:value={choice[m.mode].room} aria-label="Module">{#each rooms as r}<option>{r}</option>{/each}</select>
                {/if}
              </td>
              <td>
                <label><input type="radio" value="link" bind:group={choice[m.mode].link} />Link</label>
                <label><input type="radio" value="unlink" bind:group={choice[m.mode].link} />UnLink</label>
              </td>
              <td><input type="submit" value="Request Change" /></td>
            </tr>
          </tbody>
        </table>
      </form>
      <br />
    {/if}
  {/each}
{/if}

<style>
  .wrap { white-space: normal; }
  select { max-width: 380px; }
</style>
