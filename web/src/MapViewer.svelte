<script lang="ts">
  import { onMount } from 'svelte';
  import * as maplibregl from 'maplibre-gl';
  import 'maplibre-gl/dist/maplibre-gl.css';
  // MapLibre 6 runs tile decoding in a separate worker module; have Vite
  // bundle it as a local asset so it never comes from a CDN.
  import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?url';
  maplibregl.setWorkerUrl(workerUrl);
  import { PMTiles, Protocol } from 'pmtiles';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import type { Pack } from '$lib/types.js';

  import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
  import ScanIcon from '@lucide/svelte/icons/scan';
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';

  let { pack }: { pack: Pack } = $props();

  let container = $state<HTMLDivElement>();
  let error = $state('');
  let renderError = $state('');
  let fitRegion = $state<() => void>(() => {});

  const m = $derived(pack.manifest!);

  onMount(() => {
    let map: maplibregl.Map | undefined;
    let disposed = false;
    const protocol = new Protocol();
    // pmtiles returns tile bytes as a Uint8Array; MapLibre 6's protocol type
    // declares ArrayBuffer, so hand it exactly that.
    maplibregl.addProtocol('pmtiles', async (params, abort) => {
      const res = await protocol.tile(params, abort);
      const d = res.data;
      const data = d instanceof Uint8Array ? d.buffer.slice(d.byteOffset, d.byteOffset + d.byteLength) : d;
      return { ...res, data: data as ArrayBuffer | object };
    });

    const base = `${location.origin}/content/${encodeURIComponent(m.packId)}/`;
    const fileUrl = (p: string) => base + p.split('/').map(encodeURIComponent).join('/');
    const archive = new PMTiles(fileUrl(m.entrypoint));
    protocol.add(archive);

    const dark = window.matchMedia('(prefers-color-scheme: dark)');

    // Styles reference pack files as "{base}…" so packs work at any address.
    async function loadStyle(): Promise<maplibregl.StyleSpecification> {
      const styles = m.viewer?.style;
      const path = (dark.matches ? styles?.dark : styles?.light) ?? styles?.light;
      if (!path) throw new Error('This map pack does not include a style.');
      const res = await fetch(fileUrl(path));
      if (!res.ok) throw new Error(`Style could not be loaded (${res.status}).`);
      const text = await res.text();
      return JSON.parse(text.replaceAll('{base}', base));
    }

    (async () => {
      try {
        const [h, style] = await Promise.all([archive.getHeader(), loadStyle()]);
        if (disposed || !container) return;
        const bounds: maplibregl.LngLatBoundsLike = [
          [h.minLon, h.minLat],
          [h.maxLon, h.maxLat],
        ];
        const pad = Math.max(h.maxLon - h.minLon, h.maxLat - h.minLat) * 0.5;
        map = new maplibregl.Map({
          container,
          style,
          bounds,
          maxBounds: [
            [h.minLon - pad, h.minLat - pad],
            [h.maxLon + pad, h.maxLat + pad],
          ],
          maxZoom: h.maxZoom + 3, // over-zoom past the data's detail level
          attributionControl: { compact: false },
        });
        map.addControl(new maplibregl.NavigationControl(), 'top-right');
        map.addControl(new maplibregl.ScaleControl({ unit: 'metric' }), 'bottom-left');
        // Surface rendering problems (e.g. unreadable tiles after the drive
        // is removed) instead of silently showing an empty map.
        map.on('error', (e: maplibregl.ErrorEvent) => {
          console.warn('map:', e.error);
          renderError = e.error?.message ?? String(e.error ?? 'unknown error');
        });
        fitRegion = () => map?.fitBounds(bounds, { padding: 20 });

        const onTheme = async () => {
          try {
            map?.setStyle(await loadStyle());
          } catch (e) {
            console.warn(e);
          }
        };
        dark.addEventListener('change', onTheme);
        map.once('remove', () => dark.removeEventListener('change', onTheme));
      } catch (e) {
        error = e instanceof Error ? e.message : String(e);
      }
    })();

    return () => {
      disposed = true;
      map?.remove();
      maplibregl.removeProtocol('pmtiles');
    };
  });
</script>

<div class="flex h-dvh flex-col">
  <header class="bg-background flex items-center gap-2 border-b px-3 py-2 sm:gap-3 sm:px-4">
    <Button variant="ghost" size="sm" href="#/">
      <ArrowLeftIcon data-icon="inline-start" />
      <span class="hidden sm:inline">Library</span>
    </Button>
    <div class="min-w-0 flex-1">
      <p class="truncate text-sm font-medium leading-tight">{m.title}</p>
      <p class="text-muted-foreground truncate text-xs">Offline map · browse and zoom (no place search yet)</p>
    </div>
    <Button variant="outline" size="sm" onclick={() => fitRegion()}>
      <ScanIcon data-icon="inline-start" />
      <span class="hidden sm:inline">Whole region</span>
    </Button>
  </header>

  {#if error}
    <div class="p-4">
      <Alert.Root variant="destructive">
        <CircleAlertIcon />
        <Alert.Title>The map couldn't be opened</Alert.Title>
        <Alert.Description>{error} If the drive was disconnected, reconnect it and press Rescan in the library.</Alert.Description>
      </Alert.Root>
    </div>
  {/if}
  <div class="relative min-h-0 flex-1">
    <div bind:this={container} class="h-full w-full"></div>
    {#if renderError}
      <div class="absolute bottom-10 left-1/2 z-10 w-[min(32rem,calc(100%-2rem))] -translate-x-1/2">
        <Alert.Root variant="destructive">
          <CircleAlertIcon />
          <Alert.Title>Part of the map couldn't be drawn</Alert.Title>
          <Alert.Description>{renderError}</Alert.Description>
        </Alert.Root>
      </div>
    {/if}
  </div>
</div>
