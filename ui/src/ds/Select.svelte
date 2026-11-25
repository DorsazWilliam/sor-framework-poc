<script lang="ts">
  import { Select } from "bits-ui";
  import Chevron from "~icons/fluent/chevron-down-16-regular";
  import type { Item } from "./types";

  let {
    value = $bindable(""),
    items,
    placeholder = "",
    id,
    onValueChange,
  }: { value?: string; items: Item[]; placeholder?: string; id?: string; onValueChange?: (v: string) => void } =
    $props();

  const label = $derived(items.find((i) => i.value === value)?.label);
</script>

<Select.Root type="single" bind:value {items} {onValueChange}>
  <Select.Trigger class="input select" {id}>
    <span class:muted={!label}>{label || placeholder}</span>
    <Chevron />
  </Select.Trigger>
  <Select.Portal>
    <Select.Content class="popover" sideOffset={4}>
      <Select.Viewport>
        {#each items as it (it.value)}
          <Select.Item class="option" value={it.value} label={it.label}>{it.label || " "}</Select.Item>
        {/each}
      </Select.Viewport>
    </Select.Content>
  </Select.Portal>
</Select.Root>
