// Command fakeacct is an in-memory accounting server for local development:
// cursor-paged accounts and projects, idempotent invoice intake, and admin
// switches to simulate outages and upstream changes.
package main

import (
	"cmp"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"sync"
)

type account struct {
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	Address    string `json:"address"`
	Revision   int64  `json:"revision"`
}

type project struct {
	ExternalID        string `json:"external_id"`
	AccountExternalID string `json:"account_external_id"`
	Name              string `json:"name"`
	Revision          int64  `json:"revision"`
}

type invoice struct {
	ID       int             `json:"id"`
	Key      string          `json:"key"`
	Received int             `json:"received"`
	Body     json.RawMessage `json:"body"`
}

type store struct {
	mu       sync.Mutex
	secret   string
	fail     bool
	revision int64
	accounts map[string]account
	projects map[string]project
	invoices []*invoice
	byKey    map[string]*invoice
}

func newStore(secret string) *store {
	s := &store{secret: secret, accounts: map[string]account{}, projects: map[string]project{}, invoices: []*invoice{}, byKey: map[string]*invoice{}}
	s.seed()
	return s
}

func (s *store) putAccount(a account) {
	s.revision++
	a.Revision = s.revision
	s.accounts[a.ExternalID] = a
}

func (s *store) putProject(p project) {
	s.revision++
	p.Revision = s.revision
	s.projects[p.ExternalID] = p
}

// page returns up to limit items changed after since, oldest change first, and the cursor of the last one.
func page[T any](m map[string]T, rev func(T) int64, since int64, limit int) ([]T, int64) {
	items := []T{}
	for _, v := range m {
		if rev(v) > since {
			items = append(items, v)
		}
	}
	slices.SortFunc(items, func(a, b T) int { return cmp.Compare(rev(a), rev(b)) })
	if len(items) > limit {
		items = items[:limit]
	}
	if len(items) > 0 {
		since = rev(items[len(items)-1])
	}
	return items, since
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// guarded serves a non-admin endpoint: outage switch, then bearer secret, under the store lock.
func (s *store) guarded(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		switch {
		case s.fail:
			fail(w, http.StatusServiceUnavailable, "unavailable")
		case r.Header.Get("Authorization") != "Bearer "+s.secret:
			fail(w, http.StatusUnauthorized, "unauthorized")
		default:
			h(w, r)
		}
	}
}

func (s *store) admin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		h(w, r)
	}
}

func feed[T any](m func() map[string]T, rev func(T) int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		since, limit := int64(0), 500
		var err error
		if v := r.URL.Query().Get("since"); v != "" {
			if since, err = strconv.ParseInt(v, 10, 64); err != nil || since < 0 {
				fail(w, http.StatusBadRequest, "invalid since")
				return
			}
		}
		if v := r.URL.Query().Get("limit"); v != "" {
			if limit, err = strconv.Atoi(v); err != nil || limit < 1 {
				fail(w, http.StatusBadRequest, "invalid limit")
				return
			}
		}
		items, cursor := page(m(), rev, since, limit)
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "cursor": strconv.FormatInt(cursor, 10)})
	}
}

func (s *store) postInvoice(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		fail(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	var body json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	inv, ok := s.byKey[key]
	if !ok {
		inv = &invoice{ID: len(s.invoices) + 1, Key: key, Body: body}
		s.invoices = append(s.invoices, inv)
		s.byKey[key] = inv
	}
	inv.Received++
	writeJSON(w, http.StatusOK, map[string]any{"id": inv.ID, "key": inv.Key})
}

func (s *store) postFail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	s.fail = in.On
	writeJSON(w, http.StatusOK, map[string]bool{"fail": s.fail})
}

func (s *store) postAccount(w http.ResponseWriter, r *http.Request) {
	var a account
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil || a.ExternalID == "" {
		fail(w, http.StatusBadRequest, "external_id required")
		return
	}
	s.putAccount(a)
	writeJSON(w, http.StatusOK, s.accounts[a.ExternalID])
}

func (s *store) state(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"fail": s.fail, "revision": s.revision, "accounts": len(s.accounts), "projects": len(s.projects), "invoices": len(s.invoices)})
}

func (s *store) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /accounts", s.guarded(feed(func() map[string]account { return s.accounts }, func(a account) int64 { return a.Revision })))
	mux.HandleFunc("GET /projects", s.guarded(feed(func() map[string]project { return s.projects }, func(p project) int64 { return p.Revision })))
	mux.HandleFunc("POST /invoices", s.guarded(s.postInvoice))
	mux.HandleFunc("GET /invoices", s.guarded(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"items": s.invoices})
	}))
	mux.HandleFunc("POST /admin/fail", s.admin(s.postFail))
	mux.HandleFunc("POST /admin/accounts", s.admin(s.postAccount))
	mux.HandleFunc("GET /admin/state", s.admin(s.state))
	return mux
}

func main() {
	secret := os.Getenv("ACCT_SECRET")
	if secret == "" {
		log.Fatal("ACCT_SECRET is required")
	}
	addr := cmp.Or(os.Getenv("LISTEN_ADDR"), ":8070")
	log.Printf("fakeacct listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, newStore(secret).handler()))
}
