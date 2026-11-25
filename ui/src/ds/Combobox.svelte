<script lang="ts">
  import { Combobox } from "bits-ui";
  import Chevron from "~icons/fluent/chevron-down-16-regular";
  import type { Item } from "./types";
  import { t } from "../labels/i18n.svelte";

  let {
    value = $bindable(),
    label,
    items,
    search,
    placeholder = "",
    id,
    onValueChange,
  }: {
    value?: string | null;
    label?: string;
    items?: Item[];
    search?: (q: string) => Promise<Item[]>;
    placeholder?: string;
    id?: string;
    onValueChange?: (v: string | null) => void;
  } = $props();

  let query = $state("");
  let open = $state(false);
  let remote = $state<Item[]>([]);

  const tokens = $derived(query.toLowerCase().split(/\s+/).filter(Boolean));
  const shown = $derived(
    search ? remote : (items ?? []).filter((i) => tokens.every((k) => i.label.toLowerCase().includes(k))).slice(0, 200),
  );
  const current = $derived(label ?? items?.find((i) => i.value === value)?.label ?? "");

  let loading = $state(false);
  $effect(() => {
    if (!search || !open) return;
    const q = query;
    loading = true;
    const timer = setTimeout(async () => {
      remote = await search(q);
      loading = false;
    }, 120);
    return () => clearTimeout(timer);
  });

  function set(v: string | null) {
    value = v;
    onValueChange?.(v);
  }
</script>

<Combobox.Root
  type="single"
  items={shown}
  bind:open
  inputValue={current}
  value={value ?? ""}
  onValueChange={(v) => set(v || null)}
  onOpenChange={(o) => o || (query = "")}
>
  <div class="input combo">
    <Combobox.Input
      {id}
      {placeholder}
      defaultValue={current}
      oninput={(e) => {
        query = e.currentTarget.value;
        if (!query) set(null);
      }}
      onkeydown={(e) => {
        // Tab accepts the highlighted option of the open list.
        const list = e.key === "Tab" && document.getElementById(e.currentTarget.getAttribute("aria-controls") ?? "");
        const v = list && list.querySelector("[data-highlighted]")?.getAttribute("data-value");
        if (v) set(v);
      }}
      onfocus={(e) => e.currentTarget.select()}
    />
    <Combobox.Trigger class="field-btn" tabindex={-1}><Chevron /></Combobox.Trigger>
  </div>
  <Combobox.Portal>
    <Combobox.Content class="popover" sideOffset={4}>
      {#each shown as it (it.value)}
        <Combobox.Item class="option" value={it.value} label={it.label}>{it.label}</Combobox.Item>
      {:else}
        <div class="empty">{loading ? "…" : t("ui.no_results")}</div>
      {/each}
    </Combobox.Content>
  </Combobox.Portal>
</Combobox.Root>
