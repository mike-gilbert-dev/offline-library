<script lang="ts">
  import { onMount } from 'svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { MAX_TAG_LENGTH, normalizeTag, tagStore } from '$lib/tags.svelte.js';

  import PencilIcon from '@lucide/svelte/icons/pencil';
  import Trash2Icon from '@lucide/svelte/icons/trash-2';

  // One row at a time is being renamed or confirmed for deletion.
  let renaming = $state('');
  let newName = $state('');
  let confirmingDelete = $state('');
  let busy = $state(false);

  onMount(() => {
    tagStore.load();
  });

  const target = $derived(normalizeTag(newName));
  const mergesInto = $derived(target !== renaming && tagStore.all.some((t) => t.tag === target));

  function startRename(tag: string) {
    confirmingDelete = '';
    renaming = tag;
    newName = tag;
  }

  async function saveRename() {
    if (!target) return;
    if (target !== renaming) {
      busy = true;
      const ok = await tagStore.rename(renaming, target);
      busy = false;
      if (!ok) return;
    }
    renaming = '';
  }

  async function remove(tag: string) {
    busy = true;
    await tagStore.rename(tag, '');
    busy = false;
    confirmingDelete = '';
  }
</script>

<Card.Root>
  <Card.Header>
    <Card.Title>Tags</Card.Title>
    <Card.Description>
      Your tags are saved on this drive, separate from the packs, so they stay when a pack is replaced or updated. Add
      tags to a pack from its card in the library.
    </Card.Description>
  </Card.Header>
  <Card.Content class="space-y-3">
    {#if tagStore.error}
      <p class="text-destructive text-sm">{tagStore.error}</p>
    {/if}

    {#if !tagStore.loaded}
      <p class="text-muted-foreground text-sm">Loading…</p>
    {:else if tagStore.all.length === 0}
      <p class="text-muted-foreground text-sm">No tags yet. Use <strong>Add tags</strong> on a pack's card to create one.</p>
    {:else}
      <ul class="divide-y rounded-lg border">
        {#each tagStore.all as { tag, count } (tag)}
          <li class="flex flex-wrap items-center gap-2 px-3 py-2">
            {#if renaming === tag}
              <form
                class="flex flex-1 flex-wrap items-center gap-2"
                onsubmit={(e) => {
                  e.preventDefault();
                  saveRename();
                }}
              >
                <Input
                  bind:value={newName}
                  maxlength={MAX_TAG_LENGTH + 20}
                  aria-label="New name for {tag}"
                  class="h-8 max-w-xs flex-1"
                  onkeydown={(e) => e.key === 'Escape' && (renaming = '')}
                  autofocus
                />
                <Button type="submit" size="sm" disabled={busy || !target}>
                  {mergesInto ? 'Merge' : 'Save'}
                </Button>
                <Button type="button" size="sm" variant="ghost" onclick={() => (renaming = '')}>Cancel</Button>
                {#if mergesInto}
                  <p class="text-muted-foreground w-full text-xs">
                    "{target}" already exists. Packs tagged "{tag}" will be tagged "{target}" instead.
                  </p>
                {:else if !target && newName.trim()}
                  <p class="text-destructive w-full text-xs">Tags can be up to {MAX_TAG_LENGTH} characters.</p>
                {/if}
              </form>
            {:else if confirmingDelete === tag}
              <span class="flex-1 text-sm">
                Remove "{tag}" from {count} {count === 1 ? 'pack' : 'packs'}? The packs themselves aren't changed.
              </span>
              <Button size="sm" variant="destructive" onclick={() => remove(tag)} disabled={busy}>Remove tag</Button>
              <Button size="sm" variant="ghost" onclick={() => (confirmingDelete = '')}>Cancel</Button>
            {:else}
              <span class="font-medium">{tag}</span>
              <span class="text-muted-foreground text-sm">{count} {count === 1 ? 'pack' : 'packs'}</span>
              <span class="ml-auto flex gap-1">
                <Button size="icon-sm" variant="ghost" aria-label="Rename {tag}" title="Rename" onclick={() => startRename(tag)}>
                  <PencilIcon />
                </Button>
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label="Delete {tag}"
                  title="Delete"
                  onclick={() => {
                    renaming = '';
                    confirmingDelete = tag;
                  }}
                >
                  <Trash2Icon />
                </Button>
              </span>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </Card.Content>
</Card.Root>
