<script lang="ts">
  import type { Snippet } from "svelte";

  let { title, children, end }: { title: string; children?: Snippet; end?: Snippet } = $props();
</script>

<div class="commandbar" role="toolbar" aria-label={title}>
  <h1>{title}</h1>
  {@render children?.()}
  {#if end}<div class="end">{@render end()}</div>{/if}
</div>
