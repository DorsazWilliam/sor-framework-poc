<script lang="ts" module>
  import type { Icon } from "./types";
  export type MenuItem =
    | { label: string; icon?: Icon; checked?: boolean; disabled?: boolean; onselect: () => void }
    | { heading: string }
    | "separator";
</script>

<script lang="ts">
  import type { Snippet } from "svelte";
  import { DropdownMenu } from "bits-ui";
  import Check from "~icons/fluent/checkmark-16-regular";

  let {
    items,
    trigger,
    align = "end",
  }: { items: MenuItem[]; trigger: Snippet<[Record<string, unknown>]>; align?: "start" | "end" } = $props();
</script>

<DropdownMenu.Root>
  <DropdownMenu.Trigger>
    {#snippet child({ props })}{@render trigger(props)}{/snippet}
  </DropdownMenu.Trigger>
  <DropdownMenu.Portal>
    <DropdownMenu.Content class="popover" {align} sideOffset={4}>
      {#each items as it, i (i)}
        {#if it === "separator"}
          <DropdownMenu.Separator class="menu-sep" />
        {:else if "heading" in it}
          <div class="menu-label">{it.heading}</div>
        {:else}
          <DropdownMenu.Item class="menu-item" disabled={it.disabled} onSelect={it.onselect}>
            {#if it.checked !== undefined}
              <span class="check">{#if it.checked}<Check />{/if}</span>
            {:else if it.icon}
              <it.icon />
            {/if}
            {it.label}
          </DropdownMenu.Item>
        {/if}
      {/each}
    </DropdownMenu.Content>
  </DropdownMenu.Portal>
</DropdownMenu.Root>
