// Package modules_test runs the "À éprouver" cases of SLICE end to end: real Postgres, object storage and
// fake accounting server, demo seed. Env: TEST_DATABASE_URL, TEST_S3 (endpoint, credentials of deploy/s3.json),
// TEST_ACCT_URL (a fresh cmd/fakeacct with ACCT_SECRET=acct-dev-secret).
package modules_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DorsazWilliam/sor-framework-poc/gen"
	"github.com/DorsazWilliam/sor-framework-poc/kernel"
	"github.com/DorsazWilliam/sor-framework-poc/modules/accounting"
	"github.com/DorsazWilliam/sor-framework-poc/modules/base"
	"github.com/DorsazWilliam/sor-framework-poc/modules/billing"
	timemod "github.com/DorsazWilliam/sor-framework-poc/modules/time"
	"github.com/DorsazWilliam/sor-framework-poc/seed"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const acctSecret = "acct-dev-secret"

// setup creates a database seeded with the demo data and boots the kernel of all modules on it, with the
// object storage and the accounting connection of the test environment.
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
	name := "sor_slice_" + strings.ToLower(t.Name())
	admin.Exec(ctx, "drop database if exists "+name+" with (force)")
	if _, err := admin.Exec(ctx, "create database "+name); err != nil {
		t.Fatal(err)
	}
	admin.Close(ctx)
	u, _ := neturl.Parse(url)
	u.Path = "/" + name
	if err := kernel.Configure(ctx, u.String()); err != nil {
		t.Fatal(err)
	}
	pool, err := kernel.NewPool(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s, err := kernel.Load(gen.SQLC, base.Module, timemod.Module, billing.Module, accounting.Module)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Conn().PgConn().Exec(ctx, "create extension if not exists btree_gist;\n"+s.Schema()).ReadAll()
	conn.Release()
	if err != nil {
		t.Fatal(err)
	}
	if err := kernel.EnsurePartitions(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := kernel.MigrateJobs(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := seed.Run(ctx, pool, "demo"); err != nil {
		t.Fatal(err)
	}
	if acct := os.Getenv("TEST_ACCT_URL"); acct != "" {
		pool.Exec(ctx, "update connection set endpoint = $1", acct)
	}
	t.Setenv("SECRET_accounting", acctSecret)
	k, err := kernel.Boot(ctx, pool, s)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint := os.Getenv("TEST_S3"); endpoint != "" {
		if err := k.OpenStorage(ctx, kernel.Storage{Endpoint: endpoint, PublicEndpoint: endpoint, Bucket: "slice",
			AccessKey: "sor", SecretKey: "sor-dev-secret"}); err != nil {
			t.Fatal(err)
		}
	}
	return k
}

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

func need(t *testing.T, envs ...string) {
	t.Helper()
	for _, e := range envs {
		if os.Getenv(e) == "" {
			t.Skip(e + " not set")
		}
	}
}

func who(t *testing.T, k *kernel.Kernel, subjects ...string) *kernel.Principal {
	t.Helper()
	p, err := k.Principal(context.Background(), subjects)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func call(t *testing.T, k *kernel.Kernel, p *kernel.Principal, name string, in any) *kernel.Result {
	t.Helper()
	res, err := callE(k, p, name, in)
	if err != nil {
		t.Fatalf("%s %v: %v", name, in, err)
	}
	return res
}

func callE(k *kernel.Kernel, p *kernel.Principal, name string, in any) (*kernel.Result, error) {
	body, _ := json.Marshal(in)
	return k.Call(context.Background(), p, name, body)
}

func problem(t *testing.T, err error, code string) *kernel.Problem {
	t.Helper()
	var pr *kernel.Problem
	if !errors.As(err, &pr) || pr.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
	return pr
}

// one scans a single row of sql.
func one(t *testing.T, k *kernel.Kernel, sql string, dst ...any) {
	t.Helper()
	if err := k.Pool.QueryRow(context.Background(), sql).Scan(dst...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(timeout); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timeout: %s", what)
}

func count(t *testing.T, k *kernel.Kernel, sql string) int {
	t.Helper()
	var n int
	one(t, k, sql, &n)
	return n
}

// hold makes the first commit of command wait until the returned release is called; held is closed when
// it waits.
func hold(k *kernel.Kernel, command string) (held chan struct{}, release func()) {
	held, gate := make(chan struct{}), make(chan struct{})
	first := true
	k.BeforeCommit = func(_ context.Context, _ *pgx.Batch, c *kernel.Commit) error {
		if c.Command == command && first {
			first = false
			close(held)
			<-gate
		}
		return nil
	}
	return held, func() { close(gate) }
}

// await waits until the held command waits at its commit; it fails if the command returned instead.
func await(t *testing.T, held chan struct{}, errc chan error) {
	t.Helper()
	select {
	case <-held:
	case err := <-errc:
		t.Fatalf("the held command returned: %v", err)
	}
}

// waiting asserts that the call in errc has not returned within 300 ms.
func waiting(t *testing.T, errc chan error, what string) {
	t.Helper()
	select {
	case err := <-errc:
		t.Fatalf("%s did not wait: %v", what, err)
	case <-time.After(300 * time.Millisecond):
	}
}

func async(k *kernel.Kernel, p *kernel.Principal, name string, in any) chan error {
	errc := make(chan error, 1)
	go func() {
		_, err := callE(k, p, name, in)
		errc <- err
	}()
	return errc
}

// editable returns an activity of the demo user that the user can edit, with a task billed always.
func editable(t *testing.T, k *kernel.Kernel) (id int64, date string, project int64) {
	t.Helper()
	one(t, k, `select a.id, a.date::text, a.project_id from activity_p a join task t on t.id = a.task_id
where a.user_id = 4 and a.not_locked and a.not_invoiced and a.not_validated and a.entry_open and a.task_allowed
  and t.billing = 'always' and a.start_tod + a.duration_min <= 1380
order by a.date desc, a.id limit 1`, &id, &date, &project)
	return
}

// (a) An activity written while its period is being locked: the writer waits for the lock, then reads not_locked false.
func TestActivityVersusPeriodLock(t *testing.T) {
	k := setup(t)
	user, admin := who(t, k, "user"), who(t, k, "admin")
	id, date, _ := editable(t, k)
	lock := map[string]any{"date_from": date, "date_to": date, "user_id": 4}

	held, release := hold(k, "period_lock.create")
	lockc := async(k, admin, "period_lock.create", lock)
	await(t, held, lockc)
	upd := async(k, user, "activity.update", map[string]any{"ids": []int64{id}, "description": "during the lock"})
	waiting(t, upd, "activity.update")
	release()
	if err := <-lockc; err != nil {
		t.Fatal(err)
	}
	if pr := problem(t, <-upd, "rejected"); !slices.Contains(pr.Rejected[0].Conditions, "not_locked") {
		t.Fatalf("rejection: %+v", pr.Rejected)
	}
}

// computed is the projection of activity id without its invoice line: rate, billed minutes and amount computed
// from its inputs.
func computed(t *testing.T, k *kernel.Kernel, id int64) string {
	t.Helper()
	tx, err := k.Pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	sid := strconv.FormatInt(id, 10)
	tx.Exec(context.Background(), "delete from invoice_expense where expense_id in (select id from expense where activity_id = "+sid+")")
	tx.Exec(context.Background(), "delete from invoice_line where activity_id = "+sid)
	var s string
	if err := tx.QueryRow(context.Background(), "select row(rate, billed_min, amount)::text from activity_p where id = "+sid).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// (b) An activity written while it is being invoiced: the invoice waits for the write, then its line carries the values
// of the committed inputs.
func TestActivityVersusInvoicing(t *testing.T) {
	k := setup(t)
	user, finance := who(t, k, "user"), who(t, k, "finance")
	invoiceOf := func(project int64, date string) int64 {
		return call(t, k, finance, "invoice.create", map[string]any{"project_id": project, "date_from": date, "date_to": date}).IDs[0]
	}

	id, date, project := editable(t, k)
	inv := invoiceOf(project, date)
	var dur int
	one(t, k, "select duration_min from activity where id = "+strconv.FormatInt(id, 10), &dur)
	held, release := hold(k, "activity.update")
	upd := async(k, user, "activity.update", map[string]any{"ids": []int64{id}, "duration_min": dur + 15})
	await(t, held, upd)
	mark := async(k, finance, "invoice.mark_invoiced", map[string]any{"ids": []int64{inv}})
	waiting(t, mark, "invoice.mark_invoiced")
	release()
	if err := <-upd; err != nil {
		t.Fatal(err)
	}
	if err := <-mark; err != nil {
		t.Fatal(err)
	}
	var line string
	var invoice int64
	one(t, k, "select row(rate, billed_min, amount)::text, invoice_id from invoice_line where activity_id = "+strconv.FormatInt(id, 10), &line, &invoice)
	if invoice != inv || line != computed(t, k, id) {
		t.Fatalf("line %s on invoice %d, computed %s on %d", line, invoice, computed(t, k, id), inv)
	}
	if !strings.Contains(line, ","+strconv.Itoa(dur+15)+",") {
		t.Fatalf("billed minutes of the line %s, want %d", line, dur+15)
	}

}

type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

// (d) A refusal lists the false conditions, identically over HTTP and MCP; (e) out of reach is not_found.
func TestRejectionAndReach(t *testing.T) {
	k := setup(t)
	h, _ := k.API(kernel.Config{Verify: func(_ context.Context, raw string) ([]string, error) { return strings.Split(raw, "+"), nil }})
	srv := httptest.NewServer(h)
	defer srv.Close()
	var invoiced, other int64
	one(t, k, "select min(a.id) from activity a join invoice_line l on l.activity_id = a.id where a.user_id = 4", &invoiced)
	one(t, k, "select min(id) from activity where user_id <> 4", &other)
	in := map[string]any{"ids": []int64{invoiced}, "description": "late"}

	body, _ := json.Marshal(in)
	req, _ := http.NewRequest("POST", srv.URL+"/api/c/activity.update", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer user")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	var viaHTTP map[string]any
	json.Unmarshal(raw, &viaHTTP)
	rej := viaHTTP["rejected"].([]any)[0].(map[string]any)
	conds := rej["conditions"].([]any)
	if res.StatusCode != 409 || viaHTTP["code"] != "rejected" || viaHTTP["action"] != "editable" ||
		!slices.Contains(conds, any("not_invoiced")) || rej["row"].(map[string]any)["id"].(float64) != float64(invoiced) {
		t.Fatalf("HTTP rejection: %d %s", res.StatusCode, raw)
	}

	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearer("user")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tr, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "activity.update", Arguments: in})
	if err != nil || !tr.IsError {
		t.Fatalf("MCP: %v %+v", err, tr)
	}
	var viaMCP map[string]any
	b, _ := json.Marshal(tr.StructuredContent)
	json.Unmarshal(b, &viaMCP)
	if !reflect.DeepEqual(viaHTTP, viaMCP) {
		t.Fatalf("HTTP and MCP bodies differ:\n%s\n%s", raw, b)
	}

	user := who(t, k, "user")
	_, err = callE(k, user, "activity.update", map[string]any{"ids": []int64{other}, "description": "not mine"})
	problem(t, err, "not_found")
	_, err = callE(k, user, "activity.create", map[string]any{"user_id": 6, "date": "2026-11-02", "start_tod": 480, "duration_min": 60,
		"folder_id": 1, "task_id": 1})
	problem(t, err, "not_found")
	out, err := k.View(context.Background(), user, "activity_grid", []byte(`{"params":{"date_from":"2020-01-01","date_to":"2030-01-01"},"group":["user_id"],"agg":[{"fn":"count"}]}`))
	if err != nil || !strings.HasPrefix(string(out), `[{"user_id":4,`) || strings.Count(string(out), "user_id") != 1 {
		t.Fatalf("grid of the user: %v %s", err, out)
	}
}

// (f) A cancelled run writes nothing: no invoice, no event.
func TestRunCancelled(t *testing.T) {
	k := setup(t)
	start(t, k)
	ctx := context.Background()
	finance := who(t, k, "finance")
	from := time.Now().AddDate(0, -1, 0)
	from = time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, -1)
	params := map[string]any{"date_from": from.Format(time.DateOnly), "date_to": to.Format(time.DateOnly)}
	body, _ := json.Marshal(map[string]any{"params": params})
	out, err := k.View(ctx, finance, "invoice_run_candidates", body)
	if err != nil {
		t.Fatal(err)
	}
	var cands []struct {
		ProjectID int64 `json:"project_id"`
	}
	json.Unmarshal(out, &cands)
	if len(cands) < 3 {
		t.Fatalf("candidates: %s", out)
	}
	invoices := count(t, k, "select count(*) from invoice")
	events := count(t, k, "select count(*) from event where command = 'invoice.create'")
	// The third project held: the run creates two invoices, then waits on the foreign key check.
	tx, err := k.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "select from project where id = $1 for update", strconv.FormatInt(cands[2].ProjectID, 10)); err != nil {
		t.Fatal(err)
	}
	run := *call(t, k, finance, "invoice.run", params).Run
	state := func() (s string, done int) {
		one(t, k, "select state::text, done from run where id = "+strconv.FormatInt(run, 10), &s, &done)
		return
	}
	eventually(t, 10*time.Second, "run waiting on the third root", func() bool { s, d := state(); return s == "running" && d == 2 })
	call(t, k, finance, "run.cancel", map[string]any{"ids": []int64{run}})
	tx.Rollback(ctx)
	eventually(t, 10*time.Second, "run cancelled", func() bool { s, _ := state(); return s == "cancelled" })
	if n := count(t, k, "select count(*) from invoice"); n != invoices {
		t.Fatalf("invoices %d, before %d", n, invoices)
	}
	if n := count(t, k, "select count(*) from event where command = 'invoice.create'"); n != events {
		t.Fatalf("events %d, before %d", n, events)
	}
}

func acct(t *testing.T, method, path, body string) []byte {
	t.Helper()
	req, _ := http.NewRequest(method, os.Getenv("TEST_ACCT_URL")+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+acctSecret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return b
}

// received returns the uploads the accounting system holds under idempotency key key.
func received(t *testing.T, key int64) []struct{ ContentType, Body string } {
	t.Helper()
	var inv struct {
		Items []struct {
			Key         string `json:"key"`
			ContentType string `json:"content_type"`
			Body        string `json:"body"`
		} `json:"items"`
	}
	json.Unmarshal(acct(t, "GET", "/invoices", ""), &inv)
	var out []struct{ ContentType, Body string }
	for _, i := range inv.Items {
		if i.Key == strconv.FormatInt(key, 10) {
			out = append(out, struct{ ContentType, Body string }{i.ContentType, i.Body})
		}
	}
	return out
}

// (g) Accounting down: the export stays pending with failed results, then its CSV is received exactly once.
func TestAccountingDown(t *testing.T) {
	need(t, "TEST_ACCT_URL")
	k := setup(t)
	finance := who(t, k, "finance")
	var inv int64
	one(t, k, "select min(id) from invoice where invoiced_at is not null", &inv)
	state := func() string {
		var s string
		one(t, k, "select export_state::text from invoice_p where id = "+strconv.FormatInt(inv, 10), &s)
		return s
	}
	acct(t, "POST", "/admin/fail", `{"on":true}`)
	defer acct(t, "POST", "/admin/fail", `{"on":false}`)
	start(t, k)
	delivery := call(t, k, finance, "invoice.export", map[string]any{"ids": []int64{inv}}).Changed[0].IDs[0]
	eventually(t, 10*time.Second, "failed attempt", func() bool { return state() == "failed" })
	acct(t, "POST", "/admin/fail", `{"on":false}`)
	eventually(t, 20*time.Second, "exported", func() bool { return state() == "exported" })
	if got := received(t, delivery); len(got) != 1 || got[0].ContentType != "text/csv; charset=utf-8" ||
		!strings.HasPrefix(got[0].Body, "invoice;account;project;date;user;task;description;minutes;rate;amount\n"+strconv.FormatInt(inv, 10)+";") {
		t.Fatalf("received: %+v", got)
	}
}

// (h) Printing twice gives the same PDF: same bytes, one blob, the same file row.
func TestPrint(t *testing.T) {
	need(t, "TEST_S3")
	if _, err := exec.LookPath("typst"); err != nil {
		t.Skip("typst not installed")
	}
	k := setup(t)
	start(t, k)
	finance := who(t, k, "finance")
	var inv int64
	one(t, k, "select min(id) from invoice where invoiced_at is not null", &inv)
	sid := strconv.FormatInt(inv, 10)
	call(t, k, finance, "invoice.print", map[string]any{"ids": []int64{inv}})
	var file int64
	eventually(t, 20*time.Second, "pdf attached", func() bool {
		return k.Pool.QueryRow(context.Background(), "select pdf_file_id from invoice where id = "+sid+" and pdf_file_id is not null").Scan(&file) == nil
	})
	var hash string
	one(t, k, "select hash from file where id = "+strconv.FormatInt(file, 10), &hash)
	call(t, k, finance, "invoice.print", map[string]any{"ids": []int64{inv}})
	eventually(t, 20*time.Second, "second render", func() bool {
		return count(t, k, "select count(*) from river.river_job where args->>'kind' = 'invoice.render' and state = 'completed'") == 2
	})
	var again int64
	one(t, k, "select pdf_file_id from invoice where id = "+sid, &again)
	if again != file || count(t, k, "select count(*) from file") != 1 {
		t.Fatalf("second print: file %d (first %d), %d files", again, file, count(t, k, "select count(*) from file"))
	}
	blob, err := k.Blob(context.Background(), hash)
	if err != nil {
		t.Fatal(err)
	}
	pdf, _ := io.ReadAll(blob)
	blob.Close()
	if !strings.HasPrefix(string(pdf), "%PDF") {
		t.Fatalf("not a PDF: %q", pdf[:min(len(pdf), 20)])
	}
}
