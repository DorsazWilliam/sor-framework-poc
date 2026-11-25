<script lang="ts">
  import type { HTMLInputAttributes } from "svelte/elements";

  let {
    value = $bindable(),
    parse,
    format,
    ...rest
  }: Omit<HTMLInputAttributes, "value"> & {
    value?: number | null;
    parse: (s: string) => number | null | undefined;
    format: (n: number) => string;
  } = $props();

  const show = (v: number | null | undefined) => (v == null ? "" : format(v));
  let text = $state(show(value));
  let invalid = $state(false);
  let focused = false;

  $effect(() => {
    const v = value;
    if (!focused) text = show(v);
  });

  function commit() {
    const v = parse(text);
    invalid = v === undefined;
    if (v === undefined) return;
    value = v;
    text = show(v);
  }
</script>

<input
  class="input num"
  autocomplete="off"
  aria-invalid={invalid}
  bind:value={text}
  onfocus={(e) => {
    focused = true;
    e.currentTarget.select();
  }}
  onblur={() => {
    focused = false;
    commit();
  }}
  onkeydown={(e) => e.key === "Enter" && commit()}
  {...rest}
/>
