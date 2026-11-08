<script lang="ts">
  import { onMount } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import TagManager from './TagManager.svelte';

  import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
  import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
  import DownloadIcon from '@lucide/svelte/icons/download';
  import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';

  interface UpdateCheck {
    current: string;
    latest: string;
    updateAvailable: boolean;
    devBuild: boolean;
    notes: string;
    url: string;
    publishedAt: string;
  }

  interface InstallStatus {
    state: 'idle' | 'downloading' | 'installing' | 'restarting' | 'error';
    version?: string;
    done: number;
    total: number;
    error?: string;
  }

  let version = $state('');
  let checking = $state(false);
  let result = $state<UpdateCheck | null>(null);
  let error = $state('');
  let install = $state<InstallStatus | null>(null);
  const installing = $derived(install !== null && install.state !== 'error');

  async function startInstall() {
    if (!result) return;
    install = { state: 'downloading', version: result.latest, done: 0, total: 0 };
    try {
      const res = await fetch('/api/update/install', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ version: result.latest }),
      });
      if (!res.ok) throw new Error((await res.text()).trim() || `${res.status} ${res.statusText}`);
    } catch (e) {
      install = { state: 'error', done: 0, total: 0, error: e instanceof Error ? e.message : String(e) };
      return;
    }
    // Follow progress until the launcher restarts (then this server goes away).
    while (install && (install.state === 'downloading' || install.state === 'installing')) {
      await new Promise((r) => setTimeout(r, 500));
      try {
        install = await (await fetch('/api/update/status')).json();
      } catch {
        break; // server already restarting
      }
    }
  }

  const mb = (n: number) => (n / 1048576).toFixed(1);

  onMount(() => {
    fetch('/api/config')
      .then((r) => r.json())
      .then((c: { version: string }) => (version = c.version))
      .catch(() => {});
  });

  async function check() {
    checking = true;
    error = '';
    result = null;
    try {
      const res = await fetch('/api/update/check');
      const body = await res.json();
      if (!res.ok) throw new Error(body.error ?? `${res.status} ${res.statusText}`);
      result = body;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      checking = false;
    }
  }

  // Minimal Markdown for release notes: "## " headings, "- " lists,
  // paragraphs, **bold** and `code`. Rendered as elements, never as HTML.
  type Block = { kind: 'h' | 'li' | 'p'; text: string };
  function blocks(md: string): Block[] {
    const out: Block[] = [];
    for (const raw of md.replace(/\r/g, '').split('\n')) {
      const line = raw.trim();
      if (!line) continue;
      if (/^#{1,6}\s/.test(line)) out.push({ kind: 'h', text: line.replace(/^#+\s*/, '') });
      else if (/^[-*]\s/.test(line)) out.push({ kind: 'li', text: line.slice(2) });
      else out.push({ kind: 'p', text: line });
    }
    return out;
  }
  function inline(text: string) {
    return text
      .split(/(\*\*[^*]+\*\*|`[^`]+`)/)
      .filter(Boolean)
      .map((part) =>
        part.startsWith('**') && part.endsWith('**')
          ? { kind: 'b' as const, text: part.slice(2, -2).replace(/`/g, '') }
          : part.startsWith('`') && part.endsWith('`')
            ? { kind: 'code' as const, text: part.slice(1, -1) }
            : { kind: 't' as const, text: part },
      );
  }

  const published = (iso: string) =>
    new Date(iso).toLocaleDateString([], { year: 'numeric', month: 'long', day: 'numeric' });
</script>

<header class="bg-background/80 sticky top-0 z-10 border-b backdrop-blur">
  <div class="mx-auto flex max-w-3xl items-center gap-3 px-4 py-3 sm:px-6">
    <Button variant="ghost" size="icon" href="#/" aria-label="Back to library">
      <ArrowLeftIcon />
    </Button>
    <h1 class="text-lg font-semibold tracking-tight">Settings</h1>
  </div>
</header>

