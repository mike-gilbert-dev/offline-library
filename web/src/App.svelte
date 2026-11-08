<script lang="ts">
  import { onMount } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { badgeVariants } from '$lib/components/ui/badge/index.js';
  import type { Library, Pack } from '$lib/types.js';
  import { tagStore } from '$lib/tags.svelte.js';
  import PackCard from './PackCard.svelte';
  import Viewer from './Viewer.svelte';

  import LibraryIcon from '@lucide/svelte/icons/library-big';
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
  import UnplugIcon from '@lucide/svelte/icons/unplug';
  import ServerOffIcon from '@lucide/svelte/icons/server-off';
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
  import MailIcon from '@lucide/svelte/icons/mail';
  import SettingsIcon from '@lucide/svelte/icons/settings';
  import SearchIcon from '@lucide/svelte/icons/search';
  import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left';
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
  import XIcon from '@lucide/svelte/icons/x';
  import Settings from './Settings.svelte';

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
      if (lib.available) tagStore.load();
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

  // Hash routes: "#/" is the library, "#/pack/<id>/<article path>" a viewer.
  let hash = $state(location.hash);
  let contentOrigin = $state('');
  let appVersion = $state('');

  const route = $derived.by(() => {
    const m = /^#\/pack\/([^/]+)\/?(.*)$/.exec(hash);
    return m ? { packId: decodeURIComponent(m[1]), path: m[2] } : null;
  });
  const viewerPack = $derived(
    route && lib?.packs.find((p) => p.status === 'ok' && p.manifest?.packId === route.packId),
  );

  onMount(() => {
    refresh();
    api<{ contentOrigin: string; version: string }>('/api/config')
      .then((c) => {
        contentOrigin = c.contentOrigin;
        appVersion = c.version;
      })
      .catch(() => {});
    const id = setInterval(checkStatus, 3000);
    const onHash = () => {
      // The viewer rewrites the article path with replaceState (no event);
      // only react when switching between library and a different pack.
      const next = location.hash;
      const pack = (h: string) => /^#\/pack\/([^/]+)/.exec(h)?.[1] ?? '';
      if (pack(next) !== pack(hash) || !next.startsWith('#/pack/')) hash = next;
    };
    window.addEventListener('hashchange', onHash);
    return () => {
      clearInterval(id);
      window.removeEventListener('hashchange', onHash);
    };
  });

  const readyCount = $derived(lib?.packs.filter((p) => p.status === 'ok').length ?? 0);

  // ---- Search, tag filter and pages ----
  // Kept for the browser tab (sessionStorage) so a reload doesn't lose your
  // place; going into a pack and back keeps it anyway because App stays mounted.
  const UNTAGGED = '\u0000untagged'; // can't collide with a real tag
  const PAGE_SIZES = [12, 24, 48, 96];
  const saved = (() => {
    try {
      return JSON.parse(sessionStorage.getItem('olib-filter') ?? '{}');
    } catch {
      return {};
    }
  })();
  let query = $state<string>(typeof saved.query === 'string' ? saved.query : '');
  let activeTags = $state<string[]>(Array.isArray(saved.tags) ? saved.tags : []);
  let pageSize = $state<number>(PAGE_SIZES.includes(saved.pageSize) ? saved.pageSize : 24);
  let page = $state<number>(Number.isInteger(saved.page) ? saved.page : 1);

  $effect(() => {
    try {
      sessionStorage.setItem('olib-filter', JSON.stringify({ query, tags: activeTags, pageSize, page }));
    } catch {
      // private window or storage blocked: filters just won't survive a reload
    }
  });

  // Tags that no longer exist (renamed or deleted in Settings) stop filtering.
  $effect(() => {
    if (!tagStore.loaded) return;
    const known = new Set(tagStore.all.map((t) => t.tag));
    const kept = activeTags.filter((t) => t === UNTAGGED || known.has(t));
    if (kept.length !== activeTags.length) activeTags = kept;
  });

  function toggleTag(tag: string) {
    activeTags = activeTags.includes(tag) ? activeTags.filter((t) => t !== tag) : [...activeTags, tag];
    page = 1;
  }

  function clearFilters() {
    query = '';
    activeTags = [];
    page = 1;
  }

  function searchText(p: Pack): string {
    const m = p.manifest;
    if (!m) return p.manifestPath.toLowerCase();
    return [m.title, m.description, m.category, m.format, m.packId, m.source?.name, ...tagStore.of(m.packId)]
      .filter(Boolean)
      .join(' ')
      .toLowerCase();
  }

  // Every word typed must appear somewhere, and the pack must have every
  // selected tag, so each extra word or tag narrows the list.
  const filtered = $derived.by(() => {
    if (!lib) return [];
    const words = query.toLowerCase().split(/\s+/).filter(Boolean);
    return lib.packs.filter((p) => {
      const tags = tagStore.of(p.manifest?.packId);
      if (activeTags.some((t) => (t === UNTAGGED ? tags.length > 0 : !tags.includes(t)))) return false;
      if (!words.length) return true;
      const text = searchText(p);
      return words.every((w) => text.includes(w));
    });
  });

  const pageCount = $derived(Math.max(1, Math.ceil(filtered.length / pageSize)));
  const currentPage = $derived(Math.min(Math.max(1, page), pageCount));
  const pagePacks = $derived(filtered.slice((currentPage - 1) * pageSize, currentPage * pageSize));
  const filtering = $derived(query.trim() !== '' || activeTags.length > 0);
  const untaggedCount = $derived(lib?.packs.filter((p) => p.manifest && !tagStore.of(p.manifest.packId).length).length ?? 0);

  function goToPage(n: number) {
    page = Math.min(Math.max(1, n), pageCount);
    window.scrollTo({ top: 0 });
  }
