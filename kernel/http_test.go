package kernel_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
)

func TestHTTP(t *testing.T) {
	k := setup(t)
	h, api := k.API(kernel.Config{
		Verify: func(_ context.Context, raw string) ([]string, error) { return strings.Split(raw, "+"), nil },
		Labels: fstest.MapFS{"fr.json": {Data: []byte(`{"t.entry":"Saisie"}`)}},
		UI:     fstest.MapFS{"index.html": {Data: []byte("<p>app")}},
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	do := func(method, path, token, body string) (*http.Response, map[string]any, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		var m map[string]any
		json.Unmarshal(b, &m)
		return res, m, string(b)
	}
	if res, m, _ := do("GET", "/api/me", "", ""); res.StatusCode != 401 || m["code"] != "unauthenticated" {
		t.Fatalf("no token: %d %v", res.StatusCode, m)
	}
	if res, m, _ := do("GET", "/api/me", "nobody", ""); res.StatusCode != 403 || m["code"] != "forbidden" {
		t.Fatalf("unknown subject: %d %v", res.StatusCode, m)
	}
	res, m, _ := do("GET", "/api/me", "bot+alice", "")
	if res.StatusCode != 200 || m["id"].(float64) != 4 || m["for"].(map[string]any)["name"] != "Alice" || len(m["functions"].([]any)) != 1 ||
		m["reach"].(map[string]any)["self"].([]any)[0].(float64) != 2 {
		t.Fatalf("me as agent for alice: %v", m)
	}
	res, _, body := do("POST", "/api/v/entry_grid", "alice", `{"sort":[{"col":"day","dir":"asc"}]}`)
	if res.StatusCode != 200 || !strings.HasPrefix(body, `[{"id":1,`) || !strings.Contains(res.Header.Get("Server-Timing"), `desc="2 rt"`) {
		t.Fatalf("view: %d %s %s", res.StatusCode, res.Header.Get("Server-Timing"), body)
	}
	res, m, _ = do("POST", "/api/c/entry.update", "alice", `{"ids":[1],"worked_min":900}`)
	if res.StatusCode != 409 || m["code"] != "rejected" || res.Header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("rejected: %d %v", res.StatusCode, m)
	}
	res, m, _ = do("POST", "/api/c/entry.update", "alice", `{"ids":"x"}`)
	if res.StatusCode != 422 || m["code"] != "invalid" || len(m["errors"].([]any)) == 0 {
		t.Fatalf("invalid: %d %v", res.StatusCode, m)
	}
	res, m, _ = do("POST", "/api/c/entry.update", "alice", `{"ids":[1],"note":"http"}`)
	if res.StatusCode != 200 || m["ids"].([]any)[0].(float64) != 1 || m["changed"].([]any)[0].(map[string]any)["table"] != "entry" {
		t.Fatalf("update: %d %v", res.StatusCode, m)
	}

	// The client chooses the call id.
	callWith := func(id string) (*http.Response, map[string]any) {
		req, _ := http.NewRequest("POST", srv.URL+"/api/c/entry.update", strings.NewReader(`{"ids":[1],"note":"call id"}`))
		req.Header.Set("Authorization", "Bearer alice")
		req.Header.Set("Sor-Call", id)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var m map[string]any
		json.NewDecoder(res.Body).Decode(&m)
		return res, m
	}
	const id = "0b6f1c2e-6a41-4c55-9d1e-0f5b7c3a9e21"
	if res, m := callWith(id); res.StatusCode != 200 || m["call"] != id || count(t, k, "select count(*) from event where call = $1::uuid", id) != 1 {
		t.Fatalf("Sor-Call: %d %v", res.StatusCode, m)
	}
	if res, m := callWith("nope"); res.StatusCode != 422 || m["code"] != "invalid" {
		t.Fatalf("bad Sor-Call: %d %v", res.StatusCode, m)
	}
	if _, _, body := do("GET", "/api/labels/fr", "", ""); body != `{"t.entry":"Saisie"}` {
		t.Fatalf("labels: %s", body)
	}
	if _, _, body := do("GET", "/api/labels/de", "", ""); body != `{}` {
		t.Fatalf("missing labels: %s", body)
	}
	if _, _, body := do("GET", "/screens/x", "", ""); body != "<p>app" {
		t.Fatalf("spa fallback: %s", body)
	}
	_, m, _ = do("GET", "/api/meta", "alice", "")
	if len(m["views"].([]any)) == 0 || m["functions"].([]any)[0] != "entry" {
		t.Fatalf("meta: %v", m)
	}

	doc, _ := json.Marshal(api.OpenAPI())
	var oa struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			RequestBody struct {
				Content map[string]struct{ Schema map[string]any } `json:"content"`
			} `json:"requestBody"`
			Responses map[string]struct {
				Content map[string]struct{ Schema map[string]any } `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
	}
	json.Unmarshal(doc, &oa)
	grid := oa.Paths["/api/v/entry_grid"]["post"]
	item := grid.Responses["200"].Content["application/json"].Schema["items"].(map[string]any)
	if grid.OperationID != "view_entry_grid" || item["properties"].(map[string]any)["worked_min"].(map[string]any)["type"] != "integer" {
		t.Fatalf("openapi view: %s", doc)
	}
	create := oa.Paths["/api/c/entry.create"]["post"].RequestBody.Content["application/json"].Schema
	if req := create["required"].([]any); len(req) != 3 || create["properties"].(map[string]any)["day"].(map[string]any)["format"] != "date" {
		t.Fatalf("openapi create: %v", create)
	}
	if _, ok := oa.Paths["/api/c/entry.validate"]["post"].RequestBody.Content["application/json"].Schema["properties"].(map[string]any)["ids"]; !ok {
		t.Fatal("openapi custom command without ids")
	}
}
