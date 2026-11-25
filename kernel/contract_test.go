package kernel_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
)

// TestContract: call id in the result, history stamps as ids, Ref, sort on a ref by its label.
func TestContract(t *testing.T) {
	k := setup(t)
	alice, boss := principal(t, k, "alice"), principal(t, k, "boss")

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
	bot := principal(t, k, "bot", "alice")
	call(t, k, bot, "saved_view.create", `{"owner_id":2,"screen":"entries","name":"mine"}`)
	callErr(t, k, bot, "saved_view.create", `{"owner_id":4,"screen":"entries","name":"bot's"}`, "not_found")

	col := func(cols []kernel.Col, name string) kernel.Col {
		i := slices.IndexFunc(cols, func(c kernel.Col) bool { return c.Name == name })
		if i < 0 {
			t.Fatalf("no column %s", name)
		}
		return cols[i]
	}
	if c := col(k.ViewByName("history").Columns, "principal_id"); c.Ref != "person" {
		t.Fatalf("history principal_id ref: %+v", c)
	}
	if c := col(k.ViewByName("run").Columns, "for_id"); c.Ref != "person" {
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
