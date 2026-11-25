package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
)

// ReactionArgs are the args of a job inserted at the commit of a command: its roots and its call.
type ReactionArgs struct {
	IDs  []int64 `json:"ids"`
	Call string  `json:"call"`
}

// sorArgs is the single River job kind: every job of the registry travels as {kind, args, subjects}.
type sorArgs struct{ Job }

func (sorArgs) Kind() string { return "sor" }

// unique holds one job per args among the jobs not finished yet.
var unique = &river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending,
	rivertype.JobStateRetryable, rivertype.JobStateRunning, rivertype.JobStateScheduled}}

// MigrateJobs creates the river schema and migrates it to the version of the River library.
func MigrateJobs(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, "create schema if not exists river"); err != nil {
		return err
	}
	m, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Schema: "river"})
	if err != nil {
		return err
	}
	_, err = m.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}

// bootJobs builds the River client: one worker for every job kind, periodic jobs for Every.
func (b *booter) bootJobs(ctx context.Context) {
	b.jobs = map[string]*JobDecl{}
	var periodic []*river.PeriodicJob
	for _, j := range b.Jobs {
		if j.Kind == "" || b.jobs[j.Kind] != nil {
			b.fail("job %q: kind empty or declared twice", j.Kind)
			continue
		}
		b.jobs[j.Kind] = j
		if j.Every > 0 {
			params := b.jobParams(j.Kind, nil, nil, j.Unique)
			periodic = append(periodic, river.NewPeriodicJob(river.PeriodicInterval(j.Every),
				func() (river.JobArgs, *river.InsertOpts) { return params.Args, params.InsertOpts },
				&river.PeriodicJobOpts{RunOnStart: true}))
		}
	}
	for on, kinds := range mergedKeys(b.Reactions, b.OnInserts) {
		for _, kind := range kinds {
			if b.jobs[kind] == nil {
				b.fail("reaction on %s: job %s is not declared", on, kind)
			}
		}
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &worker{k: b.Kernel})
	driver := riverpgxv5.New(b.Pool)
	// reads the server capabilities now rather than in the first command that inserts a job
	if err := driver.GetExecutor().InitDriver(ctx); err != nil {
		b.fail("jobs: %v", err)
	}
	var err error
	b.river, err = river.NewClient(driver, &river.Config{
		Schema:       "river",
		Queues:       map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 20}},
		Workers:      workers,
		PeriodicJobs: periodic,
		MaxAttempts:  5,
		RetryPolicy:  backoff{},
	})
	if err != nil {
		b.fail("jobs: %v", err)
	}
}

func mergedKeys(ms ...map[string][]string) map[string][]string {
	out := map[string][]string{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = append(out[k], v...)
		}
	}
	return out
}

// jobParams builds the insertion of a job; As replaces the subjects.
func (k *Kernel) jobParams(kind string, args any, subjects []string, uniq bool) river.InsertManyParams {
	if as := k.jobs[kind].As; as != "" {
		subjects = []string{as}
	}
	raw, _ := json.Marshal(args)
	opts := &river.InsertOpts{}
	if uniq {
		opts.UniqueOpts = *unique
	}
	return river.InsertManyParams{Args: sorArgs{Job{Kind: kind, Args: raw, Subjects: subjects}}, InsertOpts: opts}
}

// Start runs the job workers and the periodic jobs until Stop.
func (k *Kernel) Start(ctx context.Context) error { return k.river.Start(ctx) }

// Stop waits for the running jobs and stops the workers.
func (k *Kernel) Stop(ctx context.Context) error { return k.river.Stop(ctx) }

type worker struct {
	river.WorkerDefaults[sorArgs]
	k *Kernel
}

func (w *worker) Work(ctx context.Context, j *river.Job[sorArgs]) error {
	job := j.Args.Job
	d := w.k.jobs[job.Kind]
	if d == nil {
		return river.JobCancel(fmt.Errorf("unknown job kind %s", job.Kind))
	}
	if len(job.Subjects) > 0 {
		p, err := w.k.Principal(ctx, job.Subjects)
		var pr *Problem
		if errors.As(err, &pr) {
			return river.JobCancel(err)
		}
		if err != nil {
			return err
		}
		job.Principal = p
	}
	return d.Fn(ctx, w.k, job)
}

// backoff retries after 1 s, 2 s, 4 s, 8 s.
type backoff struct{}

func (backoff) NextRetry(j *rivertype.JobRow) time.Time {
	return time.Now().Add(time.Second << min(len(j.Errors), 10))
}

// batchTx lets River insert jobs on a connection whose transaction the kernel drives: queries run now,
// statements without result (the insert notification) join the commit batch.
type batchTx struct {
	pgx.Tx
	conn *pgx.Conn
	b    *pgx.Batch
}

func (t batchTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.b.Queue(sql, args...)
	return pgconn.CommandTag{}, nil
}

func (t batchTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return t.conn.Query(ctx, sql, args...)
}

func (t batchTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return t.conn.QueryRow(ctx, sql, args...)
}

func (t batchTx) Conn() *pgx.Conn { return t.conn }
