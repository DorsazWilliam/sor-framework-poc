import type { Component } from "svelte";
import type { Row, Screen } from "../api/contract";
import ViewScreen from "./ViewScreen.svelte";
import FileView from "./FileView.svelte";
import Entry from "../entry/Entry.svelte";
import Dashboard from "./Dashboard.svelte";

type Props = { screen: Screen; link?: Record<string, unknown>; parent?: Row };

// The hand-made screens by the name a screen declares in `component` (SPEC §19); any other screen is generic.
const components: Record<string, Component<Props>> = {
  entry: Entry as Component<Props>,
  pdf: FileView as Component<Props>,
  dashboard: Dashboard as Component<Props>,
};

export const screenComponent = (s: Screen) => (s.component && components[s.component]) || (ViewScreen as Component<Props>);
