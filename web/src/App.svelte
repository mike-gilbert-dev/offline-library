<script lang="ts">
  import { onMount } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import type { Library } from '$lib/types.js';
  import PackCard from './PackCard.svelte';

  import LibraryIcon from '@lucide/svelte/icons/library-big';
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
  import UnplugIcon from '@lucide/svelte/icons/unplug';
  import ServerOffIcon from '@lucide/svelte/icons/server-off';
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';

  let lib = $state<Library | null>(null);
  let loadError = $state('');
  let serverDown = $state(false);
  let scanning = $state(false);

  async function api<T>(url: string, options?: RequestInit): Promise<T> {
    const res = await fetch(url, options);
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
  }

  async function refresh(rescan = false) {
    scanning = rescan;
    try {
      lib = await api<Library>(rescan ? '/api/rescan' : '/api/library', rescan ? { method: 'POST' } : undefined);
      loadError = '';
      serverDown = false;
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
    } finally {
      scanning = false;
    }
  }

  // Poll a cheap status check so unplugging or reconnecting the drive
  // updates the page without the user doing anything.
  async function checkStatus() {
    try {
      const s = await api<{ available: boolean }>('/api/status');
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

<header class="bg-background/80 sticky top-0 z-10 border-b backdrop-blur">
  <div class="mx-auto flex max-w-6xl items-center justify-between gap-4 px-4 py-4 sm:px-6">
    <div class="flex items-center gap-3">
      <div class="bg-primary text-primary-foreground flex size-9 items-center justify-center rounded-lg">
        <LibraryIcon class="size-5" />
      </div>
      <div>
        <h1 class="text-lg font-semibold leading-tight tracking-tight">
          {lib?.available ? lib.title : 'Offline Library'}
        </h1>
        {#if lib?.available}
          <p class="text-muted-foreground text-sm">{readyCount} of {lib.packs.length} packs ready</p>
        {/if}
      </div>
    </div>
    <Button variant="outline" onclick={() => refresh(true)} disabled={scanning || serverDown}>
      <RefreshCwIcon data-icon="inline-start" class={scanning ? 'animate-spin' : ''} />
      {scanning ? 'Scanning…' : 'Rescan'}
    </Button>
  </div>
</header>

<main class="mx-auto max-w-6xl px-4 py-8 sm:px-6">
  {#if serverDown}
    <Alert.Root variant="destructive">
      <ServerOffIcon />
      <Alert.Title>The library program has stopped</Alert.Title>
      <Alert.Description>
        Run the launcher on the drive again (for example <code>start-windows.exe</code>). It will open a new browser window.
      </Alert.Description>
    </Alert.Root>
  {:else if loadError}
    <Alert.Root variant="destructive">
      <CircleAlertIcon />
      <Alert.Title>Couldn't load the library</Alert.Title>
      <Alert.Description>{loadError}</Alert.Description>
    </Alert.Root>
  {:else if !lib}
    <p class="text-muted-foreground text-sm">Loading…</p>
  {:else if !lib.available}
    <Alert.Root>
      <UnplugIcon />
      <Alert.Title>Library not available</Alert.Title>
      <Alert.Description>
        {#each lib.problems as problem}<p>{problem}</p>{/each}
        <p>Reconnect the drive, then press <strong>Rescan</strong>.</p>
        <p class="text-xs">Looking in <code class="break-all">{lib.root}</code></p>
      </Alert.Description>
    </Alert.Root>
  {:else}
    {#if lib.problems.length}
      <Alert.Root variant="destructive" class="mb-6">
        <CircleAlertIcon />
        <Alert.Title>This library has problems</Alert.Title>
        <Alert.Description>
          <ul class="list-disc pl-4">{#each lib.problems as problem}<li>{problem}</li>{/each}</ul>
        </Alert.Description>
      </Alert.Root>
    {/if}

    {#if lib.packs.length === 0}
      <p class="text-muted-foreground text-sm">This library doesn't list any packs yet.</p>
    {:else}
      <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {#each lib.packs as pack (pack.manifestPath)}
          <PackCard {pack} />
        {/each}
      </div>
    {/if}

    <p class="text-muted-foreground mt-10 text-xs">
      Library folder: <code class="break-all">{lib.root}</code>
    </p>
  {/if}
</main>
