<script lang="ts">
  import { tick } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { cn } from '$lib/utils.js';
  import { BROADCAST, mesh, type MeshMessage, type MeshNode } from './mesh.svelte.js';

  import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
  import CheckIcon from '@lucide/svelte/icons/check';
  import CheckCheckIcon from '@lucide/svelte/icons/check-check';
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
  import ClockIcon from '@lucide/svelte/icons/clock';
  import HashIcon from '@lucide/svelte/icons/hash';
  import MessagesSquareIcon from '@lucide/svelte/icons/messages-square';
  import RadioIcon from '@lucide/svelte/icons/radio';
  import SendIcon from '@lucide/svelte/icons/send';
  import UnplugIcon from '@lucide/svelte/icons/unplug';
  import UsbIcon from '@lucide/svelte/icons/usb';

  // Meshtastic text payloads top out a little above 200 bytes.
  const MAX_BYTES = 200;
  const PUBLIC = 'ch:0';

  // A conversation is either a channel ("ch:<index>") or a direct thread
  // with one other radio ("dm:<node number>").
  const keyOf = (m: MeshMessage) =>
    m.to === BROADCAST ? `ch:${m.channel}` : `dm:${m.outgoing ? m.to : m.from}`;

  let selected = $state(PUBLIC);
  let draft = $state('');
  let threadEl = $state<HTMLElement | null>(null);

  // Radios you've messaged come first (most recent conversation on top),
  // then everyone else by when they were last heard.
  const lastMessageAt = $derived.by(() => {
    const at: Record<string, number> = {};
    for (const m of mesh.messages) at[keyOf(m)] = Math.max(at[keyOf(m)] ?? 0, m.time.getTime());
    return at;
  });
  const others = $derived(
    Object.values(mesh.nodes)
      .filter((n) => n.num !== mesh.myNodeNum)
      .sort(
        (a, b) =>
          (lastMessageAt[`dm:${b.num}`] ?? 0) - (lastMessageAt[`dm:${a.num}`] ?? 0) ||
          (b.lastHeard?.getTime() ?? 0) - (a.lastHeard?.getTime() ?? 0),
      ),
  );
  const thread = $derived(mesh.messages.filter((m) => keyOf(m) === selected));
  const selectedNode = $derived(
    selected.startsWith('dm:') ? mesh.nodes[Number(selected.slice(3))] : undefined,
  );
  const draftBytes = $derived(new TextEncoder().encode(draft).length);

  const unread = (key: string) => mesh.messages.filter((m) => !m.read && keyOf(m) === key).length;

  // Mark the open conversation read, and keep the newest message in view.
  $effect(() => {
    for (const m of thread) if (!m.read) m.read = true;
    tick().then(() => threadEl?.scrollTo({ top: threadEl.scrollHeight }));
  });

  async function send(e: SubmitEvent) {
    e.preventDefault();
    const text = draft.trim();
    if (!text || !selectedNode || draftBytes > MAX_BYTES) return;
    draft = '';
    await mesh.sendDirect(selectedNode.num, text);
  }

  const name = (num: number) => mesh.nodes[num]?.longName ?? `!${(num >>> 0).toString(16)}`;
  const timeText = (d: Date) =>
    d.toDateString() === new Date().toDateString()
      ? d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })
      : d.toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });

  function heard(n: MeshNode) {
    if (!n.lastHeard) return 'Not heard yet';
    const mins = Math.round((Date.now() - n.lastHeard.getTime()) / 60000);
    if (mins < 1) return 'Heard just now';
    if (mins < 60) return `Heard ${mins} min ago`;
    if (mins < 60 * 24) return `Heard ${Math.round(mins / 60)} h ago`;
    return `Heard ${Math.round(mins / 1440)} d ago`;
  }

  const stateLabel = {
    disconnected: 'Not connected',
    connecting: 'Connecting…',
    configuring: 'Loading radio…',
    ready: 'Connected',
  } as const;
</script>

