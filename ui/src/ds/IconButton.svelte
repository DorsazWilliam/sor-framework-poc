<script lang="ts">
  import type { HTMLButtonAttributes } from "svelte/elements";
  import type { Icon } from "./types";
  import Tooltip from "./Tooltip.svelte";

  let { icon: I, label, ...rest }: HTMLButtonAttributes & { icon: Icon; label: string } = $props();
</script>

<Tooltip text={label}>
  <button type="button" class="btn icon-btn" data-variant="subtle" aria-label={label} {...rest}><I /></button>
</Tooltip>