<main class="mx-auto max-w-3xl space-y-6 px-4 py-8 sm:px-6">
  <TagManager />

  <Card.Root>
    <Card.Header>
      <Card.Title>Updates</Card.Title>
      <Card.Description>
        Updates replace only the library program on this drive. Your packs and message history are never changed.
      </Card.Description>
    </Card.Header>
    <Card.Content class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p class="text-muted-foreground text-sm">Installed version</p>
          <p class="font-medium">
            {version === 'dev' ? 'Development build' : version || '…'}
          </p>
        </div>
        <Button variant="outline" onclick={check} disabled={checking || installing}>
          <RefreshCwIcon data-icon="inline-start" class={checking ? 'animate-spin' : ''} />
          {checking ? 'Checking…' : 'Check for updates'}
        </Button>
      </div>
      <p class="text-muted-foreground text-xs">
        Checking needs an internet connection. The library never checks on its own.
      </p>

      {#if error}
        <Alert.Root variant="destructive">
          <CircleAlertIcon />
          <Alert.Title>Couldn't check for updates</Alert.Title>
          <Alert.Description>{error}</Alert.Description>
        </Alert.Root>
      {/if}

      {#if result}
        {#if result.updateAvailable}
          <Alert.Root>
            <DownloadIcon />
            <Alert.Title>Version {result.latest} is available</Alert.Title>
            <Alert.Description>
              <p>Released {published(result.publishedAt)}.</p>
              {#if !install}
                <Button class="mt-3" onclick={startInstall}>
                  <DownloadIcon data-icon="inline-start" /> Install {result.latest}
                </Button>
              {/if}
            </Alert.Description>
          </Alert.Root>

          {#if install}
            {#if install.state === 'error'}
              <Alert.Root variant="destructive">
                <CircleAlertIcon />
                <Alert.Title>The update wasn't installed</Alert.Title>
                <Alert.Description>
                  <p>{install.error}</p>
                  <p class="mt-1">Nothing was changed; the current version keeps working.</p>
                  <Button variant="outline" class="mt-3" onclick={startInstall}>Try again</Button>
                </Alert.Description>
              </Alert.Root>
            {:else if install.state === 'restarting'}
              <Alert.Root>
                <RefreshCwIcon class="animate-spin" />
                <Alert.Title>Installed. Restarting into {install.version}…</Alert.Title>
                <Alert.Description>
                  The library will open in a new browser tab in a few seconds. You can close this tab.
                </Alert.Description>
              </Alert.Root>
            {:else}
              <div class="space-y-2 rounded-lg border p-4" role="status" aria-live="polite">
                <div class="flex justify-between text-sm">
                  <span class="font-medium">
                    {install.state === 'installing' ? 'Installing…' : `Downloading ${install.version ?? ''}…`}
                  </span>
                  {#if install.total}
                    <span class="text-muted-foreground tabular-nums">{mb(install.done)} / {mb(install.total)} MB</span>
                  {/if}
                </div>
                <div class="bg-muted h-2 overflow-hidden rounded-full">
                  <div
                    class="bg-primary h-full transition-[width] duration-300"
                    style:width={install.total ? `${Math.min(100, (install.done / install.total) * 100)}%` : '5%'}
                  ></div>
                </div>
                <p class="text-muted-foreground text-xs">
                  Downloading the program for Windows, Mac and Linux so the drive works everywhere. Keep this window open.
                </p>
              </div>
            {/if}
          {/if}
        {:else if result.devBuild}
          <Alert.Root>
            <CircleAlertIcon />
            <Alert.Title>This is a development build</Alert.Title>
            <Alert.Description>The latest release is {result.latest}. Development builds aren't updated.</Alert.Description>
          </Alert.Root>
        {:else}
          <Alert.Root>
            <CircleCheckIcon />
            <Alert.Title>You're up to date</Alert.Title>
            <Alert.Description>{result.current} is the latest version.</Alert.Description>
          </Alert.Root>
        {/if}

        <div class="rounded-lg border p-4">
          <div class="mb-2 flex flex-wrap items-center gap-2">
            <span class="font-medium">What's in {result.latest}</span>
            {#if result.updateAvailable}<Badge>New</Badge>{/if}
            <a
              class="text-muted-foreground hover:text-foreground ml-auto inline-flex items-center gap-1 text-xs underline-offset-4 hover:underline"
              href={result.url}
              target="_blank"
              rel="noopener noreferrer"
            >
              Release page <ExternalLinkIcon class="size-3" />
            </a>
          </div>
          <div class="text-muted-foreground space-y-2 text-sm">
            {#each blocks(result.notes) as block, i (i)}
              {#if block.kind === 'h'}
                <p class="text-foreground pt-1 font-medium">{@render rich(block.text)}</p>
              {:else if block.kind === 'li'}
                <p class="flex gap-2 pl-1"><span aria-hidden="true">•</span><span>{@render rich(block.text)}</span></p>
              {:else}
                <p>{@render rich(block.text)}</p>
              {/if}
            {:else}
              <p>No release notes.</p>
            {/each}
          </div>
        </div>
      {/if}
    </Card.Content>
  </Card.Root>
</main>

{#snippet rich(text: string)}
  {#each inline(text) as part, i (i)}
    {#if part.kind === 'b'}<strong class="text-foreground font-medium">{part.text}</strong>
    {:else if part.kind === 'code'}<code class="bg-muted rounded px-1 py-0.5 text-xs">{part.text}</code>
    {:else}{part.text}{/if}
  {/each}
{/snippet}
