<script lang="ts">
  // dstarrepeater/link_manager.php of the classic admin page, with its
  // GoSpot-Keeper logbook. Link and unlink run remotecontrold on the board.
  import { onMount, tick } from 'svelte';
  import { type DStarManager, type DStarForm, letters, dstarLinkBody, dstarOutput } from './dstar';

  let data = $state<DStarManager | null>(null);
  let output = $state<{ text: string; table: boolean } | null>(null);
  const form = $state<DStarForm>({ module: '', ref: '', text: '', custom: false, letter: '', action: 'LINK' });
  let textField = $state<HTMLInputElement | null>(null);

  async function load() {
    try {
      const r = await fetch('/manage/api/links/dstar', { cache: 'no-store' });
      if (!r.ok) return;
      const next: DStarManager = await r.json();
      // The classic page reloads after a request: the defaults come back.
      if (!data) Object.assign(form, { module: next.modules[0] ?? '', ref: next.defaultName, letter: next.defaultLetter });
      data = next;
    } catch { /* keep the previous view */ }
  }

  onMount(() => { void load(); });

  async function refChange() {
    if (form.ref !== 'customOption') return;
    // toggleField(): swap the select for a text field
    form.ref = data?.defaultName ?? '';
    form.custom = true;
    await tick();
    textField?.focus();
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    const r = await fetch('/manage/api/links/dstar', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-GoSpot-CSRF': data?.csrf ?? '' }, body: JSON.stringify(dstarLinkBody(form)),
    }).catch(() => null);
    let text = '';
    if (r) text = r.ok ? (await r.json()).output : await r.text();
    output = dstarOutput(r ? r.status : null, text);
    setTimeout(() => {
      output = null;
      Object.assign(form, { module: data?.modules[0] ?? '', ref: data?.defaultName ?? '', letter: data?.defaultLetter ?? '', text: '', custom: false, action: 'LINK' });
      void load();
    }, 2000);
  }
</script>

{#if output}
  {#if output.table}
    <b>D-Star Link Manager</b>
    <table><tbody><tr><th>Command Output</th></tr><tr><td>{output.text}</td></tr></tbody></table>
  {:else}
    {output.text}
  {/if}
{:else if data}
  <b>D-Star Link Manager</b>
  <form id="dstarLinkManager" onsubmit={submit}>
    <table>
      <tbody>
        <tr>
          <th style="width:150px"><a class="tooltip" href="#top">Radio Module<span><b>Radio Module</b></span></a></th>
          <th style="width:180px"><a class="tooltip" href="#top">Reflector<span><b>Reflector</b></span></a></th>
          <th style="width:150px"><a class="tooltip" href="#top">Link / Un-Link<span><b>Link / Un-Link</b></span></a></th>
          <th><a class="tooltip" href="#top">Action<span><b>Action</b></span></a></th>
        </tr>
        <tr>
          <td>
            <select name="Module" bind:value={form.module} aria-label="Radio Module">
              {#each data.modules as m}<option value={m}>{m}</option>{/each}
            </select>
          </td>
          <td>
            {#if form.custom}
              <input name="RefName" type="text" size="7" maxlength="7" aria-label="Reflector" bind:this={textField} bind:value={form.text}
                onblur={() => { if (form.text === '') form.custom = false; }} />
            {:else}
              <select name="RefName" bind:value={form.ref} onchange={refChange} aria-label="Reflector">
                <option value={data.defaultName}>{data.defaultName}</option>
                <option value="customOption">Text Entry</option>
                {#each data.reflectors as ref}<option value={ref}>{ref}</option>{/each}
              </select>
            {/if}
            <select name="Letter" bind:value={form.letter} aria-label="Reflector Module">
              <option value={data.defaultLetter}>{data.defaultLetter}</option>
              {#each letters as l}<option value={l}>{l}</option>{/each}
            </select>
          </td>
          <td>
            <input type="radio" name="Link" value="LINK" bind:group={form.action} />Link
            <input type="radio" name="Link" value="UNLINK" bind:group={form.action} />UnLink
          </td>
          <td><input type="submit" name="dstrMgrSubmit" value="Request Change" /></td>
        </tr>
      </tbody>
    </table>
  </form>
{/if}

{#if data?.keeper}
  <br />
  <b>GoSpot-Keeper Logbook</b><input type="button" value="Download Logbook" onclick={() => { location.href = '/manage/api/links/dstar/keeper'; }} />
  <table>
    <tbody>
      <tr><th><a class="tooltip" href="#top">GoSpot-Keeper Log Entries (UTC)<span><b>GoSpot-Keeper Log Entries (UTC)</b></span></a></th></tr>
      {#each data.keeper as line}<tr><td align="left">{line}</td></tr>{/each}
    </tbody>
  </table>
{/if}
