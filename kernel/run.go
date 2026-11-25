package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// runRejection is a root of a run refused by its command; the run went on.
type runRejection struct {
	Input      json.RawMessage `json:"input"`
	Code       string          `json:"code"`
	ID         int64           `json:"id,omitempty"`
	Conditions []string        `json:"conditions"`
	Row        json.RawMessage `json:"row,omitempty"`
	Constraint string          `json:"constraint,omitempty"`
}

// runner tracks a run's progress, written on connections of its own while its transaction is open.
type runner struct {
	k        *Kernel
	id       int64
	call     string // the run job's call id: its events, its progress and its commit
	live     string // live key of the run row
	mu       sync.Mutex
	done     int
	rejected []runRejection
	cancel   bool
}

// runExecute is the job of a run (SPEC §10): one transaction, a savepoint per candidate root executing
// the per-root command; a refused root is recorded and the run goes on; run.cancel rolls everything back.
func runExecute(ctx context.Context, k *Kernel, job Job) error {
	var a ReactionArgs
	if err := json.Unmarshal(job.Args, &a); err != nil || len(a.IDs) != 1 {
		return river.JobCancel(errors.New("run.execute: bad args"))
	}
	var name, state string
	var input []byte
	var owner int64
	if err := k.Pool.QueryRow(ctx, "select name, input, state, owner_id from run where id = $1", a.IDs[0]).Scan(&name, &input, &state, &owner); err != nil {
		return err
	}
	if state != "queued" && state != "running" {
		return nil
	}
	r := &runner{k: k, id: a.IDs[0], call: uuid()}
	r.live = livePayload("run", []int64{r.id}, []int64{owner}, name, r.call)
	c := k.commands[name]
	conn, err := k.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	err = r.execute(ctx, conn, job.Principal, c, input)
	if err != nil {
		conn.Exec(context.WithoutCancel(ctx), "rollback")
		return r.set(ctx, "state = 'failed', error = $3, finished_at = sor_now()", err.Error())
	}
	return nil
}

func (r *runner) execute(ctx context.Context, conn *pgxpool.Conn, p *Principal, c *Command, input []byte) error {
	if _, err := conn.Exec(ctx, "begin"); err != nil {
		return err
	}
	// every candidate, page after page
	var candidates []json.RawMessage
	for page := []json.RawMessage(nil); len(candidates)%maxLimit == 0; candidates = append(candidates, page...) {
		body, _ := json.Marshal(map[string]any{"params": json.RawMessage(input), "limit": maxLimit, "offset": len(candidates)})
		raw, err := r.k.view(ctx, conn, p, c.run.Candidates, body)
		if err == nil {
			err = json.Unmarshal(raw, &page)
		}
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
	}
	if err := r.set(ctx, "state = 'running', total = $3", strconv.Itoa(len(candidates))); err != nil {
		return err
	}
	stop := make(chan struct{})
	ticked := make(chan error, 1)
	go func() {
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				ticked <- r.flush(ctx)
				return
			case <-tick.C:
				if err := r.flush(ctx); err != nil {
					ticked <- err
					<-stop
					return
				}
			}
		}
	}()
	var rows []Row
	anchors := map[string][]int64{}
	var jobs []river.InsertManyParams
	for _, cand := range candidates {
		r.mu.Lock()
		cancelled := r.cancel
		r.mu.Unlock()
		if cancelled {
			break
		}
		x, err := r.k.newCall(p, c.run.PerRoot, cand, r.call)
		if err == nil {
			x.conn = conn
			if _, err = x.exec(ctx, "savepoint root"); err == nil {
				_, err = conn.Exec(ctx, "release savepoint root")
			} else if _, rerr := conn.Exec(ctx, "rollback to savepoint root"); rerr != nil {
				err = rerr
			}
		}
		if err := r.record(cand, pgProblem(err)); err != nil {
			close(stop)
			<-ticked
			return err
		}
		if err == nil {
			rows, jobs = append(rows, x.rows...), append(jobs, x.jobs...)
			for t, a := range x.anchors {
				anchors[t] = uniq(append(anchors[t], a...))
			}
		}
	}
	close(stop)
	if err := <-ticked; err != nil {
		return err
	}
	if r.cancel {
		if _, err := conn.Exec(ctx, "rollback"); err != nil {
			return err
		}
		return r.set(ctx, "state = 'cancelled', finished_at = sor_now()")
	}
	err := r.k.commit(ctx, conn, c.Name, r.call, rows, anchors, jobs)
	if err != nil {
		return err
	}
	return r.set(ctx, "state = 'done', finished_at = sor_now()")
}

// record counts a processed root; a refused root is recorded with its false conditions, any other error
// fails the run.
func (r *runner) record(cand json.RawMessage, err error) error {
	var pr *Problem
	if err != nil && !errors.As(err, &pr) {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done++
	switch {
	case pr == nil:
	case len(pr.Rejected) == 0:
		r.rejected = append(r.rejected, runRejection{Input: cand, Code: pr.Code, Conditions: []string{}, Constraint: pr.Constraint})
	default:
		for _, rj := range pr.Rejected {
			r.rejected = append(r.rejected, runRejection{Input: cand, Code: pr.Code, ID: rj.ID, Conditions: rj.Conditions, Row: rj.Row})
		}
	}
	return nil
}

// flush writes done and rejected and reads cancel_requested.
func (r *runner) flush(ctx context.Context) error {
	r.mu.Lock()
	done, rejected := r.done, r.rejected
	r.mu.Unlock()
	if rejected == nil {
		rejected = []runRejection{}
	}
	rj, _ := json.Marshal(rejected)
	var cancel bool
	err := r.k.Pool.QueryRow(ctx, `with u as (update run set done = $3, rejected = $4::jsonb where id = $1 returning cancel_requested)
select u.cancel_requested from u, pg_notify('sor_live', $2) n`, r.id, r.live, strconv.Itoa(done), string(rj)).Scan(&cancel)
	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()
	return err
}

// set updates the run row outside the run's transaction, with a live key.
func (r *runner) set(ctx context.Context, assign string, args ...any) error {
	_, err := r.k.Pool.Exec(context.WithoutCancel(ctx), "with u as (update run set "+assign+" where id = $1 returning id) select from u, pg_notify('sor_live', $2) n",
		append([]any{r.id, r.live}, args...)...)
	return err
}
