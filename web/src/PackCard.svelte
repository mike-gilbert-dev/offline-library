<script>
  let { pack } = $props();

  const m = $derived(pack.manifest);

  const statusLabels = {
    ok: 'Ready',
    unsupported: 'Not supported yet',
    incomplete: 'Incomplete',
    invalid: 'Damaged',
  };

  const contentUrl = $derived(
    m ? `/content/${encodeURIComponent(m.packId)}/${m.entrypoint.split('/').map(encodeURIComponent).join('/')}` : '',
  );

  function formatBytes(n) {
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    while (n >= 1024 && i < units.length - 1) {
      n /= 1024;
      i++;
    }
    return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
  }
</script>

<article class="card {pack.status}">
  <div class="top">
    <span class="category">{m?.category || 'pack'}</span>
    <span class="pill {pack.status}">{statusLabels[pack.status] ?? pack.status}</span>
  </div>

  <h2>{m?.title || pack.manifestPath}</h2>
  {#if m?.description}<p class="desc">{m.description}</p>{/if}

  {#if m}
    <dl class="meta">
      <div><dt>Format</dt><dd>{m.format || '—'}</dd></div>
      {#if m.version}<div><dt>Edition</dt><dd>{m.version}</dd></div>{/if}
      {#if m.contentDate}<div><dt>Content date</dt><dd>{m.contentDate}</dd></div>{/if}
      {#if m.languages?.length}<div><dt>Language</dt><dd>{m.languages.join(', ')}</dd></div>{/if}
      {#if pack.totalBytes}<div><dt>Size</dt><dd>{formatBytes(pack.totalBytes)}</dd></div>{/if}
    </dl>
  {/if}

  {#if pack.problems.length}
    <ul class="problems">
      {#each pack.problems as problem}<li>{problem}</li>{/each}
    </ul>
  {/if}

  {#if m?.source?.name || m?.source?.license}
    <p class="source">
      Source: {m.source.name || 'unknown'}{#if m.source.license}&nbsp;· {m.source.license}{/if}
      {#if m.source.attribution}<br />{m.source.attribution}{/if}
    </p>
  {/if}

  {#if pack.status === 'ok'}
    <div class="actions">
      <a class="button" href={contentUrl} target="_blank" rel="noopener">Open</a>
    </div>
  {/if}
</article>
