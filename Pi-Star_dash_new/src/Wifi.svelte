<script lang="ts">
  // admin/wifi.php: "#wifi" is ?page=wlan0_info (status, Reset WiFi Adapter),
  // "#wifi-conf" is ?page=wpa_conf (country, networks, Scan, Save). The board
  // never sends a stored PSK: a blank PSK keeps the saved key.
  import { onMount } from 'svelte';
  import GoSpotFrame from './GoSpotFrame.svelte';
  import { convertToChannel, convertToSecurity, ifconfigBytes, ssidOk, pskOk } from './wifi';

  type Status = {
    present: boolean; up: boolean; mac?: string; ip?: string; netmask?: string;
    rxPackets?: number; rxBytes?: number; txPackets?: number; txBytes?: number;
    ssid?: string; bssid?: string; freq?: string; signal?: string; bitrate?: string; txPower?: string;
  };
  type State = { csrf: string; revision: string; backend: string; interface: string; country: string; countries: string[];
    networks: { id: string; ssid: string; secured: boolean }[]; status: Status };
  type Net = { id: string; ssid: string; psk: string; secured: boolean; stored: string };
  type ScanRow = { bssid: string; freq: string; signal: string; security: string; ssid: string };

  const viewOf = () => location.hash === '#wifi-conf' ? 'conf' : 'info';
  let view = $state<'info' | 'conf'>(viewOf());
  let data = $state<State | null>(null);
  let nets = $state<Net[]>([]);
  let country = $state('JP');
  let scan = $state<ScanRow[] | null>(null);
  let busy = $state(false);
  let scanning = $state(false);
  let message = $state('');
  let note = $state('');
  let error = $state('');
  let notSaved = $state<string[] | null>(null);

  const invalid = $derived(nets.some(n => !ssidOk(n.ssid) || !pskOk(n.psk)));
  const iface = $derived(data?.interface ?? 'wlan0');

  async function load() {
    const r = await fetch('/manage/api/wifi', { cache: 'no-store' }).catch(() => null);
    if (!r || !r.ok) { error = r ? (await r.text()).trim() : 'No response from the board'; return; }
    const d: State = await r.json();
    data = d;
    country = d.country;
    nets = d.networks.map(n => ({ id: n.id, ssid: n.ssid, psk: '', secured: n.secured, stored: n.ssid }));
  }

  onMount(() => {
    const changed = () => { view = viewOf(); scan = null; error = ''; notSaved = null; void load(); };
    window.addEventListener('hashchange', changed);
    void load();
    // wlan0_info reloads itself every 15 seconds.
    const timer = setInterval(() => { if (view === 'info' && !busy) void load(); }, 15000);
    return () => { window.removeEventListener('hashchange', changed); clearInterval(timer); };
  });

  async function post(body: Record<string, unknown>): Promise<Response | null> {
    return fetch('/manage/api/wifi', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-GoSpot-CSRF': data?.csrf ?? '' },
      body: JSON.stringify(body),
    }).catch(() => null);
  }

  function clear() { message = ''; note = ''; error = ''; notSaved = null; }

  async function action(name: 'reset' | 'up' | 'down') {
    if (busy) return;
    busy = true; clear();
    const r = await post({ action: name });
    busy = false;
    if (!r || !r.ok) { error = r ? (await r.text()).trim() : 'No response from the board'; return; }
    message = (await r.json()).message;
    await load();
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    if (busy || invalid) return;
    busy = true; clear();
    const r = await post({ action: 'save', revision: data?.revision ?? '', country,
      networks: nets.map(n => n.id ? { id: n.id, ssid: n.ssid, psk: n.psk } : { ssid: n.ssid, psk: n.psk }) });
    busy = false;
    if (!r) { error = 'No response from the board'; return; }
    const text = (await r.text()).trim();
    if (r.status === 422) { notSaved = text.split('\n'); return; }
    if (!r.ok) { error = text; return; }
    const reply = JSON.parse(text);
    message = reply.message; note = reply.note ?? '';
    // wifi.php returns to the information page after a save.
    if (location.hash !== '#wifi') location.hash = '#wifi';
    else await load();
  }

  async function runScan() {
    if (busy) return;
    busy = scanning = true; clear(); scan = null;
    const r = await post({ action: 'scan' });
    busy = scanning = false;
    if (!r || !r.ok) { error = r ? (await r.text()).trim() : 'No response from the board'; return; }
    scan = (await r.json()).networks;
  }

  function add(ssid = '') { nets.push({ id: '', ssid, psk: '', secured: false, stored: '' }); }
  function remove(i: number) { nets.splice(i, 1); }
  // CheckSSID / CheckPSK: red while invalid, green once edited and valid.
  const ssidStyle = (n: Net) => n.ssid === n.stored ? '' : `background:${ssidOk(n.ssid) ? '#D0FFD0' : '#FFD0D0'};`;
  const pskStyle = (n: Net) => n.psk === '' ? '' : `background:${pskOk(n.psk) ? '#D0FFD0' : '#FFD0D0'};`;
  const line = (label: string, width: number) => label.padStart(width, ' ') + ' : ';
