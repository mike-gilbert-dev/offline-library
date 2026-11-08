// User tags for packs, stored on the drive in OfflineLibrary/data/tags.json
// (see server/tags.go). Shared by the library page and Settings.

export const MAX_TAG_LENGTH = 40;

interface TagsFile {
  packs: Record<string, string[]>;
}

/** Mirrors normalizeTag on the server, so the UI shows what will be saved. */
export function normalizeTag(tag: string): string {
  const t = tag.trim().split(/\s+/).join(' ').toLowerCase();
  return t.length > MAX_TAG_LENGTH || /\p{Cc}/u.test(t) ? '' : t;
}

async function call(url: string, body?: unknown): Promise<TagsFile> {
  const res = await fetch(url, body === undefined ? undefined : {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error((await res.text()).trim() || `${res.status} ${res.statusText}`);
  return res.json();
}

class TagStore {
  byPack = $state<Record<string, string[]>>({});
  loaded = $state(false);
  error = $state('');

  /** Every tag in use, with how many packs have it, sorted by name. */
  all = $derived.by(() => {
    const counts = new Map<string, number>();
    for (const tags of Object.values(this.byPack)) {
      for (const t of tags) counts.set(t, (counts.get(t) ?? 0) + 1);
    }
    return [...counts].map(([tag, count]) => ({ tag, count })).sort((a, b) => a.tag.localeCompare(b.tag));
  });

  of(packId: string | undefined): string[] {
    return (packId && this.byPack[packId]) || [];
  }

  async load() {
    await this.run(() => call('/api/tags'));
    this.loaded = true;
  }

  setPackTags(packId: string, tags: string[]) {
    return this.run(() => call(`/api/tags/pack/${encodeURIComponent(packId)}`, tags));
  }

  /** Renames a tag on every pack; renaming to "" deletes it everywhere. */
  rename(from: string, to: string) {
    return this.run(() => call('/api/tags/rename', { from, to }));
  }

  private async run(op: () => Promise<TagsFile>): Promise<boolean> {
    try {
      this.byPack = (await op()).packs ?? {};
      this.error = '';
      return true;
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
      return false;
    }
  }
}

export const tagStore = new TagStore();
