package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const secret = "test-secret"

type step struct {
	name   string
	method string
	path   string
	body   string
	header map[string]string
	status int
	check  func(t *testing.T, got map[string]any)
}

var bearer = map[string]string{"Authorization": "Bearer " + secret}

func withKey(k string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + secret, "Idempotency-Key": k}
}

func csv(k string) map[string]string {
	h := withKey(k)
	h["Content-Type"] = "text/csv; charset=utf-8"
	return h
}

func call(h http.Handler, method, path, body string, header map[string]string) (int, map[string]any) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	return rec.Code, got
}

func items(got map[string]any) []map[string]any {
	var out []map[string]any
	for _, v := range got["items"].([]any) {
		out = append(out, v.(map[string]any))
	}
	return out
}

func ids(got map[string]any) []string {
	var out []string
	for _, it := range items(got) {
		out = append(out, it["external_id"].(string))
	}
	return out
}

func expect(want ...any) func(*testing.T, map[string]any) {
	return func(t *testing.T, got map[string]any) {
		for i := 0; i < len(want); i += 2 {
			if g := got[want[i].(string)]; !reflect.DeepEqual(g, want[i+1]) {
				t.Errorf("%s = %v, want %v", want[i], g, want[i+1])
			}
		}
	}
}

func TestScenario(t *testing.T) {
	h := newStore(secret).handler()
	steps := []step{
		{name: "no bearer", method: "GET", path: "/accounts", status: 401},
		{name: "wrong bearer", method: "GET", path: "/accounts", header: map[string]string{"Authorization": "Bearer nope"}, status: 401},
		{name: "bad since", method: "GET", path: "/accounts?since=x", header: bearer, status: 400},
		{name: "bad limit", method: "GET", path: "/accounts?limit=0", header: bearer, status: 400},
		{name: "all accounts", method: "GET", path: "/accounts?limit=1000", header: bearer, status: 200, check: func(t *testing.T, got map[string]any) {
			it := items(got)
			if len(it) != 305 || it[0]["external_id"] != "A0001" || it[0]["revision"] != 1.0 || it[304]["external_id"] != "A0305" {
				t.Fatalf("accounts: %d items, first %v, last %v", len(it), it[0], it[len(it)-1])
			}
			expect("cursor", "305")(t, got)
		}},
		{name: "default limit", method: "GET", path: "/projects", header: bearer, status: 200, check: func(t *testing.T, got map[string]any) {
			if it := items(got); len(it) != 500 || it[5]["account_external_id"] != "A0002" {
				t.Fatalf("projects page: %d items, sixth %v", len(it), it[5])
			}
		}},
		{name: "extra projects", method: "GET", path: "/projects?since=1805", header: bearer, status: 200, check: func(t *testing.T, got map[string]any) {
			it := items(got)
			if len(it) != 10 || it[0]["external_id"] != "P01501" || it[0]["account_external_id"] != "A0301" || it[9]["account_external_id"] != "A0305" {
				t.Fatalf("extra projects: %v", it)
			}
			expect("cursor", "1815")(t, got)
		}},
		{name: "page", method: "GET", path: "/accounts?since=100&limit=100", header: bearer, status: 200, check: func(t *testing.T, got map[string]any) {
			if g := ids(got); len(g) != 100 || g[0] != "A0101" || g[99] != "A0200" {
				t.Fatalf("page: %v", g)
			}
			expect("cursor", "200")(t, got)
		}},
		{name: "since extras", method: "GET", path: "/accounts?since=300", header: bearer, status: 200, check: func(t *testing.T, got map[string]any) {
			if g := ids(got); !reflect.DeepEqual(g, []string{"A0301", "A0302", "A0303", "A0304", "A0305"}) {
				t.Fatalf("since 300: %v", g)
			}
		}},
		{name: "nothing new keeps cursor", method: "GET", path: "/accounts?since=1815", header: bearer, status: 200, check: expect("items", []any{}, "cursor", "1815")},
		{name: "admin needs no bearer", method: "POST", path: "/admin/accounts", body: `{"external_id":"A0002","name":"Renamed SA","address":"Rue du Lac 1, 1003 Lausanne"}`, status: 200, check: expect("revision", 1816.0)},
		{name: "upsert seen by pull", method: "GET", path: "/accounts?since=1815", header: bearer, status: 200, check: func(t *testing.T, got map[string]any) {
			if it := items(got); len(it) != 1 || it[0]["name"] != "Renamed SA" {
				t.Fatalf("after upsert: %v", it)
			}
			expect("cursor", "1816")(t, got)
		}},
		{name: "upsert creates", method: "POST", path: "/admin/accounts", body: `{"external_id":"A0999","name":"New AG"}`, status: 200, check: expect("revision", 1817.0)},
		{name: "invoice without key", method: "POST", path: "/invoices", body: "a;b\n", header: bearer, status: 400},
		{name: "invoice empty body", method: "POST", path: "/invoices", body: "", header: withKey("1"), status: 400},
		{name: "invoice first", method: "POST", path: "/invoices", body: "invoice;amount\n1;10.00\n1;5.00\n", header: csv("1"), status: 200, check: expect("id", 1.0, "key", "1")},
		{name: "invoice repeated", method: "POST", path: "/invoices", body: "invoice;amount\n1;99.00\n", header: csv("1"), status: 200, check: expect("id", 1.0, "key", "1")},
		{name: "invoice other key", method: "POST", path: "/invoices", body: `{"total":"20.00"}`, header: withKey("2"), status: 200, check: expect("id", 2.0)},
		{name: "invoices received", method: "GET", path: "/invoices", header: bearer, status: 200, check: func(t *testing.T, got map[string]any) {
			it := items(got)
			if len(it) != 2 || it[0]["received"] != 2.0 || it[0]["lines"] != 3.0 || it[0]["content_type"] != "text/csv; charset=utf-8" ||
				!strings.HasSuffix(it[0]["body"].(string), "1;5.00\n") || it[1]["received"] != 1.0 {
				t.Fatalf("invoices: %v", it)
			}
		}},
		{name: "fail on", method: "POST", path: "/admin/fail", body: `{"on":true}`, status: 200, check: expect("fail", true)},
		{name: "down accounts", method: "GET", path: "/accounts", header: bearer, status: 503},
		{name: "down invoices", method: "POST", path: "/invoices", body: `{}`, header: withKey("3"), status: 503},
		{name: "admin up while down", method: "GET", path: "/admin/state", status: 200, check: expect("fail", true, "accounts", 306.0, "projects", 1510.0, "invoices", 2.0)},
		{name: "fail off", method: "POST", path: "/admin/fail", body: `{"on":false}`, status: 200, check: expect("fail", false)},
		{name: "up again", method: "GET", path: "/invoices", header: bearer, status: 200, check: func(t *testing.T, got map[string]any) {
			if len(items(got)) != 2 {
				t.Fatalf("outage stored an invoice: %v", got)
			}
		}},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			status, got := call(h, s.method, s.path, s.body, s.header)
			if status != s.status {
				t.Fatalf("status %d, want %d (%v)", status, s.status, got)
			}
			if s.check != nil {
				s.check(t, got)
			}
		})
	}
}

func TestDeterministic(t *testing.T) {
	a := httptest.NewRecorder()
	b := httptest.NewRecorder()
	for _, path := range []string{"/accounts?limit=5000", "/projects?limit=5000"} {
		for _, rec := range []*httptest.ResponseRecorder{a, b} {
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("Authorization", "Bearer "+secret)
			newStore(secret).handler().ServeHTTP(rec, req)
		}
	}
	if a.Body.String() != b.Body.String() {
		t.Fatal("two stores serve different data")
	}
}
