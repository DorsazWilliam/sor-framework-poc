<script lang="ts">
  import type { Snippet } from "svelte";

  let { label, id, error, children }: { label: string; id: string; error?: string; children: Snippet } = $props();
</script>

<div class="field">
  <label for={id} title={label}>{label}</label>
  <div>{@render children()}</div>
  {#if error}<div class="field-error">{error}</div>{/if}
</div>
