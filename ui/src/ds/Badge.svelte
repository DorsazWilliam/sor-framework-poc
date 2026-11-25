<script lang="ts">
  import type { Snippet } from "svelte";

  let {
    kind = "neutral",
    children,
  }: { kind?: "neutral" | "accent" | "positive" | "warning" | "negative"; children: Snippet } = $props();
</script>

<span class="badge" data-kind={kind}>{@render children()}</span>
