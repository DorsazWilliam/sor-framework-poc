<script lang="ts">
  import { SvelteSet } from "svelte/reactivity";
  import type { Col, Screen, Tile as TileT } from "../api/contract";
  import CommandBar from "../ds/CommandBar.svelte";
  import UserMenu from "../shell/UserMenu.svelte";
  import Params from "./Params.svelte";
  import SavedViews from "./SavedViews.svelte";
  import Tile from "./Tile.svelte";
  import { reads } from "../app/cache.svelte";
  import { defaults } from "../app/params";
  import { keep, route, type ScreenState } from "../app/router.svelte";
  import { screenLabel, session, view } from "../app/session.svelte";

  // A screen with tiles (SPEC §20 Dashboard): its params bar feeds every tile whose view has a param of the same
  // name; its state (params, tiles) is in the URL and is what a saved view keeps.
  let { screen }: { screen: Screen } = $props();

  const merge = (s: ScreenState) => ({ params: { ...defaults(screen.defaults, session.me), ...s.params }, tiles: s.tiles ?? screen.tiles });
  const init = merge(route.state);
  let params = $state<Record<string, unknown>>(init.params);
  let tiles = $state.raw<TileT[]>(init.tiles);
  const shown = new SvelteSet<TileT>();

  // Tiles whose view the principal may read (a saved dashboard may name others).
  const permitted = $derived(tiles.filter((x) => view(x.view)));
  const cols = $derived(
    [...new Map(permitted.flatMap((x) => view(x.view)!.params.filter((p) => !x.params?.[p.name]).map((p): [string, Col] => [p.name, p]))).values()],
  );
  const ready = $derived(permitted.every((x) => shown.has(x)));

  const current = (): ScreenState => ({ params: $state.snapshot(params), tiles });
  function apply(s: ScreenState) {
    ({ params, tiles } = merge(s));
  }

  $effect(() => {
    session.status = { rows: 0, selected: 0 };
    keep({ params: $state.snapshot(params), ...(tiles === screen.tiles ? {} : { tiles }) });
  });
</script>

<section class="dashboard" data-screen={screen.id} data-ready={ready || undefined} data-fresh={(ready && !reads.pending) || undefined}>
  <CommandBar title={screenLabel(screen)}>
    {#snippet end()}<UserMenu />{/snippet}
  </CommandBar>
  <Params {cols} bind:params id={screen.id}>
    <div class="spacer"></div>
    <SavedViews screen={screen.id} {current} {apply} />
  </Params>
  <div class="tiles">
    {#each permitted as tile, i (`${i}:${tile.title}`)}
      <Tile {tile} {params} onshown={() => shown.add(tile)} />
    {/each}
  </div>
</section>

<style>
  .dashboard {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    min-height: 0;
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    align-content: start;
    gap: var(--s-5);
    flex: 1;
    padding: var(--panel-pad);
    overflow: auto;
    background: var(--bg-app);
  }
  @media (max-width: 1100px) {
    .tiles {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
</style>
