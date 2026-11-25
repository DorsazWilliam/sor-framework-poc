package kernel_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

func TestMCP(t *testing.T) {
	k := setup(t)
	srv := server(t, k)
	ctx := context.Background()
	session := func(token string) *mcp.ClientSession {
		c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		s, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer(jwt(t, token))}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	tools := func(s *mcp.ClientSession) []string {
		res, err := s.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tl := range res.Tools {
			names = append(names, tl.Name)
		}
		return names
	}
	alice, boss := session("user"), session("boss")
	if a, b := tools(alice), tools(boss); slices.Contains(a, "entry.validate") || !slices.Contains(b, "entry.validate") || !slices.Contains(a, "entry_grid") {
		t.Fatalf("tools by capability: alice %v, boss %v", a, b)
	}

	res, err := alice.CallTool(ctx, &mcp.CallToolParams{Name: "entry_grid", Arguments: map[string]any{"sort": []any{map[string]any{"col": "day"}}}})
	if err != nil || res.IsError {
		t.Fatalf("view tool: %v %+v", err, res)
	}
	var rows struct{ Rows []map[string]any }
	b, _ := json.Marshal(res.StructuredContent)
	json.Unmarshal(b, &rows)
	if len(rows.Rows) != 2 || rows.Rows[0]["person"] != "Alice" {
		t.Fatalf("view rows: %s", b)
	}

	res, err = alice.CallTool(ctx, &mcp.CallToolParams{Name: "entry.update", Arguments: map[string]any{"ids": []int{1}, "worked_min": 900}})
	if err != nil || !res.IsError {
		t.Fatalf("rejected command: %v %+v", err, res)
	}
	var pr struct {
		Code     string
		Rejected []struct {
			ID         int64
			Conditions []string
			Row        map[string]any
		}
	}
	b, _ = json.Marshal(res.StructuredContent)
	json.Unmarshal(b, &pr)
	if pr.Code != "rejected" || len(pr.Rejected) != 1 || pr.Rejected[0].Conditions[0] != "short" || pr.Rejected[0].Row["worked_min"].(float64) != 900 {
		t.Fatalf("rejection: %s", b)
	}

	res, err = alice.CallTool(ctx, &mcp.CallToolParams{Name: "entry.update", Arguments: map[string]any{"ids": []int{1}, "note": "mcp"}})
	if err != nil || res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, `"changed"`) {
		t.Fatalf("command tool: %v %+v", err, res)
	}

	rr, err := alice.ReadResource(ctx, &mcp.ReadResourceParams{URI: "screen://entry_lines"})
	if err != nil || !strings.Contains(rr.Contents[0].Text, `"views":["entry_line"]`) {
		t.Fatalf("screen resource: %v %+v", err, rr)
	}
	if rr, err := alice.ReadResource(ctx, &mcp.ReadResourceParams{URI: "screen://"}); err != nil || !strings.Contains(rr.Contents[0].Text, `"id":"entries"`) {
		t.Fatalf("screens: %v %+v", err, rr)
	}
	if _, err := alice.ReadResource(ctx, &mcp.ReadResourceParams{URI: "screen://nope"}); err == nil {
		t.Fatal("unknown screen")
	}
}