</script>

<GoSpotFrame title="WiFi Config" links={['Dashboard', 'Admin', 'Configuration', 'Expert', 'Power', 'Update', 'Backup/Restore']}>
  <div class="wifi">
    {#if message}<table><tbody><tr><td style="background:#1d1;">{message}{#if note}<br />{note}{/if}</td></tr></tbody></table><br />{/if}
    {#if error}<div role="alert"><table><tbody><tr><td style="background:#f33;">{error}</td></tr></tbody></table></div><br />{/if}
    {#if !data}
      {#if !error}Loading...{/if}
    {:else if view === 'info'}
      {@const s = data.status}
      <div class="infobox">
        <input type="button" value="Refresh" disabled={busy} onclick={() => { clear(); void load(); }} />
        <input type="button" value="Reset WiFi Adapter" disabled={busy} onclick={() => action('reset')} />
        <input type="button" value="Configure WiFi" onclick={() => { location.hash = '#wifi-conf'; }} />
        <div class="infoheader">Wireless Information and Statistics</div>
        <div class="intinfo"><div class="intheader">Interface Information</div>
          <span class="pre">{line('Interface Name', 20)}</span>{iface}<br />
          <span class="pre">{line('Interface Status', 20)}</span>{#if !s.present}<span style="color:red">Interface is not present</span>{:else if s.up}<span style="color:green">Interface is up</span>{:else}<span style="color:red">Interface is down</span>{/if}<br />
          <span class="pre">{line('IP Address', 20)}</span>{s.ip ?? ''}<br />
          <span class="pre">{line('Subnet Mask', 20)}</span>{s.netmask ?? ''}<br />
          <span class="pre">{line('Mac Address', 20)}</span>{s.mac ?? ''}<br />
          <br />
          <div class="intheader">Interface Statistics</div>
          <span class="pre">{line('Received Packets', 20)}</span>{s.rxPackets ?? ''}<br />
          <span class="pre">{line('Received Bytes', 20)}</span>{ifconfigBytes(s.rxBytes)}<br />
          <span class="pre">{line('Transferred Packets', 20)}</span>{s.txPackets ?? ''}<br />
          <span class="pre">{line('Transferred Bytes', 20)}</span>{ifconfigBytes(s.txBytes)}<br />
          <br />
        </div>
        <div class="wifiinfo"><div class="intheader">Wireless Information</div>
          <span class="pre">{line('Connected To', 15)}</span>{s.ssid ?? ''}<br />
          <span class="pre">{line('AP Mac Address', 15)}</span>{s.bssid ?? ''}<br />
          <br />
          <span class="pre">{line('Bitrate', 15)}</span>{s.bitrate ?? ''}<br />
          <span class="pre">{line('Signal Level', 15)}</span>{s.signal ?? ''}<br />
          <br />
          {#if s.txPower}<span class="pre">{line('Transmit Power', 15)}</span>{s.txPower}{/if}<br />
          <br />
          {#if s.freq && convertToChannel(s.freq) !== 'Invalid Channel'}<span class="pre">{line('Channel Info', 15)}</span>{convertToChannel(s.freq)} ({(Number(s.freq) / 1000).toFixed(3)} GHz){/if}<br />
          <span class="pre">{line('WiFi Country', 15)}</span>{data.country}<br />
          <br />
          <br />
        </div>
        <br />
      </div>
      <div class="intfooter">Information provided by ip and iw</div>
    {:else}
      <form onsubmit={save}>
        <input type="button" value="WiFi Info" onclick={() => { location.hash = '#wifi'; }} /><br />
        <div class="network" id="networkbox">
          WiFi Regulatory Domain (Country Code) : <select bind:value={country} aria-label="WiFi Country Code">
            {#each data.countries as c (c)}<option value={c}>{c}</option>{/each}
          </select><br />
          {#each nets as n, i (n)}
            <div class="NetworkBoxes">Network {i}
              <input type="button" value="Delete" aria-label={'Delete network ' + i} onclick={() => remove(i)} /><br />
              <span class="tableft">SSID :</span><input type="text" bind:value={n.ssid} aria-label={'SSID ' + i} style={ssidStyle(n)} autocomplete="off" /><br />
              <span class="tableft">PSK :</span><input type="password" bind:value={n.psk} aria-label={'PSK ' + i} style={pskStyle(n)}
                placeholder={n.id && n.secured ? '(saved; leave blank to keep)' : ''} autocomplete="new-password" /><br /><br />
            </div>
          {/each}
        </div>
        <div class="infobox">
          <input type="button" value="Scan for Networks (10 secs)" disabled={busy} onclick={runScan} />
          <input type="button" value="Add Network" onclick={() => add()} />
          <input type="submit" value="Save (and connect)" disabled={busy || invalid} />
        </div>
      </form>
      {#if notSaved}
        <div class="result" role="alert"><b>WiFi configuration NOT saved.</b><br />
          {#each notSaved as e}&nbsp;&nbsp;- {e}<br />{/each}
          Please correct the input and try again.<br /></div>
      {/if}
      {#if scanning}<div class="result">Scanning for networks...</div>{/if}
      {#if scan}
        <div class="result"><br />Networks found : <br />
          <table>
            <tbody>
              <tr><th>Connect</th><th>SSID</th><th>Channel</th><th>Signal</th><th>Security</th></tr>
              {#each scan as row (row.bssid)}
                <tr>
                  <td style="text-align: left;"><input type="button" value="Select" onclick={() => add(row.ssid)} /></td>
                  <td style="text-align: left;">{row.ssid}</td>
                  <td style="text-align: left;">{convertToChannel(row.freq)}</td>
                  <td>{row.signal}</td>
                  <td style="text-align: left;">{convertToSecurity(row.security)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    {/if}
  </div>
</GoSpotFrame>

<style>
  /* admin/wifi/styles.php with the built-in palette. */
  .wifi { text-align: left; }
  .pre { white-space: pre; }
  .network { border: 1px solid; text-align: left; padding-top: 5px; padding-left: 5px; background-color: var(--ps-odd, #d0d0d0); }
  .tableft { display: inline-block; text-align: right; width: 120px; }
  .wifi input[type=text], .wifi input[type=password] { border: 1px solid; }
  .wifi input[type=button], .wifi input[type=submit] { border: 1px solid; border-radius: 5px; }
  .infoheader { width: 100%; text-align: center; margin-top: 10px; border-bottom: 1px solid; }
  .infobox { background-color: var(--ps-banners, #dd4b39); font-weight: bold; width: 100%; overflow: hidden; }
  .intinfo { background-color: var(--ps-odd, #d0d0d0); color: var(--ps-text, #000000); width: 398px; text-align: left; border-right: 1px solid;
    border-left: 1px solid; float: left; border-bottom: 1px solid; font-family: "Courier New", Courier, monospace; font-weight: normal; }
  .wifiinfo { background-color: var(--ps-odd, #d0d0d0); color: var(--ps-text, #000000); margin: 0 0 0 400px; border-right: 1px solid;
    text-align: left; border-bottom: 1px solid; font-family: "Courier New", Courier, monospace; font-weight: normal; }
  .intheader { background-color: var(--ps-banners, #dd4b39); color: var(--ps-banner-text, #ffffff); text-align: center; width: 100%; font-weight: bold; }
  .intfooter { background-color: var(--ps-banners, #dd4b39); color: var(--ps-banner-text, #ffffff); width: 100%; border-top: 1px solid; text-align: center; }
  .result { text-align: left; white-space: normal; }
  /* Phones: stack the two information boxes instead of the fixed 398px columns. */
  @media screen and (max-width: 829px) {
    .intinfo { float: none; width: auto; }
    .wifiinfo { margin: 0; border-left: 1px solid; }
    .tableft { width: auto; }
  }
</style>