</script>

{#if hash.startsWith('#/messages')}
  <!-- Messages is built in (not a pack). The radio libraries are ~500 KB, so
       they load only when Messages is opened. -->
  {#await import('./messages/Messages.svelte') then { default: Messages }}
    <Messages />
  {/await}
{:else if hash.startsWith('#/settings')}
  <Settings />
{:else if route && viewerPack && contentOrigin && !serverDown && lib?.available}
  {#key route.packId}
    {#if viewerPack.manifest?.format === 'pmtiles'}
      <!-- MapLibre is ~1 MB, so it is only loaded when a map is opened. -->
      {#await import('./MapViewer.svelte') then { default: MapViewer }}
        <MapViewer pack={viewerPack} />
      {/await}
    {:else}
      <Viewer pack={viewerPack} {contentOrigin} initialPath={route.path} />
    {/if}
  {/key}
{:else}

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
    <div class="flex items-center gap-2">
      <Button variant="outline" href="#/messages">
        <MailIcon data-icon="inline-start" /> Messages
      </Button>
      <Button variant="outline" onclick={() => refresh(true)} disabled={scanning || serverDown}>
        <RefreshCwIcon data-icon="inline-start" class={scanning ? 'animate-spin' : ''} />
        {scanning ? 'Scanning…' : 'Rescan'}
      </Button>
      <Button variant="outline" size="icon" href="#/settings" aria-label="Settings" title="Settings">
        <SettingsIcon />
      </Button>
    </div>
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
      <div class="mb-6 flex flex-col gap-3">
        <div class="relative">
          <SearchIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
          <Input
            type="search"
            bind:value={query}
            oninput={() => (page = 1)}
            placeholder="Search titles, descriptions and tags"
            aria-label="Search packs"
            class="pl-8"
          />
        </div>

        {#if tagStore.error}
          <p class="text-destructive text-sm">{tagStore.error}</p>
        {/if}

        {#if tagStore.all.length}
          <div class="flex flex-wrap items-center gap-1.5" role="group" aria-label="Filter by tag">
            <span class="text-muted-foreground mr-1 text-xs">Tags:</span>
            {#each tagStore.all as { tag, count } (tag)}
              <button
                type="button"
                class={badgeVariants({ variant: activeTags.includes(tag) ? 'default' : 'outline' })}
                aria-pressed={activeTags.includes(tag)}
                onclick={() => toggleTag(tag)}
              >
                {tag} <span class="opacity-60">{count}</span>
              </button>
            {/each}
            {#if untaggedCount}
              <button
                type="button"
                class="{badgeVariants({ variant: activeTags.includes(UNTAGGED) ? 'default' : 'outline' })} italic"
                aria-pressed={activeTags.includes(UNTAGGED)}
                onclick={() => toggleTag(UNTAGGED)}
              >
                untagged <span class="opacity-60">{untaggedCount}</span>
              </button>
            {/if}
          </div>
        {/if}

        <div class="text-muted-foreground flex flex-wrap items-center justify-between gap-2 text-sm">
          <span>
            {#if filtered.length === 0}
              No packs match
            {:else}
              Showing {(currentPage - 1) * pageSize + 1}–{(currentPage - 1) * pageSize + pagePacks.length}
              of {filtered.length}{filtering ? ` matching (${lib.packs.length} total)` : ''}
            {/if}
            {#if filtering}
              <Button variant="link" size="sm" class="h-auto px-1" onclick={clearFilters}>
                <XIcon data-icon="inline-start" /> Clear filters
              </Button>
            {/if}
          </span>
          <label class="flex items-center gap-2">
            Per page
            <select
              class="border-input dark:bg-input/30 h-8 rounded-md border bg-transparent px-2 text-sm"
              bind:value={pageSize}
              onchange={() => (page = 1)}
            >
              {#each PAGE_SIZES as n (n)}<option value={n}>{n}</option>{/each}
            </select>
          </label>
        </div>
      </div>

      <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {#each pagePacks as pack (pack.manifestPath)}
          <PackCard {pack} activeTags={activeTags} onTagClick={toggleTag} />
        {/each}
      </div>

      {#if pageCount > 1}
        <nav class="mt-6 flex items-center justify-center gap-3" aria-label="Pages">
          <Button variant="outline" size="sm" onclick={() => goToPage(currentPage - 1)} disabled={currentPage === 1}>
            <ChevronLeftIcon data-icon="inline-start" /> Previous
          </Button>
          <span class="text-muted-foreground text-sm">Page {currentPage} of {pageCount}</span>
          <Button variant="outline" size="sm" onclick={() => goToPage(currentPage + 1)} disabled={currentPage === pageCount}>
            Next <ChevronRightIcon data-icon="inline-end" />
          </Button>
        </nav>
      {/if}
    {/if}

    <p class="text-muted-foreground mt-10 text-xs">
      Library folder: <code class="break-all">{lib.root}</code>
      {#if appVersion}<span class="ml-2">· Offline Library {appVersion}</span>{/if}
    </p>
  {/if}
</main>
{/if}
