package kernel_test

import (
	"context"
	"encoding/json"
	"errors"
	neturl "net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
	"github.com/DorsazWilliam/sor-framework-poc/kernel/testmod"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const fixture = `
insert into org (name) values ('Org');
insert into person (subject, name, functions) values ('boss', 'Boss', '{entry,validate,admin}');
insert into person (subject, name, manager_id, functions) values ('alice', 'Alice', 1, '{entry}');
insert into person (subject, name, functions) values ('bob', 'Bob', '{entry}');
insert into person (subject, name, kind, functions) values ('bot', 'Bot', 'agent', '{entry,validate}');
insert into entry (person_id, day, worked_min, note) values
  (2, '2026-11-02', 60, 'a1'), (2, '2026-11-03', 120, 'a2'), (3, '2026-11-02', 30, 'b1'), (1, '2026-11-04', 90, 'boss');
insert into entry_line (entry_id, label, line_min) values (1, 'x', 30), (1, 'y', 30);
insert into connection (kind, name, endpoint, secret_name, subject) values ('acct', 'Accounting', 'http://127.0.0.1:1', 'acct', 'bot');
`

// setup creates a fresh database with the kernel and fixture schemas and boots a kernel on it.
func setup(t *testing.T) *kernel.Kernel {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	name := "sor_test_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	admin.Exec(ctx, "drop database if exists "+name+" with (force)")
	if _, err := admin.Exec(ctx, "create database "+name); err != nil {
		t.Fatal(err)
	}
	admin.Close(ctx)
	u, err := neturl.Parse(url)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	if err := kernel.Configure(ctx, u.String()); err != nil {
		t.Fatal(err)
	}
	pool, err := kernel.NewPool(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s, err := kernel.Load(testmod.Manifest, testmod.Module)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, pool, s.Schema())
	if err := kernel.EnsurePartitions(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := kernel.MigrateJobs(ctx, pool); err != nil {
		t.Fatal(err)
	}
	exec(t, pool, fixture)
	k, err := kernel.Boot(ctx, pool, s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	c, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release()
	if _, err := c.Conn().PgConn().Exec(context.Background(), sql).ReadAll(); err != nil {
		t.Fatal(err)
	}
}

func principal(t *testing.T, k *kernel.Kernel, subjects ...string) *kernel.Principal {
	t.Helper()
	p, err := k.Principal(context.Background(), subjects)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func view(t *testing.T, k *kernel.Kernel, p *kernel.Principal, name, body string) []map[string]any {
	t.Helper()
	out, err := k.View(context.Background(), p, name, []byte(body))
	if err != nil {
		t.Fatalf("view %s %s: %v", name, body, err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func call(t *testing.T, k *kernel.Kernel, p *kernel.Principal, name, body string) *kernel.Result {
	t.Helper()
	res, err := k.Call(context.Background(), p, name, []byte(body))
	if err != nil {
		t.Fatalf("call %s %s: %v", name, body, err)
	}
	return res
}

func callErr(t *testing.T, k *kernel.Kernel, p *kernel.Principal, name, body, code string) *kernel.Problem {
	t.Helper()
	_, err := k.Call(context.Background(), p, name, []byte(body))
	var pr *kernel.Problem
	if !errors.As(err, &pr) || pr.Code != code {
		t.Fatalf("call %s %s: want %s, got %v", name, body, code, err)
	}
	return pr
}

func TestWrap(t *testing.T) {
	k := setup(t)
	alice, boss := principal(t, k, "alice"), principal(t, k, "boss")
	if rows := view(t, k, alice, "entry_grid", `{}`); len(rows) != 2 {
		t.Fatalf("alice sees %d entries, want 2", len(rows))
	}
	rows := view(t, k, boss, "entry_grid", `{"sort":[{"col":"worked_min","dir":"desc"}],"limit":2}`)
	if len(rows) != 2 || rows[0]["worked_min"].(float64) != 120 || rows[1]["worked_min"].(float64) != 90 {
		t.Fatalf("boss page: %v", rows)
	}
	rows = view(t, k, boss, "entry_grid", `{"sort":[{"col":"worked_min","dir":"desc"}],"limit":2,"offset":2}`)
	if len(rows) != 1 || rows[0]["worked_min"].(float64) != 60 {
		t.Fatalf("boss second page: %v", rows)
	}
	rows = view(t, k, boss, "entry_grid", `{"filter":[{"col":"person","op":"contains","value":"ali"},{"col":"day","op":"ge","value":"2026-11-03"}]}`)
	if len(rows) != 1 || rows[0]["day"] != "2026-11-03" {
		t.Fatalf("filter: %v", rows)
	}
	rows = view(t, k, boss, "entry_grid", `{"group":["person"],"agg":[{"col":"worked_min","fn":"sum"},{"fn":"count"}],"sort":[{"col":"person","dir":"asc"}]}`)
	if len(rows) != 2 || rows[0]["person"] != "Alice" || rows[0]["sum_worked_min"].(float64) != 180 || rows[0]["count"].(float64) != 2 {
		t.Fatalf("group: %v", rows)
	}
	rows = view(t, k, boss, "entry_grid", `{"params":{"day_from":"2026-11-04"}}`)
	if len(rows) != 1 {
		t.Fatalf("param: %v", rows)
	}
	if rows := view(t, k, alice, "entry", `{"filter":[{"col":"id","op":"in","value":[1,3]}]}`); len(rows) != 1 || rows[0]["lines_min"].(float64) != 60 {
		t.Fatalf("generic list: %v", rows)
	}
	if rows := view(t, k, alice, "entry_line", `{}`); len(rows) != 2 {
		t.Fatalf("child list: %v", rows)
	}
	if rows := view(t, k, alice, "entry.lookup", `{}`); len(rows) != 2 || rows[0]["label"] != "a1" {
		t.Fatalf("lookup: %v", rows)
	}
	_, err := k.View(context.Background(), alice, "entry_grid", []byte(`{"sort":[{"col":"nope"}]}`))
	if pr, ok := err.(*kernel.Problem); !ok || pr.Code != "invalid" {
		t.Fatalf("bad sort: %v", err)
	}
	_, err = k.View(context.Background(), alice, "entry_grid", []byte(`{"limit":6000}`))
	if pr, ok := err.(*kernel.Problem); !ok || pr.Code != "invalid" {
		t.Fatalf("limit: %v", err)
	}
}

func TestCommands(t *testing.T) {
	k := setup(t)
	ctx := context.Background()
	alice, boss, bob := principal(t, k, "alice"), principal(t, k, "boss"), principal(t, k, "bob")

	pr := callErr(t, k, alice, "entry.validate", `{"ids":[1]}`, "forbidden")
	if pr.Needs != "validate" {
		t.Fatalf("needs: %v", pr.Needs)
	}
	callErr(t, k, bob, "entry.update", `{"ids":[1],"note":"x"}`, "not_found")
	callErr(t, k, alice, "entry.update", `{"ids":[999],"note":"x"}`, "not_found")
	callErr(t, k, alice, "entry.create", `{"person_id":3,"day":"2026-11-05","worked_min":10}`, "not_found")
	callErr(t, k, alice, "entry.update", `{"ids":[1],"worked_min":"x"}`, "invalid")
	callErr(t, k, alice, "entry.update", `{"ids":[1],"worked_min":-5}`, "constraint")

	res := call(t, k, alice, "entry.create", `{"person_id":2,"day":"2026-11-05","worked_min":10,"note":"new"}`)
	if len(res.IDs) != 1 || res.Changed[0].Table != "entry" {
		t.Fatalf("create: %+v", res)
	}
	id := res.IDs[0]
	call(t, k, alice, "entry.update", `{"ids":[`+itoa(id)+`],"note":"changed"}`)

	// An update that moves the row into a forbidden state is refused with the false conditions and the row.
	pr = callErr(t, k, alice, "entry.update", `{"ids":[`+itoa(id)+`],"worked_min":700}`, "rejected")
	if pr.Action != "editable" || len(pr.Rejected) != 1 || pr.Rejected[0].Conditions[0] != "short" || !strings.Contains(string(pr.Rejected[0].Row), `"worked_min": 700`) {
		t.Fatalf("ensure rejection: %+v", pr)
	}
	var minutes int
	k.Pool.QueryRow(ctx, "select worked_min from entry where id = $1", itoa(id)).Scan(&minutes)
	if minutes != 10 {
		t.Fatalf("rolled back: %d", minutes)
	}

	// Validation by the manager (reach team), then the entry is no longer editable.
	call(t, k, boss, "entry.validate", `{"ids":[1]}`)
	pr = callErr(t, k, alice, "entry.update", `{"ids":[1],"note":"late"}`, "rejected")
	if pr.Rejected[0].Conditions[0] != "not_validated" {
		t.Fatalf("when rejection: %+v", pr)
	}
	callErr(t, k, boss, "entry.validate", `{"ids":[4]}`, "not_found") // his own entry is not in his team

	// Child create locks the parent and checks its editable action.
	callErr(t, k, alice, "entry_line.create", `{"entry_id":1,"label":"z"}`, "rejected")
	call(t, k, alice, "entry_line.create", `{"entry_id":2,"label":"z","line_min":5}`)

	// Delete removes the child rows first; one event per changed row.
	res = call(t, k, alice, "entry.delete", `{"ids":[2]}`)
	if len(res.Changed) != 2 || res.Changed[0].Table != "entry_line" {
		t.Fatalf("delete: %+v", res)
	}
	var n int
	k.Pool.QueryRow(ctx, "select count(*) from event where root_table = 'entry' and root_id = 2").Scan(&n)
	if n != 3 { // line create, line delete, entry delete
		t.Fatalf("events of entry 2: %d", n)
	}
	var changes string
	k.Pool.QueryRow(ctx, "select changes::text from event where command = 'entry.validate'").Scan(&changes)
	if changes != `{"validated": [false, true]}` {
		t.Fatalf("validate event: %s", changes)
	}
	rows := view(t, k, alice, "history", `{"params":{"root_table":"entry","root_id":`+itoa(id)+`}}`)
	if len(rows) != 2 || rows[0]["command"] != "entry.update" {
		t.Fatalf("history: %v", rows)
	}
	if _, err := k.View(ctx, bob, "history", []byte(`{"params":{"root_table":"entry","root_id":1}}`)); err == nil {
		t.Fatal("history out of reach")
	}
}

func TestRoundTrips(t *testing.T) {
	k := setup(t)
	for _, tc := range []struct {
		name string
		max  int64
		run  func(ctx context.Context, p *kernel.Principal) error
	}{
		{"view", 2, func(ctx context.Context, p *kernel.Principal) error {
			_, err := k.View(ctx, p, "entry_grid", []byte(`{"filter":[{"col":"worked_min","op":"gt","value":1}]}`))
			return err
		}},
		{"update", 4, func(ctx context.Context, p *kernel.Principal) error {
			_, err := k.Call(ctx, p, "entry.update", []byte(`{"ids":[1],"note":"rt"}`))
			return err
		}},
		{"child create", 4, func(ctx context.Context, p *kernel.Principal) error {
			_, err := k.Call(ctx, p, "entry_line.create", []byte(`{"entry_id":1,"label":"rt"}`))
			return err
		}},
		{"command with a reaction", 5, func(ctx context.Context, p *kernel.Principal) error {
			_, err := k.Call(ctx, p, "entry.ping", []byte(`{"ids":[1]}`))
			return err
		}},
		{"command with a reaction, again", 5, func(ctx context.Context, p *kernel.Principal) error {
			_, err := k.Call(ctx, p, "entry.ping", []byte(`{"ids":[2]}`))
			return err
		}},
		{"delete", 4, func(ctx context.Context, p *kernel.Principal) error {
			_, err := k.Call(ctx, p, "entry.delete", []byte(`{"ids":[1]}`))
			return err
		}},
	} {
		ctx, timing := kernel.WithTiming(context.Background())
		p, err := k.Principal(ctx, []string{"alice"})
		if err == nil {
			err = tc.run(ctx, p)
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		t.Logf("%s: %d round trips", tc.name, timing.RT.Load())
		if rt := timing.RT.Load(); rt > tc.max {
			t.Fatalf("%s: %d round trips, budget %d", tc.name, rt, tc.max)
		}
	}
}

// TestUnderLock: a writer of a lower root and a writer of the upper root's child exclude each other.
func TestUnderLock(t *testing.T) {
	k := setup(t)
	alice, boss := principal(t, k, "alice"), principal(t, k, "boss")
	hold := make(chan struct{})
	held := make(chan struct{})
	k.BeforeCommit = func(ctx context.Context, b *pgx.Batch, c *kernel.Commit) error {
		if c.Command == "org_lock.create" {
			close(held)
			<-hold
		}
		return nil
	}
	errc := make(chan error, 1)
	go func() {
		_, err := k.Call(context.Background(), boss, "org_lock.create", []byte(`{"date_from":"2026-11-01","date_to":"2026-11-30"}`))
		errc <- err
	}()
	select {
	case <-held:
	case err := <-errc:
		t.Fatalf("org_lock.create: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := k.Call(context.Background(), alice, "entry.update", []byte(`{"ids":[1],"note":"during lock"}`))
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("entry update did not wait for the period lock: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(hold)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	var pr *kernel.Problem
	if err := <-done; !errors.As(err, &pr) || pr.Code != "rejected" || pr.Rejected[0].Conditions[0] != "not_locked" {
		t.Fatalf("entry update after the lock: %v", err)
	}

	// Reverse: the lock writer waits for an entry writer.
	k.Pool.Exec(context.Background(), "delete from org_lock")
	hold, held = make(chan struct{}), make(chan struct{})
	k.BeforeCommit = func(ctx context.Context, b *pgx.Batch, c *kernel.Commit) error {
		if c.Command == "entry.update" {
			close(held)
			<-hold
		}
		return nil
	}
	go func() {
		_, err := k.Call(context.Background(), alice, "entry.update", []byte(`{"ids":[1],"note":"before lock"}`))
		errc <- err
	}()
	<-held
	go func() {
		_, err := k.Call(context.Background(), boss, "org_lock.create", []byte(`{"date_from":"2026-11-01","date_to":"2026-11-30"}`))
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("lock did not wait for the entry writer: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(hold)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestLive(t *testing.T) {
	k := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go k.Listen(ctx)
	alice, boss, bob := principal(t, k, "alice"), principal(t, k, "boss"), principal(t, k, "bob")
	bossCh, stop := k.Subscribe(boss)
	defer stop()
	bobCh, stop2 := k.Subscribe(bob)
	defer stop2()
	time.Sleep(200 * time.Millisecond) // LISTEN in place
	res := call(t, k, alice, "entry.update", `{"ids":[1],"note":"live"}`)
	select {
	case msg := <-bossCh:
		var key map[string]any
		json.Unmarshal(msg, &key)
		if key["t"] != "entry" || key["c"] != "entry.update" || key["ids"].([]any)[0].(float64) != float64(res.IDs[0]) || key["a"] != nil {
			t.Fatalf("key: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no live key for the manager")
	}
	select {
	case msg := <-bobCh:
		t.Fatalf("bob got a key out of his reach: %s", msg)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestCheckStatic(t *testing.T) {
	m := `{"queries":[
 {"name":"Good","cmd":":many","text":"select 1","comments":[" @anchor none"," @needs any"]},
 {"name":"NoNeeds","cmd":":many","text":"select 1","comments":[" @anchor none"]},
 {"name":"NoReach","cmd":":many","text":"select 1","comments":[" @anchor person_id user"," @needs any"]},
 {"name":"Bare","cmd":":one","text":"select 1","comments":[" Something."]},
 {"name":"BadNeeds","cmd":":many","text":"select 1","comments":[" @anchor none"," @needs nope"]},
 {"name":"Orphan","cmd":":exec","text":"update org set name = 'x'","comments":[]}]}`
	s, err := kernel.Load([]byte(m), testmod.Module)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(s.CheckStatic(nil), "\n")
	for _, want := range []string{"query NoReach: anchored", "query Bare: missing @anchor", `query BadNeeds: function "nope"`,
		"query Orphan: :exec query referenced by no command", "effect query EntryValidate is not in the manifest"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing violation %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Good") {
		t.Errorf("Good flagged:\n%s", got)
	}
	s, _ = kernel.Load(testmod.Manifest, testmod.Module)
	if p := s.CheckStatic(nil); len(p) > 0 {
		t.Fatalf("fixture: %v", p)
	}
}

func itoa(i int64) string { b, _ := json.Marshal(i); return string(b) }