<div class="flex h-dvh flex-col">
  <header class="bg-background/80 border-b backdrop-blur">
    <div class="mx-auto flex max-w-6xl items-center gap-3 px-4 py-3 sm:px-6">
      <Button variant="ghost" size="icon" href="#/" aria-label="Back to library">
        <ArrowLeftIcon />
      </Button>
      <div class="min-w-0 flex-1">
        <h1 class="text-lg font-semibold leading-tight tracking-tight">Messages</h1>
        <p class="text-muted-foreground truncate text-sm">
          {#if mesh.state === 'ready' && mesh.me}
            {mesh.me.longName} · {mesh.me.id}{mesh.firmware ? ` · firmware ${mesh.firmware.split('.').slice(0, 3).join('.')}` : ''}
          {:else}
            {stateLabel[mesh.state]}
          {/if}
        </p>
      </div>
      {#if mesh.supported}
        {#if mesh.state === 'disconnected'}
          <Button onclick={() => mesh.connect()}>
            <UsbIcon data-icon="inline-start" /> Connect radio
          </Button>
        {:else}
          <Badge variant={mesh.state === 'ready' ? 'default' : 'secondary'} class="hidden sm:inline-flex">
            {stateLabel[mesh.state]}
          </Badge>
          <Button variant="outline" onclick={() => mesh.disconnect()}>
            <UnplugIcon data-icon="inline-start" /> Disconnect
          </Button>
        {/if}
      {/if}
    </div>
  </header>

  {#if !mesh.supported}
    <main class="mx-auto w-full max-w-2xl px-4 py-10">
      <Alert.Root>
        <CircleAlertIcon />
        <Alert.Title>Messages needs Chrome, Edge or Brave</Alert.Title>
        <Alert.Description>
          This browser can't talk to USB radios. Open the library in Google Chrome, Microsoft Edge or Brave to send and
          receive Meshtastic messages. The rest of the library works in any browser.
        </Alert.Description>
      </Alert.Root>
    </main>
  {:else if mesh.state === 'disconnected'}
    <main class="mx-auto w-full max-w-2xl px-4 py-10">
      {#if mesh.error}
        <Alert.Root variant="destructive" class="mb-6">
          <CircleAlertIcon />
          <Alert.Title>Couldn't stay connected to the radio</Alert.Title>
          <Alert.Description>{mesh.error}</Alert.Description>
        </Alert.Root>
      {/if}
      <div class="flex flex-col items-center gap-4 rounded-xl border border-dashed px-6 py-14 text-center">
        <div class="bg-muted flex size-12 items-center justify-center rounded-full">
          <RadioIcon class="size-6" />
        </div>
        <div>
          <h2 class="font-semibold">Connect a Meshtastic radio</h2>
          <p class="text-muted-foreground mt-1 max-w-sm text-sm">
            Plug the radio into this computer with a USB data cable, then choose it from the list.
          </p>
        </div>
        <Button onclick={() => mesh.connect()}>
          <UsbIcon data-icon="inline-start" /> Connect radio
        </Button>
      </div>
    </main>
  {:else if mesh.state !== 'ready'}
    <main class="text-muted-foreground mx-auto w-full max-w-2xl px-4 py-10 text-sm">
      {stateLabel[mesh.state]} Reading channels and the list of nearby radios.
    </main>
  {:else}
    <main class="mx-auto grid min-h-0 w-full max-w-6xl flex-1 grid-cols-1 md:grid-cols-[18rem_1fr] md:gap-0">
      <!-- Conversations -->
      <nav
        class={cn('min-h-0 overflow-y-auto border-r p-2', selected !== PUBLIC || selectedNode ? 'hidden md:block' : '')}
        aria-label="Conversations"
      >
        <p class="text-muted-foreground px-2 pt-2 pb-1 text-xs font-medium">Channels</p>
        {@render convo(PUBLIC, 'Public channel', 'Everyone in range', null)}
        <p class="text-muted-foreground px-2 pt-4 pb-1 text-xs font-medium">Direct messages</p>
        {#each others as n (n.num)}
          {@render convo(`dm:${n.num}`, n.longName, heard(n), n)}
        {:else}
          <p class="text-muted-foreground px-2 py-2 text-sm">No other radios heard yet.</p>
        {/each}
      </nav>

      <!-- Thread -->
      <section class="flex min-h-0 flex-col">
        {#if mesh.historyProblem}
          <p class="bg-muted text-muted-foreground flex items-center gap-2 border-b px-4 py-2 text-xs">
            <CircleAlertIcon class="size-3.5 shrink-0" /> {mesh.historyProblem}
          </p>
        {/if}
        <div class="flex items-center gap-2 border-b px-4 py-3">
          <Button variant="ghost" size="icon" class="md:hidden" onclick={() => (selected = PUBLIC)} aria-label="All conversations">
            <ArrowLeftIcon />
          </Button>
          <div class="min-w-0">
            <h2 class="truncate font-medium">{selectedNode?.longName ?? 'Public channel'}</h2>
            <p class="text-muted-foreground truncate text-xs">
              {#if selectedNode}
                {selectedNode.id}{selectedNode.hopsAway !== undefined ? ` · ${selectedNode.hopsAway === 0 ? 'direct' : `${selectedNode.hopsAway} hop${selectedNode.hopsAway > 1 ? 's' : ''}`}` : ''}{selectedNode.snr !== undefined ? ` · SNR ${selectedNode.snr.toFixed(1)} dB` : ''}
              {:else}
                Messages everyone in range can read
              {/if}
            </p>
          </div>
        </div>

        <div bind:this={threadEl} class="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 py-4">
          {#each thread as m (m.from + ':' + m.id)}
            <div class={cn('flex flex-col', m.outgoing ? 'items-end' : 'items-start')}>
              {#if !m.outgoing && !selectedNode}
                <span class="text-muted-foreground mb-0.5 px-1 text-xs">{name(m.from)}</span>
              {/if}
              <div
                class={cn(
                  'max-w-[80%] rounded-2xl px-3.5 py-2 text-sm wrap-break-word whitespace-pre-wrap',
                  m.outgoing ? 'bg-primary text-primary-foreground rounded-br-md' : 'bg-muted rounded-bl-md',
                )}
              >
                {m.text}
              </div>
              <span class="text-muted-foreground mt-1 flex items-center gap-1 px-1 text-xs">
                {timeText(m.time)}
                {#if m.status === 'sending'}
                  · <ClockIcon class="size-3" /> Sending
                {:else if m.status === 'relayed'}
                  · <CheckIcon class="size-3" /> Relayed by another radio
                {:else if m.status === 'delivered'}
                  · <CheckCheckIcon class="size-3" /> Delivered
                {:else if m.status === 'unconfirmed'}
                  · <CheckIcon class="size-3" /> Sent, no confirmation
                {:else if m.status === 'failed'}
                  · <span class="text-destructive flex items-center gap-1"><CircleAlertIcon class="size-3" /> {m.error ?? 'Not delivered'}</span>
                {/if}
              </span>
            </div>
          {:else}
            <div class="text-muted-foreground flex h-full flex-col items-center justify-center gap-2 text-sm">
              <MessagesSquareIcon class="size-6" />
              {selectedNode ? `No messages with ${selectedNode.longName} yet.` : 'No public messages since connecting.'}
            </div>
          {/each}
        </div>

        {#if selectedNode}
          <form class="flex items-center gap-2 border-t p-3" onsubmit={send}>
            <Input bind:value={draft} placeholder={`Message ${selectedNode.longName}`} autocomplete="off" aria-label="Message" />
            {#if draftBytes > MAX_BYTES - 40}
              <span class={cn('text-xs tabular-nums', draftBytes > MAX_BYTES ? 'text-destructive' : 'text-muted-foreground')}>
                {MAX_BYTES - draftBytes}
              </span>
            {/if}
            <Button type="submit" size="icon" disabled={!draft.trim() || draftBytes > MAX_BYTES} aria-label="Send">
              <SendIcon />
            </Button>
          </form>
        {:else}
          <p class="text-muted-foreground border-t px-4 py-3 text-xs">
            Sending to the public channel is turned off while Messages is being tested. Pick a radio under Direct
            messages to send a private message.
          </p>
        {/if}
      </section>
    </main>
  {/if}
</div>

{#snippet convo(key: string, title: string, subtitle: string, node: MeshNode | null)}
  {@const count = unread(key)}
  <button
    type="button"
    class={cn(
      'hover:bg-muted flex w-full items-center gap-3 rounded-lg px-2 py-2 text-left',
      selected === key && 'bg-muted',
    )}
    onclick={() => (selected = key)}
  >
    <span
      class="bg-secondary text-secondary-foreground flex size-9 shrink-0 items-center justify-center rounded-full text-xs font-semibold"
    >
      {#if node}{node.shortName.slice(0, 4)}{:else}<HashIcon class="size-4" />{/if}
    </span>
    <span class="min-w-0 flex-1">
      <span class="block truncate text-sm font-medium">{title}</span>
      <span class="text-muted-foreground block truncate text-xs">{subtitle}</span>
    </span>
    {#if count}<Badge class="rounded-full px-1.5">{count}</Badge>{/if}
  </button>
{/snippet}
