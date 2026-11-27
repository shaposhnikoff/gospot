<script lang="ts">
  // /kiosk: incoming calls on a wall monitor next to the hotspot. Readable
  // from a few metres; everything is visible without hover or scrolling.
  import { onMount } from 'svelte';
  import { applyTheme } from './classic';
  import { type Conn, type Recent, POLL_MS, REQUEST_TIMEOUT_MS, RECENT_CALLS, initialConn, accept, fail, resume, kioskView } from './kiosk';

  let conn = $state<Conn>(initialConn(Date.now()));
  let now = $state(Date.now());
  const view = $derived(kioskView(conn, now));
  const slots = $derived.by(() => {
    const rows: (Recent | null)[] = 'recent' in view ? [...view.recent] : [];
    while (rows.length < RECENT_CALLS) rows.push(null);
    return rows;
  });
  const wall = (t: number) => new Date(t).toTimeString().slice(0, 8);

  onMount(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    let ticker: ReturnType<typeof setInterval> | undefined;
    let inflight = false;
    let stopped = false;

    async function poll() {
      timer = undefined;
      if (stopped || document.hidden || inflight) return;
      inflight = true;
      try {
        const r = await fetch('/api/dashboard', { cache: 'no-store', headers: { Accept: 'application/json' }, signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS) });
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        conn = accept(conn, await r.json(), Date.now());
        applyTheme(conn.data?.Theme);
        // The board was updated while this screen stayed open.
        if (conn.reload) location.reload();
      } catch {
        conn = fail(conn, Date.now());
      } finally {
        inflight = false;
        now = Date.now();
      }
      if (!stopped && !document.hidden) timer = setTimeout(poll, POLL_MS);
    }

    function start() {
      conn = resume(conn, Date.now());
      now = Date.now();
      clearInterval(ticker);
      // The duration counter and clock advance between polls.
      ticker = setInterval(() => { now = Date.now(); }, 250);
      if (timer === undefined) void poll();
    }

    function stop() {
      clearTimeout(timer);
      timer = undefined;
      clearInterval(ticker);
    }

    const visibility = () => (document.hidden ? stop() : start());
    document.addEventListener('visibilitychange', visibility);
    document.documentElement.classList.add('kiosk-page');
    if (!document.hidden) start();
    return () => {
      stopped = true;
      stop();
      document.removeEventListener('visibilitychange', visibility);
      document.documentElement.classList.remove('kiosk-page');
    };
  });
</script>

<svelte:head>
  <title>{view.state === 'onair' ? `On air: ${view.call.callsign}` : 'station' in view && view.station ? `${view.station.callsign} · Calls` : 'GoSpot · Calls'}</title>
</svelte:head>

