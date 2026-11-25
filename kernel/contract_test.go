package kernel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
	"github.com/DorsazWilliam/sor-framework-poc/kernel/testmod"
)

// TestDashboard: a date grouped by period, tiles in the meta and filtered by capability, the version of the registry,
// the boot check of tiles.
func TestDashboard(t *testing.T) {
	k := setup(t)
	ctx := context.Background()
	boss := principal(t, k, "boss")

	// ISO weeks start on Monday: 2026-11-02..04 are one week (boss reads his and alice's entries); sort and aggregates go with the period column.
	rows := view(t, k, boss, "entry_grid", `{"group":[{"col":"day","by":"week"}],"agg":[{"col":"worked_min","fn":"sum"},{"fn":"count"}]}`)
	if len(rows) != 1 || rows[0]["day_week"] != "2026-11-02" || rows[0]["sum_worked_min"].(float64) != 270 || rows[0]["count"].(float64) != 3 {
		t.Fatalf("by week: %v", rows)
	}
	rows = view(t, k, boss, "entry_grid", `{"group":[{"col":"day","by":"day"},"person_id"],"agg":[{"col":"worked_min","fn":"sum"}],"sort":[{"col":"day_day","dir":"desc"}]}`)
	if len(rows) != 3 || rows[0]["day_day"] != "2026-11-04" || rows[2]["day_day"] != "2026-11-02" {
		t.Fatalf("by day, sorted: %v", rows)
	}
	rows = view(t, k, boss, "entry_grid", `{"group":[{"col":"day","by":"month"}],"agg":[{"fn":"count"}],"sort":[{"col":"day_month"}]}`)
	if len(rows) != 1 || rows[0]["day_month"] != "2026-11-01" {
		t.Fatalf("by month: %v", rows)
	}
	for _, g := range []string{`{"col":"worked_min","by":"week"}`, `{"col":"day","by":"hour"}`, `{"col":"nope","by":"week"}`} {
		if _, err := k.View(ctx, boss, "entry_grid", []byte(`{"group":[`+g+`]}`)); err == nil || err.(*kernel.Problem).Code != "invalid" {
			t.Fatalf("group %s: %v", g, err)
		}
	}

	// Tiles are in the meta; a tile whose view is not permitted is dropped, and a screen left without content.
	screen := func(m kernel.Meta, id string) *kernel.Screen {
		i := slices.IndexFunc(m.Screens, func(s kernel.Screen) bool { return s.ID == id })
		if i < 0 {
			return nil
		}
		return &m.Screens[i]
	}
	b := screen(k.Permitted(boss), "board")
	if b == nil || len(b.Tiles) != 2 || b.Tiles[0].Wrap.Group[0] != (kernel.GroupBy{Col: "day", By: "week"}) || screen(k.Permitted(boss), "admin_board") == nil {
		t.Fatalf("boss board: %+v", b)
	}
	raw, _ := json.Marshal(b.Tiles[0])
	if !strings.Contains(string(raw), `"group":[{"col":"day","by":"week"}]`) {
		t.Fatalf("tile json: %s", raw)
	}
	alice := k.Permitted(principal(t, k, "user"))
	if b := screen(alice, "board"); b == nil || len(b.Tiles) != 1 || b.Tiles[0].View != "entry_grid" || screen(alice, "admin_board") != nil {
		t.Fatalf("alice board: %+v", b)
	}
	if s := screen(alice, "entries"); s == nil || s.Tiles == nil {
		t.Fatalf("tiles never null: %+v", s)
	}

	// The version is stable across boots and changes with a declaration; a bad tile fails the boot.
	boot := func(screens ...kernel.Screen) (*kernel.Kernel, error) {
		s, err := kernel.Load(testmod.Manifest, testmod.Module, kernel.Module{Name: "extra", Register: func(r *kernel.Registry) {
			for _, sc := range screens {
				r.Screen(sc)
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
		return kernel.Boot(ctx, k.Pool, s)
	}
	again, err := boot()
	if err != nil || again.Meta.Version != k.Meta.Version || len(k.Meta.Version) != 16 || k.Permitted(boss).Version != k.Meta.Version {
		t.Fatalf("version %q, again %q: %v", k.Meta.Version, again.Meta.Version, err)
	}
	more, err := boot(kernel.Screen{ID: "more", Group: "time", Tiles: []kernel.Tile{{Title: "tile.x", View: "org", Form: "table"}}})
	if err != nil || more.Meta.Version == k.Meta.Version {
		t.Fatalf("version after a declaration: %q: %v", more.Meta.Version, err)
	}
	_, err = boot(kernel.Screen{ID: "bad", Group: "time", Tiles: []kernel.Tile{
		{Title: "tile.a", View: "nope", Form: "number"},
		{Title: "tile.b", View: "entry_grid", Form: "pie", Link: "nowhere"},
		{Title: "tile.c", View: "entry_grid", Form: "bar", Wrap: &kernel.Wrap{Group: []kernel.GroupBy{{Col: "person", By: "week"}}}},
		{Title: "tile.d", View: "entry_grid", Form: "bar", Wrap: &kernel.Wrap{Agg: []kernel.Agg{{Col: "nope", Fn: "sum"}}}},
	}})
	for _, want := range []string{"tile tile.a: view nope does not exist", `tile tile.b: unknown form "pie"`, "tile tile.b: link nowhere is not a screen",
		"tile tile.c: wrap on entry_grid", "tile tile.d: wrap on entry_grid"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("boot: missing %q in %v", want, err)
		}
	}
}

// TestContract: call id in the result, history stamps as ids, Ref, sort on a ref by its label.
func TestContract(t *testing.T) {
	k := setup(t)
	alice, boss := principal(t, k, "user"), principal(t, k, "boss")

	res := call(t, k, alice, "entry.update", `{"ids":[1],"note":"call"}`)
	if res.Call == "" || count(t, k, "select count(*) from event where call = $1::uuid", res.Call) != 1 {
		t.Fatalf("call: %+v", res)
	}
	rows := view(t, k, alice, "history", `{"params":{"root_table":"entry","root_id":1}}`)
	if len(rows) != 1 || rows[0]["principal_id"].(float64) != float64(alice.ID) || rows[0]["for_id"] != nil {
		t.Fatalf("history: %v", rows)
	}

	// A module re-declaring a kernel command adds options.
	if c := k.CommandByName("delivery.retry"); c.Needs != "admin" || c.Table != "delivery" {
		t.Fatalf("re-declared command: %+v", c)
	}

	// Reach self is the effective identity: an agent acting for alice owns alice's saved views.
	bot := principal(t, k, "agent-user", "user")
	call(t, k, bot, "saved_view.create", `{"owner_id":2,"screen":"entries","name":"mine"}`)
	callErr(t, k, bot, "saved_view.create", `{"owner_id":4,"screen":"entries","name":"bot's"}`, "not_found")

	// A table declared with Group has a generated screen: its list view and table, a tab per child table by its foreign key.
	i := slices.IndexFunc(k.Meta.Screens, func(s kernel.Screen) bool { return s.ID == "org" })
	if i < 0 {
		t.Fatal("no generated screen for org")
	}
	if s := k.Meta.Screens[i]; s.Group != "setup" || s.Table != "org" || len(s.Views) != 1 || s.Views[0] != "org" || len(s.Children) != 1 ||
		s.Children[0].ID != "org_lock" || s.Children[0].Table != "org_lock" || s.Children[0].Views[0] != "org_lock" || s.Children[0].Link["org_id"] != "id" {
		t.Fatalf("generated screen: %+v", s)
	}
	if !slices.Contains(k.Meta.Groups, "setup") {
		t.Fatalf("groups: %v", k.Meta.Groups)
	}

	col := func(cols []kernel.Col, name string) kernel.Col {
		i := slices.IndexFunc(cols, func(c kernel.Col) bool { return c.Name == name })
		if i < 0 {
			t.Fatalf("no column %s", name)
		}
		return cols[i]
	}
	meta := func(name string) *kernel.View {
		return k.Meta.Views[slices.IndexFunc(k.Meta.Views, func(v *kernel.View) bool { return v.Name == name })]
	}
	if c := col(meta("history").Columns, "principal_id"); c.Ref != "person" {
		t.Fatalf("history principal_id ref: %+v", c)
	}
	if c := col(meta("run").Columns, "for_id"); c.Ref != "person" {
		t.Fatalf("run for_id ref: %+v", c)
	}

	// person_id sorts by the person's name (Alice before Boss), id stays the last key; filter and group stay on the id.
	rows = view(t, k, boss, "entry_grid", `{"sort":[{"col":"person_id"}],"limit":2}`)
	if len(rows) != 2 || rows[0]["id"].(float64) != 1 || rows[1]["id"].(float64) != 2 {
		t.Fatalf("sort on ref: %v", rows)
	}
	rows = view(t, k, boss, "entry_grid", `{"sort":[{"col":"person_id","dir":"desc"}]}`)
	if len(rows) != 3 || rows[0]["person"] != "Boss" {
		t.Fatalf("sort on ref desc: %v", rows)
	}
	rows = view(t, k, boss, "entry_grid", `{"group":["person_id"],"agg":[{"fn":"count"}],"sort":[{"col":"person_id"}]}`)
	if len(rows) != 2 || rows[0]["person_id"].(float64) != 2 || rows[0]["count"].(float64) != 2 {
		t.Fatalf("group on ref: %v", rows)
	}
	if rows = view(t, k, boss, "entry_grid", `{"filter":[{"col":"person_id","op":"eq","value":1}]}`); len(rows) != 1 {
		t.Fatalf("filter on ref: %v", rows)
	}
}

// TestMetaNotNull: every list and map of the meta is present, empty rather than null (SPEC §13); optional
// fields are omitted, so no value of the meta JSON is null.
func TestMetaNotNull(t *testing.T) {
	k := setup(t)
	raw, err := json.Marshal(k.Meta)
	if err != nil {
		t.Fatal(err)
	}
	var meta any
	json.Unmarshal(raw, &meta)
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch v := v.(type) {
		case nil:
			t.Errorf("null at %s", path)
		case map[string]any:
			for k, x := range v {
				walk(path+"."+k, x)
			}
		case []any:
			for i, x := range v {
				walk(fmt.Sprintf("%s[%d]", path, i), x)
			}
		}
	}
	walk("meta", meta)
}
