// Command sor is the system-of-record binary.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	sor "github.com/DorsazWilliam/sor-framework-poc"
	"github.com/DorsazWilliam/sor-framework-poc/gen"
	"github.com/DorsazWilliam/sor-framework-poc/kernel"
	"github.com/DorsazWilliam/sor-framework-poc/seed"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2/clientcredentials"
)

const usage = `usage: sor <command>
  up                 migrate, seed $SOR_SEED (default demo) if the domain is empty, serve
  serve              serve HTTP on $LISTEN_ADDR (default :8080)
  migrate            migrate the river schema, apply the module schemas with pgschema, create event partitions
  seed demo|perf     load the synthetic dataset
  check [--static]   check the registry (--static: manifest and declarations only, no database)
  openapi            write gen/openapi.json
  call <command> '<json>' | view <view> '<json>' | meta
                     HTTP client ($SOR_URL, $SOR_TOKEN_URL, $SOR_CLIENT_ID)`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "check":
		if len(args) > 1 && args[1] == "--static" {
			return checkStatic()
		}
		if err := checkStatic(); err != nil {
			return err
		}
		_, _, err := boot(ctx)
		return err
	case "serve":
		return serve(ctx)
	case "migrate":
		return migrate(ctx)
	case "seed":
		if len(args) < 2 {
			return errors.New(usage)
		}
		pool, err := kernel.NewPool(ctx, os.Getenv("DATABASE_URL"))
		if err != nil {
			return err
		}
		defer pool.Close()
		return seed.Run(ctx, pool, args[1])
	case "up":
		if err := migrate(ctx); err != nil {
			return err
		}
		if err := seedIfEmpty(ctx); err != nil {
			return err
		}
		return serve(ctx)
	case "openapi":
		k, pool, err := boot(ctx)
		if err != nil {
			return err
		}
		defer pool.Close()
		_, api := k.API(kernel.Config{})
		b, err := json.MarshalIndent(api.OpenAPI(), "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join("gen", "openapi.json"), append(b, '\n'), 0o644)
	case "call", "view":
		if len(args) < 2 {
			return errors.New(usage)
		}
		body := "{}"
		if len(args) > 2 {
			body = args[2]
		}
		path := map[string]string{"call": "/api/c/", "view": "/api/v/"}[args[0]] + args[1]
		return client(ctx, http.MethodPost, path, body)
	case "meta":
		return client(ctx, http.MethodGet, "/api/meta", "")
	}
	return errors.New(usage)
}

func load() (*kernel.Static, error) { return kernel.Load(gen.SQLC, modules...) }

func checkStatic() error {
	s, err := load()
	if err != nil {
		return err
	}
	var labels map[string]string
	if sor.Labels != nil {
		if b, err := fs.ReadFile(sor.Labels, "fr.json"); err == nil {
			if err := json.Unmarshal(b, &labels); err != nil {
				return fmt.Errorf("labels/fr.json: %w", err)
			}
		}
	}
	if p := s.CheckStatic(labels); len(p) > 0 {
		return errors.New(strings.Join(p, "\n"))
	}
	return nil
}

func boot(ctx context.Context) (*kernel.Kernel, *pgxpool.Pool, error) {
	s, err := load()
	if err != nil {
		return nil, nil, err
	}
	pool, err := kernel.NewPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, nil, err
	}
	k, err := kernel.Boot(ctx, pool, s)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return k, pool, nil
}

func serve(ctx context.Context) error {
	k, pool, err := boot(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := k.OpenStorage(ctx, kernel.Storage{Endpoint: os.Getenv("S3_ENDPOINT"), PublicEndpoint: os.Getenv("S3_PUBLIC_ENDPOINT"),
		Bucket: os.Getenv("S3_BUCKET"), AccessKey: os.Getenv("S3_ACCESS_KEY"), SecretKey: os.Getenv("S3_SECRET_KEY")}); err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	if err := k.Start(ctx); err != nil {
		return err
	}
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		k.Stop(stop)
	}()
	go k.Listen(ctx)
	h, _ := k.API(kernel.Config{Issuer: os.Getenv("OIDC_ISSUER"), Dial: os.Getenv("OIDC_DIAL"), ClientID: os.Getenv("OIDC_CLIENT_ID"),
		PublicURL: os.Getenv("PUBLIC_URL"), Labels: sor.Labels, UI: uiFS()})
	addr := env("LISTEN_ADDR", ":8080")
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shut)
	}()
	slog.Info("serving", "addr", addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// migrate creates btree_gist, migrates the river schema, creates the plan database, applies the concatenated schemas with pgschema,
// then creates the event partitions.
func migrate(ctx context.Context) error {
	s, err := load()
	if err != nil {
		return err
	}
	url := os.Getenv("DATABASE_URL")
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return err
	}
	pool, err := kernel.NewPool(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, "create extension if not exists btree_gist"); err != nil {
		return err
	}
	if err := kernel.MigrateJobs(ctx, pool); err != nil {
		return fmt.Errorf("river: %w", err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "select exists(select from pg_database where datname = 'sorplan')").Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err := pool.Exec(ctx, "create database sorplan"); err != nil {
			return err
		}
	}
	plan := cfg.Copy()
	plan.Database = "sorplan"
	pc, err := pgx.ConnectConfig(ctx, plan)
	if err != nil {
		return err
	}
	_, err = pc.Exec(ctx, "create extension if not exists btree_gist")
	pc.Close(ctx)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "sor-schema-*.sql")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(s.Schema()); err != nil {
		return err
	}
	f.Close()
	port := strconv.Itoa(int(cfg.Port))
	cmd := exec.CommandContext(ctx, "pgschema", "apply",
		"--host", cfg.Host, "--port", port, "--db", cfg.Database, "--user", cfg.User, "--password", cfg.Password, "--sslmode", "disable",
		"--schema", "public", "--file", f.Name(),
		"--plan-host", cfg.Host, "--plan-port", port, "--plan-db", "sorplan", "--plan-user", cfg.User, "--plan-password", cfg.Password,
		"--plan-sslmode", "disable", "--auto-approve")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pgschema apply: %w", err)
	}
	return kernel.EnsurePartitions(ctx, pool)
}

func seedIfEmpty(ctx context.Context) error {
	s, err := load()
	if err != nil {
		return err
	}
	pool, err := kernel.NewPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	empty, err := s.DomainEmpty(ctx, pool)
	if err != nil || !empty {
		return err
	}
	size := env("SOR_SEED", "demo")
	slog.Info("seeding", "size", size)
	return seed.Run(ctx, pool, size)
}

// client calls the server with a client-credentials token and prints the response.
func client(ctx context.Context, method, path, body string) error {
	cc := clientcredentials.Config{ClientID: os.Getenv("SOR_CLIENT_ID"), ClientSecret: env("SOR_CLIENT_SECRET", "secret"),
		TokenURL: os.Getenv("SOR_TOKEN_URL")}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(env("SOR_URL", "http://localhost:8080"), "/")+path, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := cc.Client(ctx).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	if res.StatusCode >= 400 {
		return fmt.Errorf("%s", res.Status)
	}
	return nil
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