<div class="kiosk" data-state={view.state}>
  <header>
    <div class="station">
      <b>{'station' in view && view.station ? view.station.callsign : 'GoSpot'}</b>
      {#if 'station' in view && view.station?.freq}<span class="freq">{view.station.freq}</span>{/if}
    </div>
    <div class="status">
      {#if view.state === 'lost'}
        <span class="link lost">● Connection lost</span>
      {:else if view.state === 'connecting'}
        <span class="link">● Connecting</span>
      {:else}
        <span class="link ok">● Live</span>
      {/if}
      <span class="wall">{wall(now)}</span>
    </div>
  </header>

  {#if view.state === 'onair'}
    <section class="card onair {view.call.kind}">
      <div class="who">
        <div class="badge">ON AIR · {view.call.sourceLabel}{#if view.more > 0}<span> · +{view.more} more</span>{/if}</div>
        <div class="callsign">{view.call.callsign}</div>
        <div class="name">{view.call.name}</div>
      </div>
      <div class="facts">
        <div class="fact target"><span class="k">Target</span><span class="v">{view.call.target}</span><span class="sub">{view.call.targetName}</span></div>
        <div class="fact"><span class="k">Mode</span><span class="v">{view.call.mode}{#if view.call.slot}&nbsp;{view.call.slot}{/if}</span></div>
        <div class="fact timer"><span class="k">Duration</span><span class="v">{view.call.elapsed}</span></div>
      </div>
    </section>
  {:else if view.state === 'idle'}
    <section class="card idle" class:offline={view.offline}>
      <div class="who">
        <div class="headline">{view.offline ? 'Modem offline' : 'Listening'}</div>
        <div class="since">
          {#if !view.history}Call history unavailable
          {:else if view.ago}Last call {view.ago}
          {:else}No calls in the last 24 hours{/if}
        </div>
      </div>
      {#if view.last}
        <div class="last">
          <span class="k">Last caller</span><span class="v"><b>{view.last.callsign}</b> <i>{view.last.name}</i></span>
          <span class="k">Target</span><span class="v"><b>{view.last.target}</b> <i>{view.last.targetName}</i></span>
          <span class="k">{view.last.source === 'RF' ? 'RF' : 'Network'}</span>
          <span class="v"><b>{view.last.duration}</b> <em class="q {view.last.level}">{view.last.quality}</em> <i>{view.last.signal}</i></span>
        </div>
      {/if}
    </section>
  {:else if view.state === 'lost'}
    <section class="card lost">
      <div class="who">
        <div class="headline">Connection lost</div>
        <div class="since">No reply from the hotspot for {view.lostFor}. Retrying every second; the list below is the last one received.</div>
      </div>
    </section>
  {:else}
    <section class="card connecting"><div class="who"><div class="headline">Connecting…</div></div></section>
  {/if}

  <section class="calls" class:stale={view.state === 'lost'}>
    <div class="row head">
      <span class="time">Time</span><span class="cs">Callsign</span><span class="nm">Name</span><span class="tg">Target</span><span class="tgn"></span>
      <span class="mode">Mode</span><span class="dur">Dur</span><span class="qc">Loss/BER</span>
    </div>
    {#if view.state !== 'connecting' && slots.every(s => s === null)}
      <div class="empty">No calls heard yet</div>
    {:else}
      {#each slots as r, i (i)}
        {#if r}
          <div class="row" class:rf={r.source === 'RF'}>
            <span class="time">{r.clock}</span>
            <span class="cs">{r.callsign}</span>
            <span class="nm">{r.name}</span>
            <span class="tg">{r.target}</span>
            <span class="tgn">{r.targetName}</span>
            <span class="mode m-{r.mode.replace(/[^A-Za-z0-9]/g, '')}">{r.modeLabel}<small>{r.source === 'RF' ? 'RF' : 'Net'}</small></span>
            <span class="dur">{r.duration}</span>
            <span class="qc"><em class="q {r.level}">{r.quality}</em></span>
          </div>
        {:else}
          <div class="row filler" aria-hidden="true"></div>
        {/if}
      {/each}
    {/if}
  </section>
</div>

<style>
  :global(html.kiosk-page), :global(html.kiosk-page body) {
    margin: 0; padding: 0; width: 100%; height: 100%; overflow: hidden; background: #0b0b0b;
  }
  :global(html.kiosk-page #app) { width: 100%; height: 100%; }

  .kiosk {
    --accent: var(--ps-banners, #dd4b39);
    --accent-text: var(--ps-banner-text, #ffffff);
    --accent-drop: var(--ps-banner-drop, #303030);
    --bg: #0b0b0b; --panel: #171717; --row-a: #121212; --row-b: #1c1c1c;
    --text: #ffffff; --muted: #c9c9c9; --dim: #9a9a9a;
    box-sizing: border-box; width: 100vw; height: 100vh; height: 100dvh; overflow: hidden;
    display: grid; grid-template-columns: minmax(0, 1fr); grid-template-rows: 8vh 37vh minmax(0, 1fr); gap: 1.2vh; padding: 0 0 1.2vh;
    background: var(--bg); color: var(--text); cursor: none;
    font-family: system-ui, -apple-system, "Segoe UI", Roboto, "DejaVu Sans", "Liberation Sans", Arial, sans-serif;
    font-variant-numeric: tabular-nums;
  }
  .kiosk * { box-sizing: border-box; }
  span, div, b, i, em { min-width: 0; }
  i { font-style: normal; }

  header {
    display: flex; align-items: center; justify-content: space-between; gap: 2vw; padding: 0 2.5vw;
    background: var(--accent); color: var(--accent-text); font-size: min(4.2vh, 3.4vw); white-space: nowrap;
    text-shadow: 0.06em 0.06em 0 var(--accent-drop);
  }
  .station { display: flex; align-items: baseline; gap: 1.5vw; overflow: hidden; }
  .station b { flex: none; font-size: 1.15em; letter-spacing: 0.04em; }
  .freq { opacity: 0.9; overflow: hidden; text-overflow: ellipsis; font-size: 0.8em; }
  .status { display: flex; align-items: center; gap: 2vw; flex: none; }
  .link { font-size: 0.75em; padding: 0.15em 0.6em; border-radius: 0.4em; background: rgba(0, 0, 0, 0.35); text-shadow: none; }
  .link.ok { color: #6f6; }
  .link.lost { background: #fa0; color: #000; font-weight: 700; }
  .wall { font-weight: 700; }

  .card {
    margin: 0 2.5vw; border-radius: 1.2vh; overflow: hidden; display: grid;
    grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr); align-items: center; gap: 2vw; padding: 2vh 2.5vw;
    background: var(--panel); border-left: 1.2vh solid var(--accent);
  }
  .who { display: flex; flex-direction: column; justify-content: center; gap: 0.6vh; overflow: hidden; }
  .badge { font-size: min(4vh, 3vw); font-weight: 800; letter-spacing: 0.08em; white-space: nowrap; }
  .callsign {
    font-size: min(15vh, 10.5vw); line-height: 1; font-weight: 900; letter-spacing: 0.03em;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .name, .since { font-size: min(6vh, 4.4vw); font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .headline { font-size: min(11vh, 8vw); font-weight: 900; line-height: 1.05; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .facts { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 1.4vh 2vw; align-content: center; }
  .fact { display: flex; flex-direction: column; overflow: hidden; white-space: nowrap; }
  .fact.target { grid-column: 1 / -1; }
  .fact .k, .last .k { font-size: min(2.6vh, 2vw); text-transform: uppercase; letter-spacing: 0.1em; font-weight: 700; opacity: 0.8; }
  .fact .v { font-size: min(6.5vh, 4.6vw); font-weight: 800; line-height: 1.1; overflow: hidden; text-overflow: ellipsis; }
  .fact .sub { font-size: min(3.6vh, 2.6vw); font-weight: 600; overflow: hidden; text-overflow: ellipsis; min-height: 1.2em; }
  .fact.timer .v { font-size: min(9vh, 6.4vw); }

  /* Idle: the last call in three label/value lines. */
  .last { display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: baseline; gap: 1.6vh 1.5vw; }
  .last .v { font-size: min(4.4vh, 3vw); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .last .v b { font-weight: 800; }
  .last .v i { color: var(--muted); font-weight: 600; margin-left: 0.3em; }
  .last .v em + i { font-size: 0.8em; }

  /* Classic TRX colours (repeaterinfo.php): RX from RF green, TX from the network red. */
  .card.onair { color: #000; border-left-color: #000; }
  .card.onair.rx { background: #4aa361; }
  .card.onair.tx { background: #f33; }
  .card.idle .headline { color: #3d3; }
  .card.idle.offline .headline { color: #b0b0b0; }
  .card.idle .since { color: var(--muted); }
  .card.lost { background: #fa0; color: #000; border-left-color: #000; grid-template-columns: minmax(0, 1fr); }
  .card.lost .since { white-space: normal; font-size: min(4.6vh, 3.4vw); }
  .card.connecting { grid-template-columns: minmax(0, 1fr); }
  .card.connecting .headline { color: var(--muted); }

  .q { font-style: normal; border-radius: 0.3em; padding: 0 0.3em; }
  .q.good { background: #1d1; color: #000; }
  .q.warn { background: #fa0; color: #000; }
  .q.bad { background: #f33; color: #000; }

  .calls {
    container-type: size; margin: 0 2.5vw; overflow: hidden;
    display: grid; grid-template-rows: 0.7fr repeat(10, minmax(0, 1fr));
  }
  .calls.stale { opacity: 0.45; }
  .row {
    display: grid; align-items: center; gap: 0 1.2cqw; padding: 0 1.2cqw; white-space: nowrap; overflow: hidden;
    grid-template-columns: 7.6ch minmax(0, 1.1fr) minmax(0, 1fr) 8.6ch minmax(0, 1fr) 11.5ch 6.5ch 10ch;
    grid-template-areas: "time cs nm tg tgn mode dur qc";
    font-size: min(calc(100cqh / 10.7 * 0.56), 2.1cqw);
  }
  .row:nth-child(even) { background: var(--row-a); }
  .row:nth-child(odd) { background: var(--row-b); }
  .row.head {
    background: var(--accent); color: var(--accent-text); font-weight: 700; text-transform: uppercase;
    letter-spacing: 0.03em; font-size: min(calc(100cqh / 10.7 * 0.34), 1.5cqw);
  }
  .row > span { overflow: hidden; text-overflow: ellipsis; }
  .time { grid-area: time; color: var(--muted); }
  .cs { grid-area: cs; font-weight: 800; letter-spacing: 0.02em; }
  .nm { grid-area: nm; color: var(--muted); }
  .tg { grid-area: tg; font-weight: 700; }
  .tgn { grid-area: tgn; color: var(--muted); }
  .row.head .tg { overflow: visible; }
  .mode { grid-area: mode; font-weight: 700; }
  .dur { grid-area: dur; text-align: right; }
  .qc { grid-area: qc; text-align: right; }
  .row.head > span { color: inherit; }
  .mode small { font-size: 0.7em; margin-left: 0.4em; color: var(--dim); font-weight: 600; }
  .row.rf .mode small { color: #6f6; }
  /* Classic mode colours (hang colours in repeaterinfo.php). */
  .m-DMR { color: #f93; } .m-DStar { color: #ade; } .m-YSF { color: #ff9; } .m-P25 { color: #f9f; } .m-NXDN { color: #c9f; } .m-M17 { color: #f9f; }
  .row.filler { background: transparent; }
  .empty { grid-row: 2 / -1; display: grid; place-items: center; color: var(--dim); font-size: min(6cqh, 3.5cqw); }

  /* Portrait TVs (1080x1920): taller card; each call takes two lines
     (callsign over name, talkgroup over its name, mode under the time). */
  @media (orientation: portrait) {
    .kiosk { grid-template-rows: 5vh 44vh minmax(0, 1fr); }
    header { font-size: min(3vh, 4.6vw); }
    .card { grid-template-columns: minmax(0, 1fr); grid-template-rows: auto auto; align-content: center; gap: 2.2vh; padding: 1.8vh 4vw; }
    .badge { font-size: min(2.6vh, 4vw); }
    .callsign { font-size: min(9vh, 15vw); }
    .name, .since { font-size: min(3.6vh, 5.6vw); }
    .card.lost .since { font-size: min(3vh, 4.8vw); }
    .headline { font-size: min(7vh, 9.5vw); }
    .fact .k, .last .k { font-size: min(1.8vh, 3vw); }
    .fact .v { font-size: min(3.6vh, 6vw); }
    .fact .sub { font-size: min(2.4vh, 4vw); }
    .last .v { font-size: min(3vh, 4.8vw); }
    .fact.timer .v { font-size: min(5vh, 8.5vw); }
    .row {
      grid-template-columns: 8.6ch minmax(0, 0.95fr) minmax(0, 1.05fr) 6.5ch 9.5ch;
      grid-template-rows: 1fr 0.8fr; align-content: center; row-gap: 0.1em;
      grid-template-areas: "time cs tg dur qc" "mode nm tgn dur qc";
      font-size: min(calc(100cqh / 10.7 * 0.36), 3.6cqw);
    }
    .row .nm, .row .tgn, .row .mode { font-size: 0.8em; }
    .row.head { grid-template-rows: 1fr; grid-template-areas: "time cs tg dur qc"; font-size: min(calc(100cqh / 10.7 * 0.3), 2.6cqw); }
    .row.head .nm, .row.head .tgn, .row.head .mode { display: none; }
  }
</style>
