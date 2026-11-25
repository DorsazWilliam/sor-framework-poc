package kernel_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
)

func TestHTTP(t *testing.T) {
	k := setup(t)
	h, api := k.API(kernel.Config{
		Issuer: issuer(t),
		Labels: fstest.MapFS{"fr.json": {Data: []byte(`{"t.entry":"Saisie"}`)}},
		UI:     fstest.MapFS{"index.html": {Data: []byte("<p>app")}},
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	do := func(method, path, token, body string) (*http.Response, map[string]any, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+jwt(t, token))
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
	res, m, _ := do("GET", "/api/me", "agent-user", "")
	if res.StatusCode != 200 || m["id"].(float64) != 4 || m["self"].(float64) != 2 || m["for"].(map[string]any)["name"] != "Alice" || len(m["functions"].([]any)) != 1 ||
		m["reach"].(map[string]any)["self"].([]any)[0].(float64) != 2 {
		t.Fatalf("me as agent for alice: %v", m)
	}
	res, _, body := do("POST", "/api/v/entry_grid", "user", `{"sort":[{"col":"day","dir":"asc"}]}`)
	if res.StatusCode != 200 || !strings.HasPrefix(body, `[{"id":1,`) || !strings.Contains(res.Header.Get("Server-Timing"), `desc="2 rt"`) {
		t.Fatalf("view: %d %s %s", res.StatusCode, res.Header.Get("Server-Timing"), body)
	}
	res, m, _ = do("POST", "/api/c/entry.update", "user", `{"ids":[1],"worked_min":900}`)
	if res.StatusCode != 409 || m["code"] != "rejected" || res.Header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("rejected: %d %v", res.StatusCode, m)
	}
	res, m, _ = do("POST", "/api/c/entry.update", "user", `{"ids":"x"}`)
	if res.StatusCode != 422 || m["code"] != "invalid" || len(m["errors"].([]any)) == 0 {
		t.Fatalf("invalid: %d %v", res.StatusCode, m)
	}
	res, m, _ = do("POST", "/api/c/entry.update", "user", `{"ids":[1],"note":"http"}`)
	if res.StatusCode != 200 || m["ids"].([]any)[0].(float64) != 1 || m["changed"].([]any)[0].(map[string]any)["table"] != "entry" {
		t.Fatalf("update: %d %v", res.StatusCode, m)
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
	// The meta served to a principal holds what its capability permits only.
	names := func(m map[string]any, key string) []any {
		var out []any
		for _, x := range m[key].([]any) {
			out = append(out, x.(map[string]any)["name"])
		}
		return out
	}
	_, m, _ = do("GET", "/api/meta", "user", "")
	if !slices.Contains(names(m, "views"), "entry_grid") || slices.Contains(names(m, "commands"), "entry.validate") ||
		slices.Contains(names(m, "views"), "entry_run_candidates") || slices.Contains(names(m, "tables"), "connection") || m["functions"].([]any)[0] != "entry" {
		t.Fatalf("meta of alice: %v", m)
	}
	if _, m, _ = do("GET", "/api/meta", "boss", ""); !slices.Contains(names(m, "commands"), "entry.validate") {
		t.Fatalf("meta of boss: %v", m["commands"])
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
	for _, path := range []string{"/api/me", "/api/meta"} {
		if ref := oa.Paths[path]["get"].Responses["200"].Content["application/json"].Schema["$ref"]; ref == nil {
			t.Fatalf("openapi %s: %s", path, doc)
		}
	}
}
