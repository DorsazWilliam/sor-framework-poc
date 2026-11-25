package kernel

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type principalKey struct{}

// mcpHandler serves MCP (SPEC §13): stateless Streamable HTTP behind the API authentication; per request
// a server with one tool per view and per command permitted to the principal, with the HTTP schemas.
func (k *Kernel) mcpHandler() http.Handler {
	schemas := map[string]json.RawMessage{}
	for _, v := range k.Meta.Views {
		schemas["v "+v.Name], _ = json.Marshal(v.in)
	}
	for _, c := range k.Meta.Commands {
		schemas["c "+c.Name], _ = json.Marshal(c.in)
	}
	screens, _ := json.Marshal(k.Meta.Screens)
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		p := r.Context().Value(principalKey{}).(*Principal)
		s := mcp.NewServer(&mcp.Implementation{Name: "sor", Version: "0.1.0"}, nil)
		for _, v := range k.Meta.Views {
			if p.can(v.Needs) == nil {
				s.AddTool(&mcp.Tool{Name: v.Name, Description: v.Description, InputSchema: schemas["v "+v.Name]},
					func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						out, err := k.View(ctx, p, v.Name, req.Params.Arguments)
						return toolResult(map[string]json.RawMessage{"rows": out}, err)
					})
			}
		}
		for _, c := range k.Meta.Commands {
			if p.can(c.Needs) == nil {
				s.AddTool(&mcp.Tool{Name: c.Name, Description: c.Description, InputSchema: schemas["c "+c.Name]},
					func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						res, err := k.Call(ctx, p, c.Name, req.Params.Arguments)
						return toolResult(res, err)
					})
			}
		}
		s.AddResource(&mcp.Resource{URI: "screen://", Name: "screens", Description: "All screens: their views, commands and children.",
			MIMEType: "application/json"}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return resource(req.Params.URI, screens), nil
		})
		s.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "screen://{id}", Name: "screen",
			Description: "A screen: its views, commands and children.", MIMEType: "application/json"},
			func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				sc := findScreen(k.Meta.Screens, strings.TrimPrefix(req.Params.URI, "screen://"))
				if sc == nil {
					return nil, mcp.ResourceNotFoundError(req.Params.URI)
				}
				b, _ := json.Marshal(sc)
				return resource(req.Params.URI, b), nil
			})
		return s
	}, &mcp.StreamableHTTPOptions{Stateless: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info(r.Context()).name = "mcp"
		p, err := k.authenticate(r)
		if err != nil {
			writeProblem(w, err)
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

// toolResult returns out as text and structured content, or the problem body with isError.
func toolResult(out any, err error) (*mcp.CallToolResult, error) {
	isErr := err != nil
	if isErr {
		out = asProblem(err)
	}
	b, _ := json.Marshal(out)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}, StructuredContent: json.RawMessage(b), IsError: isErr}, nil
}

func resource(uri string, b []byte) *mcp.ReadResourceResult {
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(b)}}}
}

func findScreen(ss []Screen, id string) *Screen {
	for i := range ss {
		if ss[i].ID == id {
			return &ss[i]
		}
		if s := findScreen(ss[i].Children, id); s != nil {
			return s
		}
	}
	return nil
}
