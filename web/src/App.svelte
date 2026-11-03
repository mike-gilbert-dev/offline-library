<script>
  import { onMount } from 'svelte';
  import PackCard from './PackCard.svelte';

  let lib = $state(null);
  let loadError = $state('');
  let serverDown = $state(false);
  let scanning = $state(false);

  async function api(url, options) {
    const res = await fetch(url, options);
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
  }

  async function refresh(rescan = false) {
    scanning = rescan;
    try {
      lib = await api(rescan ? '/api/rescan' : '/api/library', rescan ? { method: 'POST' } : undefined);
      loadError = '';
      serverDown = false;
    } catch (e) {
      loadError = e?.message ?? String(e);
    } finally {
      scanning = false;
    }
  }

  // Poll a cheap status check so unplugging or reconnecting the drive
  // updates the page without the user doing anything.
  async function checkStatus() {
    try {
      const s = await api('/api/status');
      serverDown = false;
      if (lib && s.available !== lib.available) await refresh(true);
    } catch {
      serverDown = true;
    }
  }

  onMount(() => {
    refresh();
    const id = setInterval(checkStatus, 3000);
    return () => clearInterval(id);
  });

  const readyCount = $derived(lib?.packs.filter((p) => p.status === 'ok').length ?? 0);
</script>

<header>
  <div>
    <h1>{lib?.available ? lib.title : 'Offline Library'}</h1>
    {#if lib?.available}
      <p class="sub">{readyCount} of {lib.packs.length} packs ready</p>
    {/if}
  </div>
  <button onclick={() => refresh(true)} disabled={scanning || serverDown}>
    {scanning ? 'Scanning…' : 'Rescan'}
  </button>
</header>

<main>
  {#if serverDown}
    <section class="notice error">
      <h2>The library program has stopped</h2>
      <p>Run the launcher on the drive again (for example <code>start-windows.exe</code>). It will open a new browser window.</p>
    </section>
  {:else if loadError}
    <section class="notice error">
      <h2>Couldn't load the library</h2>
      <p>{loadError}</p>
    </section>
  {:else if !lib}
    <p class="muted">Loading…</p>
  {:else if !lib.available}
    <section class="notice warn">
      <h2>Library not available</h2>
      {#each lib.problems as problem}<p>{problem}</p>{/each}
      <p class="muted">Looking in <code>{lib.root}</code></p>
      <p>Reconnect the drive, then press <strong>Rescan</strong>.</p>
    </section>
  {:else}
    {#if lib.problems.length}
      <section class="notice warn">
        <h2>This library has problems</h2>
        <ul>{#each lib.problems as problem}<li>{problem}</li>{/each}</ul>
      </section>
    {/if}
    {#if lib.packs.length === 0}
      <p class="muted">This library doesn't list any packs yet.</p>
    {:else}
      <div class="grid">
        {#each lib.packs as pack (pack.manifestPath)}
          <PackCard {pack} />
        {/each}
      </div>
    {/if}
    <footer class="muted">Library folder: <code>{lib.root}</code></footer>
  {/if}
</main>
