<script lang="ts">
  import type { Lang } from "../api/contract";
  import Menu, { type MenuItem } from "../ds/Menu.svelte";
  import Button from "../ds/Button.svelte";
  import Person from "~icons/fluent/person-16-regular";
  import SignOut from "~icons/fluent/sign-out-20-regular";
  import { changeLang, session, setTheme } from "../app/session.svelte";
  import { i18n, t } from "../labels/i18n.svelte";

  const langs: Lang[] = ["fr", "de", "en"];

  const items = $derived<MenuItem[]>([
    { heading: t("ui.language") },
    ...langs.map((l) => ({ label: t(`ui.lang.${l}`), checked: i18n.lang === l, onselect: () => changeLang(l) })),
    "separator",
    { heading: t("ui.theme") },
    ...(["light", "dark"] as const).map((th) => ({
      label: t(`ui.theme.${th}`),
      checked: session.theme === th,
      onselect: () => setTheme(th),
    })),
    "separator",
    { label: t("ui.logout"), icon: SignOut, onselect: () => (location.href = "/auth/logout") },
  ]);
</script>

<Menu {items}>
  {#snippet trigger(props)}
    <Button variant="subtle" icon={Person} data-testid="user-menu" {...props}>
      {session.me.name}{session.me.for ? ` → ${session.me.for.name}` : ""}
    </Button>
  {/snippet}
</Menu>
