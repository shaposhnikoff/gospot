<script lang="ts">
  // Markup and styles follow Pi-Star_DV_Dash index.php, repeaterinfo.php,
  // lh.php, localtx.php and css/pistar-css*.php (revision 10bf34c9).
  import { onMount } from 'svelte';
  import {
    type Dashboard, type Row, cellStyle, trxStyle, modeLabel, target, callsign, liveSeconds,
    duration, loss, ber, lossStyle, berStyle, rssi, red, green, dataNote, applyTheme,
  } from './classic';
  import { readJSON, serviceOrigin } from './runtime';
  import LinkManagers from './LinkManagers.svelte';

  // admin: the /admin/ variant of index.php, served by the HTTPS management UI.
  let { admin = false }: { admin?: boolean } = $props();
  type SystemInfo = {
    hostname: string; kernel: string; uptimeSeconds: number | null; loadAverage: [number, number, number] | null;
    cpuTempC: number | null; addresses: string[] | null; platform: string; classicServices: { name: string; state: string }[] | null;
  };
  let sys = $state<SystemInfo | null>(null);
  let operator = $state('/');
  let d = $state<Dashboard | null>(null);
  let failed = $state(false);
  const year = new Date().getFullYear();
  const pairs = <T,>(items: T[] | null) => {
    const out: T[][] = [];
    for (let i = 0; i < (items?.length ?? 0); i += 2) out.push(items!.slice(i, i + 2));
    return out;
  };

  onMount(() => {
    // Workspace pages share this document; the class gives the classic body
    // rules precedence while this page is mounted.
    document.body.classList.add('pistar-classic');
    let stop = false;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const r = await fetch(admin ? '/manage/api/dashboard' : '/api/dashboard', { cache: 'no-store' });
        if (!r.ok) throw new Error(String(r.status));
        d = await r.json();
        applyTheme(d?.Theme);
        failed = false;
      } catch {
        failed = true;
      }
      if (!stop) timer = setTimeout(poll, 1000);
    }
    void poll();
    // system.php is refreshed every 15 s on the classic admin page.
    let sysTimer: ReturnType<typeof setTimeout>;
    async function pollSystem() {
      try { sys = await readJSON('/manage/api/system') as SystemInfo; } catch { /* keep the last values */ }
      if (!stop) sysTimer = setTimeout(pollSystem, 15000);
    }
    if (admin) {
      void pollSystem();
      void readJSON('/runtime.json').then(c => {
        const port = c && typeof c === 'object' && 'operatorPort' in c ? c.operatorPort : null;
        const origin = serviceOrigin(location.origin, 'http:', port);
        if (origin) operator = origin + '/';
      }).catch(() => {});
    }
    return () => { stop = true; clearTimeout(timer); clearTimeout(sysTimer); document.body.classList.remove('pistar-classic'); };
  });

  // system.php: CPU temperature colours and `uptime -p` wording.
  const tempStyle = (c: number) => c >= 69 ? 'background: #f00' : c >= 50 ? 'background: #fa0' : 'background: #1d1';
  const shortHost = (h: string) => h.length >= 16 ? h.slice(0, 14) + '..' : h;
  function uptime(seconds: number | null): string {
    if (seconds == null) return '';
    const parts: string[] = [];
    const units: [string, number][] = [['week', 604800], ['day', 86400], ['hour', 3600], ['minute', 60]];
    let rest = Math.floor(seconds);
    for (const [name, size] of units) {
      const n = Math.floor(rest / size); rest -= n * size;
      if (n) parts.push(`${n} ${name}${n === 1 ? '' : 's'}`);
    }
    return 'up ' + (parts.join(', ') || '0 minutes');
  }

  $effect(() => {
    if (d?.Controller === 'none') {
      const t = setTimeout(() => { location.href = '/admin/configure.php'; }, 10000);
      return () => clearTimeout(t);
    }
  });
</script>

<svelte:head><title>{d?.Callsign ?? 'Pi-Star'} - Digital Voice Dashboard</title></svelte:head>

