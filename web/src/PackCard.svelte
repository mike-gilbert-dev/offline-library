<script lang="ts">
  import type { Component } from 'svelte';
  import * as Card from '$lib/components/ui/card/index.js';
  import { Badge, type BadgeVariant } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import type { Pack, PackStatus } from '$lib/types.js';

  import BookOpenIcon from '@lucide/svelte/icons/book-open';
  import BookTextIcon from '@lucide/svelte/icons/book-text';
  import MapIcon from '@lucide/svelte/icons/map';
  import HeartPulseIcon from '@lucide/svelte/icons/heart-pulse';
  import PackageIcon from '@lucide/svelte/icons/package';
  import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
  import CircleDashedIcon from '@lucide/svelte/icons/circle-dashed';
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
  import CircleXIcon from '@lucide/svelte/icons/circle-x';
  import ArrowUpRightIcon from '@lucide/svelte/icons/arrow-up-right';

  let { pack }: { pack: Pack } = $props();

  const m = $derived(pack.manifest);

  const categoryIcons: Record<string, Component> = {
    encyclopedia: BookOpenIcon,
    manuals: BookTextIcon,
    maps: MapIcon,
    medical: HeartPulseIcon,
  };
  const CategoryIcon = $derived(categoryIcons[m?.category ?? ''] ?? PackageIcon);

  const statuses: Record<PackStatus, { label: string; variant: BadgeVariant; icon: Component }> = {
    ok: { label: 'Ready', variant: 'secondary', icon: CircleCheckIcon },
    unsupported: { label: 'Not supported yet', variant: 'outline', icon: CircleDashedIcon },
    incomplete: { label: 'Incomplete', variant: 'destructive', icon: TriangleAlertIcon },
    invalid: { label: 'Damaged', variant: 'destructive', icon: CircleXIcon },
  };
  const status = $derived(statuses[pack.status]);

  // ZIM packs open in the in-app viewer; PDFs open in the browser's own viewer.
  const inApp = $derived(m?.format === 'zim' || m?.format === 'pmtiles');
  const contentUrl = $derived(
    !m
      ? ''
      : inApp
        ? `#/pack/${encodeURIComponent(m.packId)}/`
        : `/content/${encodeURIComponent(m.packId)}/${m.entrypoint.split('/').map(encodeURIComponent).join('/')}`,
  );

  const details = $derived(
    m
      ? [
          ['Format', m.format || '—'],
          ['Edition', m.version],
          ['Content date', m.contentDate],
          ['Language', m.languages?.join(', ')],
          ['Size', pack.totalBytes ? formatBytes(pack.totalBytes) : ''],
        ].filter(([, v]) => v)
      : [],
  );

  function formatBytes(n: number) {
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    while (n >= 1024 && i < units.length - 1) {
      n /= 1024;
      i++;
    }
    return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
  }
</script>

<Card.Root class="h-full">
  <Card.Header>
    <div class="text-muted-foreground mb-1 flex items-center gap-2 text-xs font-medium uppercase tracking-wide">
      <CategoryIcon class="size-3.5" />
      {m?.category || 'pack'}
    </div>
    <Card.Title class="text-base">{m?.title || pack.manifestPath}</Card.Title>
    {#if m?.description}
      <Card.Description>{m.description}</Card.Description>
    {/if}
    <Card.Action>
      <Badge variant={status.variant}>
        <status.icon data-icon="inline-start" />
        {status.label}
      </Badge>
    </Card.Action>
  </Card.Header>

  <Card.Content class="flex flex-col gap-4">
    {#if details.length}
      <dl class="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-3">
        {#each details as [label, value] (label)}
          <div>
            <dt class="text-muted-foreground text-xs">{label}</dt>
            <dd class="font-medium">{value}</dd>
          </div>
        {/each}
      </dl>
    {/if}

    {#if pack.problems.length}
      <ul class="text-destructive list-disc space-y-1 pl-4 text-xs">
        {#each pack.problems as problem}<li>{problem}</li>{/each}
      </ul>
    {/if}
  </Card.Content>

  <Card.Footer class="mt-auto flex items-end justify-between gap-4">
    <p class="text-muted-foreground text-xs">
      {#if m?.source?.name || m?.source?.license}
        {m.source.name || 'Unknown source'}{#if m.source.license}&nbsp;· {m.source.license}{/if}
        {#if m.source.attribution}<br />{m.source.attribution}{/if}
      {/if}
    </p>
    {#if pack.status === 'ok'}
      {#if inApp}
        <Button href={contentUrl} size="sm">Open</Button>
      {:else}
        <Button href={contentUrl} target="_blank" rel="noopener" size="sm">
          Open
          <ArrowUpRightIcon data-icon="inline-end" />
        </Button>
      {/if}
    {/if}
  </Card.Footer>
</Card.Root>
