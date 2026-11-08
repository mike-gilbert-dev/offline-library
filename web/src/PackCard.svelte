<script lang="ts">
  import type { Component } from 'svelte';
  import * as Card from '$lib/components/ui/card/index.js';
  import { Badge, badgeVariants, type BadgeVariant } from '$lib/components/ui/badge/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { normalizeTag, tagStore } from '$lib/tags.svelte.js';
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
  import TagIcon from '@lucide/svelte/icons/tag';
  import XIcon from '@lucide/svelte/icons/x';

  let {
    pack,
    activeTags = [],
    onTagClick,
  }: { pack: Pack; activeTags?: string[]; onTagClick?: (tag: string) => void } = $props();

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

  // ---- Tags ----
  const tags = $derived(tagStore.of(m?.packId));
  let editing = $state(false);
  let draft = $state('');
  let saving = $state(false);
  let tagInput = $state<HTMLInputElement | null>(null);
  const datalistId = $derived(`tag-suggestions-${m?.packId}`);
  const suggestions = $derived(tagStore.all.map((t) => t.tag).filter((t) => !tags.includes(t)));

  async function saveTags(next: string[]) {
    if (!m) return;
    saving = true;
    await tagStore.setPackTags(m.packId, next);
    saving = false;
  }

  async function addDraft() {
    // Commas separate several tags typed at once: "hunting, recipes".
    const added = draft.split(',').map(normalizeTag).filter((t) => t && !tags.includes(t));
    draft = '';
    if (added.length) await saveTags([...tags, ...added]);
    tagInput?.focus();
  }

  function onTagKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      addDraft();
    } else if (e.key === 'Escape') {
      draft = '';
      editing = false;
    } else if (e.key === 'Backspace' && draft === '' && tags.length) {
      saveTags(tags.slice(0, -1));
    }
  }

  function startEditing() {
    editing = true;
    queueMicrotask(() => tagInput?.focus());
  }

  async function finishEditing() {
    if (draft.trim()) await addDraft();
    editing = false;
  }

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

    {#if m && editing}
      <div class="flex flex-col gap-2">
        <div class="flex flex-wrap gap-1.5">
          {#each tags as tag (tag)}
            <Badge variant="secondary" class="pr-1">
              {tag}
              <button
                type="button"
                class="hover:bg-foreground/10 rounded-full p-0.5"
                aria-label="Remove tag {tag}"
                disabled={saving}
                onclick={() => saveTags(tags.filter((t) => t !== tag))}
              >
                <XIcon class="size-3" />
              </button>
            </Badge>
          {:else}
            <span class="text-muted-foreground text-xs">No tags yet.</span>
          {/each}
        </div>
        <div class="flex gap-2">
          <Input
            bind:ref={tagInput}
            bind:value={draft}
            onkeydown={onTagKey}
            list={datalistId}
            maxlength={200}
            placeholder="Add a tag, then press Enter"
            aria-label="Add a tag to {m.title}"
            class="h-8"
          />
          <datalist id={datalistId}>
            {#each suggestions as s (s)}<option value={s}></option>{/each}
          </datalist>
          <Button size="sm" variant="outline" onclick={finishEditing} disabled={saving}>Done</Button>
        </div>
      </div>
    {:else if m}
      <div class="flex flex-wrap items-center gap-1.5">
        {#each tags as tag (tag)}
          <button
            type="button"
            class={badgeVariants({ variant: activeTags.includes(tag) ? 'default' : 'outline' })}
            title={activeTags.includes(tag) ? `Stop filtering by "${tag}"` : `Show packs tagged "${tag}"`}
            aria-pressed={activeTags.includes(tag)}
            onclick={() => onTagClick?.(tag)}
          >
            {tag}
          </button>
        {/each}
        <button
          type="button"
          class="{badgeVariants({ variant: 'ghost' })} text-muted-foreground"
          onclick={startEditing}
          disabled={!tagStore.loaded}
        >
          <TagIcon data-icon="inline-start" />
          {tags.length ? 'Edit tags' : 'Add tags'}
        </button>
      </div>
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
