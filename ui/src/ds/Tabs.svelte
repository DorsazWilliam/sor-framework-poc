<script lang="ts">
  import type { Snippet } from "svelte";
  import { Tabs } from "bits-ui";
  import type { Item } from "./types";

  let { value = $bindable(""), items, panel }: { value?: string; items: Item[]; panel: Snippet<[string]> } = $props();
</script>

<Tabs.Root bind:value class="tabs-root">
  <Tabs.List class="tabs">
    {#each items as it (it.value)}
      <Tabs.Trigger class="tab" value={it.value}>{it.label}</Tabs.Trigger>
    {/each}
  </Tabs.List>
  {#each items as it (it.value)}
    <Tabs.Content class="tab-panel" value={it.value}>
      {#if value === it.value}{@render panel(it.value)}{/if}
    </Tabs.Content>
  {/each}
</Tabs.Root>
