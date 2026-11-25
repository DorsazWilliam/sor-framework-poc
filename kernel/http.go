package kernel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"golang.org/x/oauth2"
)

// Config is the HTTP surface configuration.
type Config struct {
	Issuer    string // OIDC issuer, one URL for browser and app
	Dial      string // host:port the app dials for the issuer host, if it differs
	ClientID  string
	PublicURL string
	Labels    fs.FS // <lang>.json
	UI        fs.FS // the SPA; index.html is the fallback
	// Verify replaces the OIDC verification of a raw token; it returns [sub] or [sub, act_for].
	Verify func(ctx context.Context, raw string) ([]string, error)
}

const sessionCookie = "sor_session"

// API builds the HTTP handler and the OpenAPI document: one operation per view and per command.
func (k *Kernel) API(cfg Config) (http.Handler, huma.API) {
	mux := http.NewServeMux()
	hc := huma.DefaultConfig("sor", "0.1.0")
	hc.DocsPath, hc.SchemasPath, hc.CreateHooks = "", "", nil
	hc.Components.Schemas = schemaRegistry
	api := humago.New(mux, hc)
	schema := func(v any) *huma.Schema { return schemaRegistry.Schema(reflect.TypeOf(v), true, "") }
	problemResp := &huma.Response{Description: "Error", Content: map[string]*huma.MediaType{"application/problem+json": {Schema: schema(Problem{})}}}
	register := func(op *huma.Operation, serve func(w http.ResponseWriter, r *http.Request)) {
		op.Responses["default"] = problemResp
		api.OpenAPI().AddOperation(op)
		api.Adapter().Handle(op, api.Middlewares().Handler(func(ctx huma.Context) {
			r, w := humago.Unwrap(ctx)
			serve(w, r)
		}))
	}
	for _, v := range k.Meta.Views {
		register(&huma.Operation{OperationID: "view_" + v.Name, Method: http.MethodPost, Path: "/api/v/" + v.Name, Summary: v.Name,
			Description: v.Description, Tags: []string{"views"},
			RequestBody: &huma.RequestBody{Content: map[string]*huma.MediaType{"application/json": {Schema: v.in}}},
			Responses:   map[string]*huma.Response{"200": {Description: "Rows", Content: map[string]*huma.MediaType{"application/json": {Schema: v.out}}}},
		}, func(w http.ResponseWriter, r *http.Request) {
			k.serve(w, r, "view "+v.Name, func(p *Principal, body []byte) (any, error) {
				out, err := k.View(r.Context(), p, v.Name, body)
				return json.RawMessage(out), err
			})
		})
	}
	for _, c := range k.Meta.Commands {
		register(&huma.Operation{OperationID: "command_" + c.Name, Method: http.MethodPost, Path: "/api/c/" + c.Name, Summary: c.Name,
			Description: c.Description, Tags: []string{"commands"},
			RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{"application/json": {Schema: c.in}}},
			Responses:   map[string]*huma.Response{"200": {Description: "Changed rows", Content: map[string]*huma.MediaType{"application/json": {Schema: schema(Result{})}}}},
		}, func(w http.ResponseWriter, r *http.Request) {
			k.serve(w, r, "command "+c.Name, func(p *Principal, body []byte) (any, error) { return k.Call(r.Context(), p, c.Name, body) })
		})
	}
	a := &authn{cfg: cfg}
	k.verify = a.verify
	for path, get := range map[string]func(p *Principal) any{"me": func(p *Principal) any { return p.Me() }, "meta": func(p *Principal) any { return k.Permitted(p) }} {
		register(&huma.Operation{OperationID: path, Method: http.MethodGet, Path: "/api/" + path, Summary: path, Tags: []string{"registry"},
			Responses: map[string]*huma.Response{"200": {Description: path, Content: map[string]*huma.MediaType{"application/json": {Schema: schema(get(&Principal{}))}}}},
		}, func(w http.ResponseWriter, r *http.Request) {
			k.serve(w, r, path, func(p *Principal, _ []byte) (any, error) { return get(p), nil })
		})
	}
	mux.HandleFunc("GET /api/labels/{lang}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		lang := r.PathValue("lang")
		if cfg.Labels != nil && regexp.MustCompile(`^[a-z]{2}$`).MatchString(lang) {
			if b, err := fs.ReadFile(cfg.Labels, lang+".json"); err == nil {
				w.Write(b)
				return
			}
		}
		w.Write([]byte("{}"))
	})
	mux.HandleFunc("GET /api/live", k.live)
	k.fileRoutes(mux)
	mux.Handle("/mcp", k.mcpHandler())
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, problem(404, "not_found", r.URL.Path))
	})
	mux.HandleFunc("GET /auth/login", a.login)
	mux.HandleFunc("GET /auth/callback", a.callback)
	mux.HandleFunc("GET /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, "/", http.StatusFound)
	})
	mux.Handle("/", spa(cfg.UI))
	return observe(mux), api
}

