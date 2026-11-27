<script lang="ts">
  // Admin-only blocks of the classic index.php: bm_links.php, bm_manager.php,
  // tgif_static_links.php + tgif_static_manager.php or tgif_manager.php,
  // ysf_manager.php, p25_manager.php, nxdn_manager.php, m17_manager.php.
  import { onMount } from 'svelte';
  import { tgifCell, nextTGIFRefresh, type TGIFLinks } from './tgif';

  type Option = { value: string; label: string };
  type Manager = { mode: string; selected: string; room?: string; options: Option[] };
  type BM = { master: string; id: string; static: string[] | null; dynamic: string[] | null; manager: boolean; simplex: boolean; error?: string };
  type TGIF = { id: string; static: boolean };
  type Reply = { csrf: string; bm: BM | null; tgif: TGIF | null; managers: Manager[] | null };

  let data = $state<Reply | null>(null);
  let output = $state<{ title: string; text: string } | null>(null);
  const choice = $state<Record<string, { target: string; link: string; room: string }>>({});
  const bmForm = $state({ tg: '', slot: '2', mode: 'add' });
  const tgifForm = $state({ tg: '', slot: '2', action: 'ADD' });
  const tgifLegacy = $state({ tg: '', slot: '2', action: 'UNLINK' });
  let tgif = $state<TGIFLinks | null>(null);
  let tgifTimer: ReturnType<typeof setTimeout> | undefined;
  let tgifRetried = false;
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
      if (data?.tgif?.static && tgifTimer === undefined) startTGIF();
    } catch { /* keep the previous view */ }
  }

  // The TGIF table is filled after the page is drawn (two third-party HTTPS
  // calls); until then the cells show an ellipsis, as in admin/index.php.
  function startTGIF() {
    clearTimeout(tgifTimer);
    tgif = null;
    tgifRetried = false;
    tgifTimer = setTimeout(loadTGIF, 100);
  }

  async function loadTGIF() {
    if (!data?.tgif?.static) { tgifTimer = undefined; return; } // token removed meanwhile
    let ok = false;
    try {
      const r = await fetch('/manage/api/links/tgif', { cache: 'no-store' });
      if (r.ok) { tgif = await r.json(); ok = true; }
    } catch { /* keep the placeholder or the previous values */ }
    const next = nextTGIFRefresh(ok, tgifRetried);
    tgifRetried = next.retried;
    tgifTimer = setTimeout(loadTGIF, next.delay);
  }

  onMount(() => {
    void load();
    const t = setInterval(load, 180000); // bm_links.php refresh cadence
    return () => { clearInterval(t); clearTimeout(tgifTimer); };
  });

  async function send(title: string, url: string, body: object, back: number) {
    const r = await fetch(url, {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-GoSpot-CSRF': data?.csrf ?? '' }, body: JSON.stringify(body),
    }).catch(() => null);
    let text = 'No response from the board';
    if (r) text = r.ok ? (await r.json()).output : (await r.text()).trim();
    output = { title, text };
    // The classic page reloads itself here, which also refetches TGIF.
    setTimeout(() => { output = null; void load(); if (data?.tgif?.static) startTGIF(); }, back);
  }

  function tgifSubmit(e: SubmitEvent) {
    e.preventDefault();
    const kind = (e.submitter as HTMLInputElement | null)?.name === 'tgifDynamicModify' ? 'dynamic' : 'static';
    void send('TGIF Manager', '/manage/api/links/tgif', { kind, action: tgifForm.action, tg: tgifForm.tg, slot: tgifForm.slot }, 3000);
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

  {#if data.tgif?.static}
    <div id="tgifStaticConnects">
      <b>Active TGIF Connections</b>
      <table>
        <tbody>
          <tr>
            <th><a class="tooltip" href="#top">TGIF Master<span><b>Connected Master</b></span></a></th>
            <th><a class="tooltip" href="#top">Repeater ID<span><b>The ID for this Repeater/Hotspot</b></span></a></th>
            <th><a class="tooltip" href="#top">Static TGs<span><b>Statically linked talkgroups</b></span></a></th>
            <th><a class="tooltip" href="#top">Dynamic TGs<span><b>Dynamically linked talkgroups</b></span></a></th>
          </tr>
          <tr>
            <td>tgif.network</td><td>{data.tgif.id}</td>
            {#each [tgif ? tgifCell(tgif.static, tgif.staticState) : ['…'], tgif ? tgifCell(tgif.dynamic, tgif.dynamicState) : ['…']] as lines, i (i)}
              <td>{#each lines as line, j (j)}{#if j}<br />{/if}{line}{/each}</td>
            {/each}
          </tr>
        </tbody>
      </table>
      <br />
      {#if tgif?.errors?.length}
        <table>
          <tbody>
            <tr><th>TGIF API</th></tr>
            {#each tgif.errors as error, i (i)}<tr><td>{error}</td></tr>{/each}
          </tbody>
        </table>
        <br />
      {/if}
    </div>
    <b>TGIF Manager</b>
    <form onsubmit={tgifSubmit}>
      <table role="presentation">
        <tbody>
          <tr>
            <th aria-hidden="true" id="lblTgifTG" style="width:25%;"><a class="tooltip" href="#top">Talkgroup<span><b>Enter the Talkgroup number</b></span></a></th>
            <th aria-hidden="true" id="lblTgifSlot" style="width:25%;"><a class="tooltip" href="#top">Slot<span><b>Where to add/remove</b></span></a></th>
            <th aria-hidden="true" id="lblTgifAddRemove" style="width:25%;"><a class="tooltip" href="#top">Add / Remove<span><b>Add or Remove</b></span></a></th>
            <th><a class="tooltip" href="#top">Action<span><b>Take Action</b></span></a></th>
          </tr>
          <tr>
            <td><input aria-labelledby="lblTgifTG" type="text" inputmode="numeric" name="tgifStaticTalkgroup" size="10" maxlength="8" bind:value={tgifForm.tg} /></td>
            <td role="radiogroup" aria-labelledby="lblTgifSlot"><input id="rbTgifTS1" type="radio" name="tgifStaticSlot" value="1" bind:group={tgifForm.slot} /><label for="rbTgifTS1">TS1</label> <input id="rbTgifTS2" type="radio" name="tgifStaticSlot" value="2" bind:group={tgifForm.slot} /><label for="rbTgifTS2">TS2</label></td>
            <td role="radiogroup" aria-labelledby="lblTgifAddRemove"><input id="rbTgifAdd" type="radio" name="tgifStaticAction" value="ADD" bind:group={tgifForm.action} /><label for="rbTgifAdd">Add</label> <input id="rbTgifDel" type="radio" name="tgifStaticAction" value="DEL" bind:group={tgifForm.action} /><label for="rbTgifDel">Remove</label></td>
            <td><input type="submit" value="Set Static" name="tgifStaticModify" /> <input type="submit" value="Set Dynamic" name="tgifDynamicModify" /></td>
          </tr>
        </tbody>
      </table>
      <br />
    </form>
  {:else if data.tgif}
    <b>TGIF Manager</b>
    <form onsubmit={e => { e.preventDefault(); void send('TGIF Manager', '/manage/api/links/tgif', { kind: 'legacy', action: tgifLegacy.action, tg: tgifLegacy.tg, slot: tgifLegacy.slot }, 3000); }}>
      <table>
        <tbody>
          <tr>
            <th style="width:25%;"><a class="tooltip" href="#top">Talkgroup Number<span><b>Enter the Talkgroup number</b></span></a></th>
            <th style="width:25%;"><a class="tooltip" href="#top">Slot<span><b>Where to link/unlink</b></span></a></th>
            <th style="width:25%;"><a class="tooltip" href="#top">Link / Unlink<span><b>Link or unlink</b></span></a></th>
            <th><a class="tooltip" href="#top">Action<span><b>Take Action</b></span></a></th>
          </tr>
          <tr>
            <td><input type="text" name="tgifNumber" size="10" maxlength="7" bind:value={tgifLegacy.tg} aria-label="Talkgroup Number" /></td>
            <td>
              <label><input type="radio" name="tgifSlot" value="1" bind:group={tgifLegacy.slot} />TS1</label>
              <label><input type="radio" name="tgifSlot" value="2" bind:group={tgifLegacy.slot} />TS2</label>
            </td>
            <td>
              <label><input type="radio" name="tgifAction" value="LINK" bind:group={tgifLegacy.action} />Link</label>
              <label><input type="radio" name="tgifAction" value="UNLINK" bind:group={tgifLegacy.action} />UnLink</label>
            </td>
            <td><input type="submit" value="Modify Static" name="tgifSubmit" /></td>
          </tr>
        </tbody>
      </table>
      <br />
    </form>
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
