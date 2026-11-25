<script lang="ts">
  import type { Snippet } from "svelte";
  import Close from "~icons/fluent/dismiss-16-regular";
  import IconButton from "./IconButton.svelte";
  import { t } from "../labels/i18n.svelte";

  let {
    title,
    width = $bindable(420),
    onclose,
    children,
  }: { title: string; width?: number; onclose: () => void; children: Snippet } = $props();

  function resize(e: PointerEvent) {
    const grip = e.currentTarget as HTMLElement;
    const x0 = e.clientX;
    const w0 = width;
    grip.setPointerCapture(e.pointerId);
    grip.onpointermove = (m) => (width = Math.max(320, w0 + x0 - m.clientX));
    grip.onpointerup = () => (grip.onpointermove = null);
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions (Escape bubbling from any control closes the drawer) -->
<aside class="drawer" style:width="{width}px" onkeydown={(e) => e.key === "Escape" && !e.defaultPrevented && onclose()}>
  <div class="drawer-grip" role="separator" aria-orientation="vertical" onpointerdown={resize}></div>
  <header class="drawer-head">
    <h2>{title}</h2>
    <IconButton icon={Close} label={t("ui.close")} onclick={onclose} />
  </header>
  <div class="drawer-body">{@render children()}</div>
</aside>