// serve authenticates, reads the body and writes the JSON result or the problem.
func (k *Kernel) serve(w http.ResponseWriter, r *http.Request, name string, run func(p *Principal, body []byte) (any, error)) {
	info(r.Context()).name = name
	p, err := k.authenticate(r)
	if err != nil {
		writeProblem(w, err)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		writeProblem(w, problem(422, "invalid", err.Error()))
		return
	}
	out, err := run(p, body)
	if err != nil {
		writeProblem(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if raw, ok := out.(json.RawMessage); ok {
		w.Write(raw) // produced by Postgres, passed through untouched
		return
	}
	json.NewEncoder(w).Encode(out)
}

func (k *Kernel) authenticate(r *http.Request) (*Principal, error) {
	raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if c, err := r.Cookie(sessionCookie); err == nil && raw == "" {
		raw = c.Value
	}
	if raw == "" {
		return nil, problem(401, "unauthenticated", "")
	}
	subjects, err := k.verify(r.Context(), raw)
	if err != nil {
		return nil, problem(401, "unauthenticated", err.Error())
	}
	p, err := k.Principal(r.Context(), subjects)
	if err == nil {
		info(r.Context()).principal = p.ID
	}
	return p, err
}

// asProblem returns the problem body of err; an error the kernel did not classify is logged and internal.
func asProblem(err error) *Problem {
	var p *Problem
	if !errors.As(err, &p) {
		slog.Error("request failed", "err", err)
		p = &Problem{Status: 500, Title: "Internal Server Error", Code: "internal"}
	}
	return p
}

func writeProblem(w http.ResponseWriter, err error) {
	p := asProblem(err)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	json.NewEncoder(w).Encode(p)
}

func (k *Kernel) live(w http.ResponseWriter, r *http.Request) {
	info(r.Context()).name = "live"
	p, err := k.authenticate(r)
	if err != nil {
		writeProblem(w, err)
		return
	}
	ch, cancel := k.Subscribe(p)
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	rc := http.NewResponseController(w)
	io.WriteString(w, ": live\n\n")
	rc.Flush()
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
		case <-tick.C:
			io.WriteString(w, ": ping\n\n")
		}
		if rc.Flush() != nil {
			return
		}
	}
}

func spa(ui fs.FS) http.Handler {
	if ui == nil {
		return http.NotFoundHandler()
	}
	files := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if _, err := fs.Stat(ui, name); name == "" || err != nil {
			b, err := fs.ReadFile(ui, "index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(b)
			return
		}
		files.ServeHTTP(w, r)
	})
}

// Observation: Server-Timing header and one slog line per request.

type reqInfo struct {
	name      string
	principal int64
}

type infoKey struct{}

func info(ctx context.Context) *reqInfo {
	if i, ok := ctx.Value(infoKey{}).(*reqInfo); ok {
		return i
	}
	return &reqInfo{}
}

type timedWriter struct {
	http.ResponseWriter
	t      *Timing
	start  time.Time
	status int
}

