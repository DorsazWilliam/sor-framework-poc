<script lang="ts">
  import type { Snippet } from "svelte";
  import { Popover } from "bits-ui";
  import More from "~icons/fluent/more-horizontal-16-regular";
  import { t } from "../labels/i18n.svelte";

  // Commands keep their full label; those that do not fit go, in order, into the "more" popover.
  let { title, children, end }: { title: string; children?: Snippet; end?: Snippet } = $props();

  let shown = $state(Infinity); // number of commands in the bar
  let open = $state(false);

  function measure(box: HTMLElement) {
    const fit = () => {
      const items = [...box.children] as HTMLElement[];
      const right = box.getBoundingClientRect().right;
      const n = items.findIndex((el) => el.getBoundingClientRect().right > right + 0.5);
      shown = n < 0 ? Infinity : n;
      items.forEach((el, i) => (el.style.visibility = i < shown ? "" : "hidden"));
    };
    const ro = new ResizeObserver(fit);
    const watch = () => [box, ...box.children].forEach((el) => ro.observe(el));
    const mo = new MutationObserver(() => (watch(), fit()));
    watch();
    mo.observe(box, { childList: true });
    return () => (ro.disconnect(), mo.disconnect());
  }

  // In the popover, the commands already in the bar are hidden.
  function overflow(list: HTMLElement) {
    [...list.children].forEach((el, i) => ((el as HTMLElement).hidden = i < shown));
  }
</script>

<div class="commandbar" role="toolbar" aria-label={title}>
  <h1>{title}</h1>
  <div class="commands" {@attach measure}>{@render children?.()}</div>
  {#if shown !== Infinity}
    <Popover.Root bind:open>
      <Popover.Trigger>
        {#snippet child({ props })}
          <button {...props} type="button" class="btn icon-btn" data-variant="subtle" aria-label={t("ui.more")} title={t("ui.more")}><More /></button>
        {/snippet}
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content class="popover more-commands" align="end" sideOffset={4}>
          <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
          <div class="list" {@attach overflow} onclick={(e) => (e.target as Element).closest("button") && (open = false)}>
            {@render children?.()}
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  {/if}
  {#if end}<div class="end">{@render end()}</div>{/if}
</div>
