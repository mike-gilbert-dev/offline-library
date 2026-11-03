<script lang="ts">
  import { onMount } from 'svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import type { Pack } from '$lib/types.js';

  import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
  import HouseIcon from '@lucide/svelte/icons/house';
  import SearchIcon from '@lucide/svelte/icons/search';
  import GlobeIcon from '@lucide/svelte/icons/globe';

  let {
    pack,
    contentOrigin,
    initialPath = '',
  }: { pack: Pack; contentOrigin: string; initialPath?: string } = $props();

  const packId = $derived(pack.manifest!.packId);
  const base = $derived(`${contentOrigin}/zim/${encodeURIComponent(packId)}/`);

  // Only set when *we* navigate (search, home); in-page link clicks are
  // reported back by the bridge script and just update the address bar.
  let frameSrc = $state('');
  let frame = $state<HTMLIFrameElement>();
  let pageTitle = $state('');

  let query = $state('');
  let results = $state<{ title: string; path: string }[]>([]);
  let active = $state(0);
  let showResults = $state(false);
  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  let searchSeq = 0;

  let externalLink = $state<{ url: string; text: string } | null>(null);

  onMount(() => {
    frameSrc = base + initialPath;
    const onMessage = (ev: MessageEvent) => {
      if (ev.origin !== contentOrigin || ev.source !== frame?.contentWindow) return;
      const msg = ev.data ?? {};
      if (msg.type === 'olib:navigated' && typeof msg.path === 'string') {
        pageTitle = typeof msg.title === 'string' ? msg.title : '';
        const prefix = `/zim/${encodeURIComponent(packId)}/`;
        const rest = msg.path.startsWith(prefix) ? msg.path.slice(prefix.length) : '';
        history.replaceState(null, '', `#/pack/${encodeURIComponent(packId)}/${rest}`);
      } else if (msg.type === 'olib:external-link' && /^https?:\/\//.test(msg.url)) {
        externalLink = { url: msg.url, text: String(msg.text ?? '') };
      }
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  });

  function go(path: string) {
    // Assign on the element directly: it reloads even if the URL is unchanged
    // (e.g. Home after browsing away from the main page via in-page links).
    if (frame) frame.src = base + path;
    else frameSrc = base + path;
    showResults = false;
    query = '';
    results = [];
  }

  function onInput() {
    clearTimeout(searchTimer);
    const q = query.trim();
    if (!q) {
      results = [];
      return;
    }
    searchTimer = setTimeout(async () => {
      const seq = ++searchSeq;
      try {
        const res = await fetch(`/api/packs/${encodeURIComponent(packId)}/search?q=${encodeURIComponent(q)}&limit=12`);
        if (!res.ok || seq !== searchSeq) return;
        results = await res.json();
        active = 0;
        showResults = true;
      } catch {
        /* server gone: the library page will say so */
      }
    }, 120);
  }

  function onKeydown(ev: KeyboardEvent) {
    if (ev.key === 'ArrowDown') {
      active = Math.min(active + 1, results.length - 1);
      ev.preventDefault();
    } else if (ev.key === 'ArrowUp') {
      active = Math.max(active - 1, 0);
      ev.preventDefault();
    } else if (ev.key === 'Enter' && results[active]) {
      go(results[active].path);
    } else if (ev.key === 'Escape') {
      showResults = false;
    }
  }

  function openExternal() {
    if (externalLink) window.open(externalLink.url, '_blank', 'noopener,noreferrer');
    externalLink = null;
  }
</script>

<div class="flex h-dvh flex-col">
  <header class="bg-background flex items-center gap-2 border-b px-3 py-2 sm:gap-3 sm:px-4">
    <Button variant="ghost" size="sm" href="#/">
      <ArrowLeftIcon data-icon="inline-start" />
      <span class="hidden sm:inline">Library</span>
    </Button>
    <Button variant="ghost" size="icon-sm" onclick={() => go('')} title="Main page" aria-label="Main page">
      <HouseIcon />
    </Button>

    <div class="min-w-0 flex-1">
      <p class="truncate text-sm font-medium leading-tight">{pageTitle || pack.manifest?.title}</p>
      <p class="text-muted-foreground truncate text-xs">{pack.manifest?.title}</p>
    </div>

    <div class="relative w-40 sm:w-72">
      <SearchIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
      <Input
        class="pl-8"
        placeholder="Search titles…"
        aria-label="Search article titles"
        bind:value={query}
        oninput={onInput}
        onkeydown={onKeydown}
        onfocus={() => (showResults = results.length > 0)}
        onblur={() => setTimeout(() => (showResults = false), 150)}
      />
      {#if showResults}
        <div class="bg-popover text-popover-foreground absolute right-0 z-20 mt-1 max-h-96 w-72 overflow-auto rounded-lg border p-1 shadow-md sm:w-full">
          {#if results.length === 0}
            <p class="text-muted-foreground px-2 py-1.5 text-sm">No matching titles</p>
          {:else}
            {#each results as r, i (r.path)}
              <button
                type="button"
                class="w-full truncate rounded-md px-2 py-1.5 text-left text-sm {i === active ? 'bg-accent text-accent-foreground' : ''}"
                onmouseenter={() => (active = i)}
                onclick={() => go(r.path)}
              >
                {r.title}
              </button>
            {/each}
          {/if}
        </div>
      {/if}
    </div>
  </header>

  {#if externalLink}
    <div class="border-b p-3">
      <Alert.Root>
        <GlobeIcon />
        <Alert.Title>This link goes to the internet</Alert.Title>
        <Alert.Description>
          <p class="break-all">{externalLink.url}</p>
          <div class="mt-2 flex gap-2">
            <Button size="sm" onclick={openExternal}>Open in browser</Button>
            <Button size="sm" variant="outline" onclick={() => (externalLink = null)}>Stay offline</Button>
          </div>
        </Alert.Description>
      </Alert.Root>
    </div>
  {/if}

  {#if frameSrc}
    <iframe
      bind:this={frame}
      src={frameSrc}
      title={pageTitle || 'Article'}
      class="w-full flex-1 border-0 bg-white"
      sandbox="allow-scripts allow-same-origin"
    ></iframe>
  {/if}
</div>
