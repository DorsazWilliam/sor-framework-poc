package kernel_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
	"github.com/DorsazWilliam/sor-framework-poc/kernel/testmod"
)

func start(t *testing.T, k *kernel.Kernel) {
	t.Helper()
	if err := k.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		k.Stop(ctx)
	})
}

// eventually polls cond until it holds or the timeout passes.
func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(timeout); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timeout: %s", what)
}

func count(t *testing.T, k *kernel.Kernel, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := k.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestReaction(t *testing.T) {
	k := setup(t)
	start(t, k)
	alice, boss := principal(t, k, "user"), principal(t, k, "boss")
	call(t, k, alice, "entry.ping", `{"ids":[2]}`)
	var note string
	eventually(t, 5*time.Second, "reaction job", func() bool {
		k.Pool.QueryRow(context.Background(), "select note from entry where id = 2").Scan(&note)
		return strings.HasPrefix(note, "marked ")
	})
	if n := count(t, k, "select count(*) from event where command = 'entry.update' and row_id = 2 and principal_id = $1", itoa(alice.ID)); n != 1 {
		t.Fatalf("the job did not act as alice: %d", n)
	}
	var callID string
	k.Pool.QueryRow(context.Background(), "select args->'args'->>'call' from river.river_job where args->>'kind' = 'entry.mark'").Scan(&callID)
	if note != "marked "+callID {
		t.Fatalf("args: %s vs call %s", note, callID)
	}
	// A rejected command inserts no job.
	call(t, k, boss, "entry.validate", `{"ids":[1]}`)
	callErr(t, k, alice, "entry.ping", `{"ids":[1]}`, "rejected")
	if n := count(t, k, "select count(*) from river.river_job where args->>'kind' = 'entry.mark'"); n != 1 {
		t.Fatalf("jobs after a rejection: %d", n)
	}
}

func TestPeriodic(t *testing.T) {
	k := setup(t)
	before := testmod.Ticks.Load()
	start(t, k)
	eventually(t, 10*time.Second, "two ticks", func() bool { return testmod.Ticks.Load() >= before+2 })
	if testmod.TickBy.Load() != principal(t, k, "agent-user").ID {
		t.Fatalf("tick ran as %d", testmod.TickBy.Load())
	}
}

type runRow struct {
	State    string
	Total    int
	Done     int
	Rejected []struct {
		Code       string
		Conditions []string
		Input      map[string]any
	}
}

func readRun(t *testing.T, k *kernel.Kernel, id int64) runRow {
	t.Helper()
	var r runRow
	var rej []byte
	if err := k.Pool.QueryRow(context.Background(), "select state::text, total, done, rejected from run where id = $1", itoa(id)).Scan(&r.State, &r.Total, &r.Done, &rej); err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(rej, &r.Rejected)
	return r
}

func TestRun(t *testing.T) {
	k := setup(t)
	start(t, k)
	boss := principal(t, k, "boss")
	res := call(t, k, boss, "entry.run", `{"day":"2026-11-10","n":6,"split":3,"bad":4}`)
	if res.Run == nil {
		t.Fatalf("no run: %+v", res)
	}
	var r runRow
	eventually(t, 10*time.Second, "run done", func() bool { r = readRun(t, k, *res.Run); return r.State == "done" })
	if r.Total != 6 || r.Done != 6 || len(r.Rejected) != 1 || r.Rejected[0].Code != "rejected" || r.Rejected[0].Conditions[0] != "short" ||
		r.Rejected[0].Input["worked_min"].(float64) != 700 {
		t.Fatalf("run: %+v", r)
	}
	if n := count(t, k, "select count(*) from run where id = $1 and owner_id = $2", itoa(*res.Run), itoa(boss.Self)); n != 1 {
		t.Fatal("the run is not its starter's")
	}
	if n := count(t, k, "select count(*) from entry where day = '2026-11-10'"); n != 5 {
		t.Fatalf("entries: %d", n)
	}
	if n := count(t, k, "select count(*) from event where command = 'entry.create' and principal_id = $1", itoa(boss.ID)); n != 5 {
		t.Fatalf("events: %d", n)
	}
}

// TestRunCall: a run is an invocation of its own (SPEC §9): its progress, its commit and its events carry the
// run job's call id, not the one of the command that started it, so the client does not take them for its echo.
func TestRunCall(t *testing.T) {
	k := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go k.Listen(ctx)
	start(t, k)
	keys, stop := k.Subscribe(principal(t, k, "boss"))
	defer stop()
	time.Sleep(200 * time.Millisecond) // LISTEN in place
	res := call(t, k, principal(t, k, "boss"), "entry.run", `{"day":"2026-11-12","n":3,"split":3,"bad":-1}`)
	sent := res.Call
	if res.Run == nil {
		t.Fatalf("start: %+v", res)
	}
	eventually(t, 10*time.Second, "run done", func() bool { return readRun(t, k, *res.Run).State == "done" })
	var started, progress int
	calls := map[string]bool{}
	for drained := false; !drained; {
		select {
		case msg := <-keys:
			var key struct{ T, E string }
			json.Unmarshal(msg, &key)
			switch {
			case key.T == "run" && key.E == sent:
				started++
			case key.T == "run":
				progress++
				calls[key.E] = true
			}
		case <-time.After(300 * time.Millisecond):
			drained = true
		}
	}
	if started != 1 || progress < 2 || len(calls) != 1 {
		t.Fatalf("run keys: %d with the call of the start, %d progress keys with calls %v", started, progress, calls)
	}
	for runCall := range calls {
		if n := count(t, k, "select count(*) from event where call = $1::uuid and command = 'entry.create'", runCall); n != 3 {
			t.Fatalf("events of the run's call: %d", n)
		}
	}
}

func TestRunCancel(t *testing.T) {
	k := setup(t)
	start(t, k)
	ctx := context.Background()
	boss := principal(t, k, "boss")
	entries := count(t, k, "select count(*) from entry")
	events := count(t, k, "select count(*) from event where tbl <> 'run'")
	// Hold person 2: the run creates the entries of person 1, then waits on the foreign key check.
	lock, err := k.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Exec(ctx, "select from person where id = 2 for update"); err != nil {
		t.Fatal(err)
	}
	res := call(t, k, boss, "entry.run", `{"day":"2026-11-11","n":6,"split":2,"bad":-1}`)
	eventually(t, 10*time.Second, "progress while the run is open", func() bool {
		r := readRun(t, k, *res.Run)
		return r.State == "running" && r.Total == 6 && r.Done == 2
	})
	call(t, k, boss, "run.cancel", `{"ids":[`+itoa(*res.Run)+`]}`)
	lock.Rollback(ctx)
	eventually(t, 10*time.Second, "run cancelled", func() bool { return readRun(t, k, *res.Run).State == "cancelled" })
	if n := count(t, k, "select count(*) from entry"); n != entries {
		t.Fatalf("entries: %d, before %d", n, entries)
	}
	if n := count(t, k, "select count(*) from event where tbl <> 'run'"); n != events {
		t.Fatalf("events: %d, before %d", n, events)
	}
}

func TestDelivery(t *testing.T) {
	k := setup(t)
	t.Setenv("SECRET_acct", "s3cret")
	var mu sync.Mutex
	fail := 2
	accepted := map[string]int{}
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if r.URL.Path != "/invoices" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "Bearer s3cret" {
			w.WriteHeader(400)
			return
		}
		if fail != 0 {
			fail--
			w.WriteHeader(503)
			return
		}
		accepted[r.Header.Get("Idempotency-Key")]++
		w.WriteHeader(201)
	}))
	defer srv.Close()
	k.Pool.Exec(context.Background(), "update connection set endpoint = $1", srv.URL)
	start(t, k)
	boss, bot := principal(t, k, "boss"), principal(t, k, "agent-user")

	call(t, k, boss, "entry.export", `{"ids":[1]}`)
	eventually(t, 30*time.Second, "delivery accepted", func() bool {
		return count(t, k, "select count(*) from delivery_result where ok") == 1
	})
	mu.Lock()
	if attempts != 3 || len(accepted) != 1 || accepted["1"] != 1 {
		t.Fatalf("attempts %d, accepted %v", attempts, accepted)
	}
	mu.Unlock()
	if n := count(t, k, "select count(*) from delivery_result where not ok and status = 503"); n != 2 {
		t.Fatalf("failed results: %d", n)
	}
	if n := count(t, k, "select count(*) from event where command = 'delivery.result' and principal_id = $1", itoa(bot.ID)); n != 3 {
		t.Fatalf("results not recorded as the connection's principal: %d", n)
	}

	// A delivery whose attempts are exhausted is sent again by delivery.retry.
	mu.Lock()
	fail = -1
	mu.Unlock()
	call(t, k, boss, "entry.export", `{"ids":[2]}`)
	eventually(t, 40*time.Second, "attempts exhausted", func() bool {
		return count(t, k, "select count(*) from river.river_job where args->>'kind' = 'delivery.send' and state = 'discarded'") == 1
	})
	if n := count(t, k, "select count(*) from delivery_result where delivery_id = 2"); n != 5 {
		t.Fatalf("attempts of delivery 2: %d", n)
	}
	mu.Lock()
	fail = 0
	mu.Unlock()
	call(t, k, boss, "delivery.retry", `{"ids":[2]}`)
	eventually(t, 10*time.Second, "retried delivery accepted", func() bool {
		return count(t, k, "select count(*) from delivery_result where delivery_id = 2 and ok") == 1
	})
	mu.Lock()
	defer mu.Unlock()
	if accepted["2"] != 1 {
		t.Fatalf("accepted: %v", accepted)
	}
}