{#snippet head(local: boolean)}
  <tr>
    <th><a class="tooltip" href="#top">Time ({d?.TimeZone})<span><b>Time in {d?.TimeZone} time zone</b></span></a></th>
    <th><a class="tooltip" href="#top">Mode<span><b>Transmitted Mode</b></span></a></th>
    <th style="min-width:14ch"><a class="tooltip" href="#top">Callsign<span><b>Callsign</b></span></a></th>
    <th><a class="tooltip" href="#top">Target<span><b>Target, D-Star Reflector, DMR Talk Group etc</b></span></a></th>
    <th><a class="tooltip" href="#top">Src<span><b>Received from source</b></span></a></th>
    <th><a class="tooltip" href="#top">Dur(s)<span><b>Duration in Seconds</b></span></a></th>
    {#if local}
      <th style="min-width:5ch"><a class="tooltip" href="#top">BER<span><b>Bit Error Rate</b></span></a></th>
      <th style="min-width:8ch"><a class="tooltip" href="#top">RSSI<span><b>Received Signal Strength Indication</b></span></a></th>
    {:else}
      <th><a class="tooltip" href="#top">Loss<span><b>Packet Loss</b></span></a></th>
      <th><a class="tooltip" href="#top">BER<span><b>Bit Error Rate</b></span></a></th>
    {/if}
  </tr>
{/snippet}

{#snippet row(r: Row, local: boolean)}
  {@const cs = callsign(r.callsign, d?.Theme?.Lookup)}
  <tr>
    <td align="left">{r.clock}</td>
    <td align="left">{modeLabel(r.mode)}</td>
    {#if cs.kind === 'id'}
      <td align="left"><a href={cs.href} target="_blank" rel="noopener noreferrer">{cs.text}</a></td>
    {:else if cs.kind === 'call'}
      <td align="left"><div style="float:left;"><a href={cs.href} target="_blank" rel="noopener noreferrer">{cs.text}</a>{#if r.suffix?.trim()}/{r.suffix}{/if}</div> <div style="text-align:right;">(<a href={cs.gps} target="_blank" rel="noopener noreferrer">GPS</a>)</div></td>
    {:else}
      <td align="left">{cs.text}</td>
    {/if}
    <td align="left">{target(r.target)}</td>
    {#if r.source === 'RF'}<td style={green}>RF</td>{:else}<td>{r.source}</td>{/if}
    {#if r.live}
      <td colspan="3" style={red}>TX {liveSeconds(r, d!.Now)} sec</td>
    {:else if dataNote(r)}
      <td colspan="3" style={green}>{r.note}</td>
    {:else}
      <td>{duration(r)}</td>
      {#if local}
        <td style={berStyle(r)}>{ber(r)}</td>
        <td>{rssi(r)}</td>
      {:else}
        <td style={lossStyle(r)}>{loss(r)}</td>
        <td style={berStyle(r)}>{ber(r)}</td>
      {/if}
    {/if}
  </tr>
{/snippet}

<div class="container" id="top">
  <div class="header">
    <div style="font-size: 8px; text-align: left; padding-left: 8px; float: left;">Hostname: {d?.Hostname ?? ''}</div>
    <div style="font-size: 8px; text-align: right; padding-right: 8px;">Pi-Star:{d?.PiStar || 'native'} / Dashboard: {d?.Version ?? ''}</div>
    <h1>Pi-Star Digital Voice Dashboard for {d?.Callsign ?? ''}</h1>
    {#if d?.Theme?.BannerH1}<h1>{d.Theme.BannerH1}</h1>{/if}
    {#if d?.Theme?.BannerExt}<p style="text-align: center; color: #ffffff;">{d.Theme.BannerExt}</p>{/if}
    <p style="padding-right: 5px; text-align: right; color: #ffffff;">
      {#if admin}
        <a href={operator} style="color: #ffffff;">Dashboard</a> |
        <a href="/manage/" style="color: #ffffff;">Admin</a> |
        <a href="/manage/#logs" style="color: #ffffff;">Live Logs</a> |
        <a href="/manage/#power" style="color: #ffffff;">Power</a> |
        <a href="/manage/#update" style="color: #ffffff;">Update</a> |
        <a href="/manage/#configuration" style="color: #ffffff;">Configuration</a>
      {:else}
        <a href="/" style="color: #ffffff;">Dashboard</a> |
        <a href="/admin/" style="color: #ffffff;">Admin</a> |
        <a href="/admin/configure.php" style="color: #ffffff;">Configuration</a>
      {/if}
    </p>
  </div>

  {#if admin}
    <div class="contentwide" id="sysInfo">
      <b>Gateway Hardware Information</b>
      <table style="table-layout: fixed;">
        <tbody>
          <tr>
            <th><a class="tooltip" href="#top">Hostname<br /><span><b>System IP Address:<br />{#each sys?.addresses ?? [] as ip, i}{ip}{#if i < (sys?.addresses?.length ?? 0) - 1},<br />{/if}{/each}</b></span></a></th>
            <th><a class="tooltip" href="#top">Kernel<span><b>Release</b></span></a></th>
            <th colspan="2"><a class="tooltip" href="#top">Platform<span><b>Uptime:<br />{uptime(sys?.uptimeSeconds ?? null)}</b></span></a></th>
            <th><a class="tooltip" href="#top">CPU Load<span><b>CPU Load</b></span></a></th>
            <th><a class="tooltip" href="#top">CPU Temp<span><b>CPU Temp</b></span></a></th>
          </tr>
          <tr>
            <td>{shortHost(sys?.hostname ?? '')}</td>
            <td>{sys?.kernel ?? ''}</td>
            <td colspan="2">{sys?.platform ?? ''}</td>
            <td>{sys?.loadAverage ? sys.loadAverage.map(n => n.toFixed(2)).join(' / ') : ''}</td>
            {#if sys?.cpuTempC != null}
              <td style={tempStyle(sys.cpuTempC)}>{sys.cpuTempC.toFixed(1)}&deg;C / {(sys.cpuTempC * 9 / 5 + 32).toFixed(1)}&deg;F</td>
            {:else}<td></td>{/if}
          </tr>
          <tr><th colspan="6">Service Status</th></tr>
          <tr>
            {#each sys?.classicServices ?? [] as s}<td style={'background: #' + (s.state === 'running' ? '1d1' : 'b55')}>{s.name}</td>{/each}
          </tr>
        </tbody>
      </table>
      <br />
    </div>
  {/if}

  {#if !d}
    <div class="contentwide">{failed ? 'Dashboard data unavailable, retrying…' : 'Loading…'}</div>
  {:else if d.Controller === 'none'}
    <div class="contentwide">
      <h1>No Mode Defined...</h1>
      <p>I don't know what mode I am in, you probably just need to configure me.</p>
      <p>You will be re-directed to the configuration portal in 10 secs</p>
    </div>
  {:else}
    <div class="nav">
      <table>
        <tbody>
          <tr><th colspan="2">Modes Enabled</th></tr>
          {#each pairs(d.Modes) as p}<tr>{#each p as c}<td style={cellStyle(c.state)}>{c.label}</td>{/each}</tr>{/each}
        </tbody>
      </table>
      <br />
      <table>
        <tbody>
          <tr><th colspan="2">Network Status</th></tr>
          {#each pairs(d.Networks) as p}<tr>{#each p as c}<td style={cellStyle(c.state)}>{c.label}</td>{/each}</tr>{/each}
        </tbody>
      </table>
      <br />
      <table>
        <tbody>
          <tr><th colspan="2">Radio Info</th></tr>
          <tr><th>Trx</th><td style={trxStyle(d.TRX.kind, d.TRX.mode)}>{d.TRX.label}</td></tr>
          <tr><th>Tx</th><td style="background: #ffffff;">{d.TX}</td></tr>
          <tr><th>Rx</th><td style="background: #ffffff;">{d.RX}</td></tr>
          {#if d.Firmware}<tr><th>FW</th><td style="background: #ffffff;">{d.Firmware}</td></tr>{/if}
          {#if d.TCXO}<tr><th>TCXO</th><td style="background: #ffffff;">{d.TCXO}</td></tr>{/if}
        </tbody>
      </table>

      {#if d.DStar}
        <br />
        <table>
          <tbody>
            <tr><th colspan="2">D-Star Repeater</th></tr>
            <tr><th>RPT1</th><td style="background: #ffffff;">{d.DStar.RPT1.replaceAll(' ', ' ')}</td></tr>
            <tr><th>RPT2</th><td style="background: #ffffff;">{d.DStar.RPT2.replaceAll(' ', ' ')}</td></tr>
            <tr><th colspan="2">D-Star Network</th></tr>
            {#if d.DStar.APRS}<tr><th>APRS</th><td style="background: #ffffff;">{d.DStar.APRS}</td></tr>{/if}
            {#if d.DStar.IRC}<tr><th>IRC</th><td style="background: #ffffff;">{d.DStar.IRC}</td></tr>{/if}
          </tbody>
        </table>
      {/if}

      {#if d.DMR}
        <br />
        <table>
          <tbody>
            <tr><th colspan="2">DMR Repeater</th></tr>
            <tr><th>DMR ID</th><td style="background: #ffffff;">{d.DMR.ID}</td></tr>
            <tr><th>DMR CC</th><td style="background: #ffffff;">{d.DMR.CC}</td></tr>
            <tr><th>TS1</th>{#if d.DMR.TS1}<td style="background:#0b0;">enabled</td>{:else}<td style="background:#606060; color:#b0b0b0;">disabled</td>{/if}</tr>
            <tr><th>TS2</th>{#if d.DMR.TS2}<td style="background:#0b0;">enabled</td>{:else}<td style="background:#606060; color:#b0b0b0;">disabled</td>{/if}</tr>
            <tr><th colspan="2">DMR Master</th></tr>
            {#if d.DMR.Network}
              {#each d.DMR.Masters ?? [] as m}<tr><td style="background: #ffffff;" colspan="2">{m}</td></tr>{/each}
            {:else}
              <tr><td colspan="2" style="background:#606060; color:#b0b0b0;">No DMR Network</td></tr>
            {/if}
          </tbody>
        </table>
      {/if}

      {#if d.YSF}
        <br />
        <table><tbody>
          <tr><th colspan="2">YSF Network</th></tr>
          <tr><td colspan="2" style="background: #ffffff;">{d.YSF.Link}</td></tr>
        </tbody></table>
      {/if}

      {#each [{ name: 'P25', code: 'NAC', block: d.P25 }, { name: 'NXDN', code: 'RAN', block: d.NXDN }, { name: 'M17', code: 'CAN', block: d.M17 }] as x}
        {#if x.block}
          <br />
          <table><tbody>
            <tr><th colspan="2">{x.name} Radio</th></tr>
            <tr><th style="width:70px">{x.code}</th><td>{x.block.Code}</td></tr>
            <tr><th colspan="2">{x.name} Network</th></tr>
            <tr><td colspan="2" style="background: #ffffff;">{x.block.Link}</td></tr>
          </tbody></table>
        {/if}
      {/each}

      {#if d.POCSAG}
        <br />
        <table><tbody>
          <tr><th colspan="2">POCSAG</th></tr>
          <tr><th>Tx</th><td>{d.POCSAG.TX}</td></tr>
          {#if d.POCSAG.Master}
            <tr><th colspan="2">POCSAG Master</th></tr>
            <tr><td colspan="2" style="background: #ffffff;">{d.POCSAG.Master}</td></tr>
          {/if}
        </tbody></table>
      {/if}
    </div>

    <div class="content">
      {#if admin}<LinkManagers />{/if}
      <div id="lastHerd">
        <b>Gateway Activity</b>
        <table>
          <tbody>
            {@render head(false)}
            {#each d.Heard ?? [] as r}{@render row(r, false)}{/each}
          </tbody>
        </table>
      </div>
      <br />
      <div id="localTxs">
        <b>Local RF Activity</b>
        <table>
          <tbody>
            {@render head(true)}
            {#each d.Local ?? [] as r}{@render row(r, true)}{/each}
          </tbody>
        </table>
      </div>
    </div>
  {/if}

  <div class="footer">
    Pi-Star / Pi-Star Dashboard, &copy; Andy Taylor (MW0MWZ) 2014-{year}.<br />
    ircDDBGateway Dashboard by Hans-J. Barthen (DL5DI),<br />
    MMDVMDash developed by Kim Huebel (DG9VH), <br />
    Need help? Click <a style="color: #ffffff;" href="https://www.facebook.com/groups/pistarusergroup/" target="_blank" rel="noopener noreferrer">here for the Facebook Group</a><br />
    or Click <a style="color: #ffffff;" href="https://forum.pistar.uk/" target="_blank" rel="noopener noreferrer">here to join the Support Forum</a><br />
    Get your copy of Pi-Star from <a style="color: #ffffff;" href="http://www.pistar.uk/downloads/" target="_blank" rel="noopener noreferrer">here</a>.<br />
  </div>
</div>

<style>
  /* css/pistar-css.php with its built-in palette */
  .container { width: 820px; text-align: left; margin: auto; border-radius: 10px; box-shadow: 3px 3px 3px #707070; background: #ffffff; }
  :global(body.pistar-classic) { margin: 8px; font: 12px verdana, arial, sans-serif; background: var(--ps-page, #edf0f5); color: #000000; }
  .header { background: var(--ps-banners, #dd4b39); text-decoration: none; color: var(--ps-banner-text, #ffffff); font-family: verdana, arial, sans-serif; text-align: left; padding: 5px 0; border-radius: 10px 10px 0 0; }
  .header p { margin: 12px 0; }
  .nav { float: left; margin: 0; padding: 3px; width: 160px; background: #242d31; font-weight: normal; min-height: 100%; }
  .content { margin: 0 0 0 166px; padding: 1px 5px 5px 5px; color: var(--ps-text, #000000); background: var(--ps-content, #ffffff); text-align: center; }
  .contentwide { padding: 5px; color: var(--ps-text, #000000); background: var(--ps-content, #ffffff); text-align: center; }
  .footer { background: var(--ps-banners, #dd4b39); text-decoration: none; color: var(--ps-banner-text, #ffffff); font-family: verdana, arial, sans-serif; font-size: 9px; text-align: center; padding: 10px 0; border-radius: 0 0 10px 10px; clear: both; }
  .container :global(table) { vertical-align: middle; text-align: center; empty-cells: show; padding: 0; border-collapse: collapse; border-color: #000000; border-style: solid; border-spacing: 4px; border-width: 2px; text-decoration: none; color: #ffffff; background: #000000; font-family: verdana, arial, sans-serif; width: 100%; white-space: nowrap; }
  .container :global(th) { font-family: "Lucidia Console", Monaco, monospace; text-shadow: 1px 1px var(--ps-head-drop, #8b0000); text-decoration: none; background: var(--ps-banners, #dd4b39); border: 1px solid #c0c0c0; }
  .container :global(tr:nth-child(even)) { background: var(--ps-even, #f7f7f7); }
  .container :global(tr:nth-child(odd)) { background: var(--ps-odd, #d0d0d0); }
  .container :global(td) { color: #000000; font-family: "Lucidia Console", Monaco, monospace; text-decoration: none; border: 1px solid #000000; }
  .container :global(a) { text-decoration: none; }
  .container :global(a:link), .container :global(a:visited) { text-decoration: none; color: #0000e0; font-weight: normal; }
  h1 { text-shadow: 2px 2px var(--ps-banner-drop, #303030); text-align: center; font-size: 2em; margin: 0.67em 0; }
  .container :global(a.tooltip), .container :global(a.tooltip:link), .container :global(a.tooltip:visited), .container :global(a.tooltip:active) { text-decoration: none; position: relative; color: #ffffff; }
  .container :global(a.tooltip:hover) { text-shadow: none; color: #ffffff; background: transparent; }
  .container :global(a.tooltip span) { text-shadow: none; display: none; }
  .container :global(a.tooltip:hover span) { text-shadow: none; display: block; position: absolute; top: 20px; left: 0; z-index: 100; color: #000000; border: 1px solid #000000; background: #f7f7f7; font: 12px Verdana, sans-serif; text-align: left; white-space: nowrap; }
  .container :global(th:last-child a.tooltip:hover span) { left: auto; right: 0; }
  .container :global(a.tooltip span b) { display: block; color: #000000; margin: 0; padding: 0; font-size: 12px; font-weight: bold; border: 0; border-bottom: 1px solid black; background: #d0d0d0; }

  /* css/pistar-css-mini.php, linked for screens up to 829px */
  @media screen and (max-width: 829px) {
    .container { width: 100%; }
    .nav { display: none; }
    .content { margin: 0; padding: 5px; font-size: 1.4em; }
    .content > div { overflow-x: auto; }
    .contentwide { font-size: 1.4em; }
  }
</style>