func (w *timedWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.Header().Set("Server-Timing", fmt.Sprintf(`db;dur=%.1f;desc="%d rt", app;dur=%.1f`,
			float64(w.t.DB.Load())/1e6, w.t.RT.Load(), float64(time.Since(w.start).Microseconds())/1e3))
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *timedWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *timedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, t := WithTiming(r.Context())
		in := &reqInfo{name: r.Method + " " + r.URL.Path}
		tw := &timedWriter{ResponseWriter: w, t: t, start: time.Now()}
		next.ServeHTTP(tw, r.WithContext(context.WithValue(ctx, infoKey{}, in)))
		slog.Info("request", "name", in.name, "principal", in.principal, "status", tw.status,
			"ms", float64(time.Since(tw.start).Microseconds())/1e3, "rt", t.RT.Load())
	})
}

// authn verifies tokens and runs the browser login (authorization code + PKCE, server side).
type authn struct {
	cfg      Config
	mu       sync.Mutex
	client   *http.Client
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
}

func (a *authn) init(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.verifier != nil {
		return nil
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if a.cfg.Dial != "" {
		u, err := url.Parse(a.cfg.Issuer)
		if err != nil {
			return err
		}
		d := &net.Dialer{Timeout: 5 * time.Second}
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == u.Host {
				addr = a.cfg.Dial
			}
			return d.DialContext(ctx, network, addr)
		}
	}
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), a.cfg.Issuer)
	if err != nil {
		return err
	}
	a.client = client
	a.verifier = provider.Verifier(&oidc.Config{SkipClientIDCheck: true})
	a.oauth = &oauth2.Config{ClientID: a.cfg.ClientID, Endpoint: provider.Endpoint(),
		RedirectURL: strings.TrimSuffix(a.cfg.PublicURL, "/") + "/auth/callback", Scopes: []string{oidc.ScopeOpenID}}
	return nil
}

func (a *authn) verify(ctx context.Context, raw string) ([]string, error) {
	if a.cfg.Verify != nil {
		return a.cfg.Verify(ctx, raw)
	}
	if err := a.init(ctx); err != nil {
		return nil, err
	}
	tok, err := a.verifier.Verify(oidc.ClientContext(ctx, a.client), raw)
	if err != nil {
		return nil, err
	}
	var claims struct {
		ActFor string `json:"act_for"`
	}
	if err := tok.Claims(&claims); err != nil {
		return nil, err
	}
	if claims.ActFor != "" {
		return []string{tok.Subject, claims.ActFor}, nil
	}
	return []string{tok.Subject}, nil
}

func (a *authn) login(w http.ResponseWriter, r *http.Request) {
	if err := a.init(r.Context()); err != nil {
		writeProblem(w, err)
		return
	}
	state, verifier := randomString(), oauth2.GenerateVerifier()
	for name, v := range map[string]string{"sor_state": state, "sor_pkce": verifier} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: v, Path: "/auth/", MaxAge: 600, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	}
	http.Redirect(w, r, a.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (a *authn) callback(w http.ResponseWriter, r *http.Request) {
	state, err1 := r.Cookie("sor_state")
	verifier, err2 := r.Cookie("sor_pkce")
	if err := errors.Join(err1, err2, a.init(r.Context())); err != nil || state.Value != r.URL.Query().Get("state") {
		writeProblem(w, problem(401, "unauthenticated", "invalid login state"))
		return
	}
	ctx := oidc.ClientContext(r.Context(), a.client)
	tok, err := a.oauth.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(verifier.Value))
	if err != nil {
		writeProblem(w, problem(401, "unauthenticated", err.Error()))
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	id, err := a.verifier.Verify(ctx, raw)
	if err != nil {
		writeProblem(w, problem(401, "unauthenticated", err.Error()))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: raw, Path: "/", Expires: id.Expiry, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	for _, name := range []string{"sor_state", "sor_pkce"} {
		http.SetCookie(w, &http.Cookie{Name: name, Path: "/auth/", MaxAge: -1})
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func randomString() string {
	b := make([]byte, 24)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
