<script lang="ts">
  import type { Snippet } from "svelte";
  import type { HTMLButtonAttributes } from "svelte/elements";
  import type { Icon } from "./types";

  let {
    variant = "default",
    icon: I,
    children,
    ...rest
  }: HTMLButtonAttributes & { variant?: "default" | "primary" | "subtle"; icon?: Icon; children?: Snippet } = $props();
</script>

<button type="button" class="btn" data-variant={variant} {...rest}>
  {#if I}<I />{/if}
  {#if children}<span class="label">{@render children()}</span>{/if}
</button>
