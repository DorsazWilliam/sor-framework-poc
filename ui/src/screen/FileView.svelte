<script lang="ts">
  import type { Screen } from "../api/contract";
  import { api } from "../api/client";
  import { follow, reads } from "../app/cache.svelte";
  import { screenView } from "../app/session.svelte";
  import { linkWrap } from "../app/params";
  import { t } from "../labels/i18n.svelte";

  // Component "pdf": the file of the screen's row (its first column referencing file), displayed; the link selects the row.
  let { screen, link }: { screen: Screen; link: Record<string, unknown> } = $props();

  const v = $derived(screenView(screen));
  const column = $derived(v.columns.find((c) => c.ref === "file")?.name);
  let file = $state<{ url: string; name: string; type: string } | null>(null);
  let loaded = $state(false);
  let shown: unknown = null;

  $effect(() => {
    const { params, filter } = linkWrap(v, link);
    return follow([[v.name, { params, filter, limit: 1 }]], async ([[row]]) => {
      const id = column && row?.[column];
      if (id == null) file = null;
      else if (id !== shown) file = await api.file({ view: v.name, params, id: row.id as number, column: column! });
      shown = id;
      loaded = true;
    });
  });
</script>

<div class="fileview" data-screen={screen.id} data-ready={loaded || undefined} data-fresh={(loaded && !reads.pending) || undefined}>
  {#if file}
    <iframe src={file.url} title={file.name}></iframe>
  {:else if loaded}
    <p class="muted">{t("ui.no_file")}</p>
  {/if}
</div>

<style>
  .fileview {
    display: flex;
    flex: 1;
    min-height: 0;
  }
  iframe {
    flex: 1;
    border: 0;
    background: var(--surface-subtle);
  }
  p {
    padding: var(--panel-pad);
  }
</style>
