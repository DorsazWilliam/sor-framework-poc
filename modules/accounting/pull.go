package accounting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
)

var client = &http.Client{Timeout: 30 * time.Second}

// feeds are pulled in order: a project references its client.
var feeds = []struct{ name, command string }{
	{"accounts", "account.upsert_from_accounting"},
	{"projects", "project.upsert_from_accounting"},
}

type connection struct {
	ID         int64   `json:"id"`
	Endpoint   string  `json:"endpoint"`
	SecretName *string `json:"secret_name"`
	Cursor     *string `json:"cursor"`
}

// accountingPull is the job pulling the accounting connections (all, or those of a connection.pull): each
// changed item since the cursor → its upsert command; then connection.advance records the new cursor, or
// the error with the cursor unchanged.
func accountingPull(ctx context.Context, k *kernel.Kernel, job kernel.Job) error {
	var a kernel.ReactionArgs
	json.Unmarshal(job.Args, &a) // null for the periodic job
	filter := []kernel.Filter{{Col: "kind", Op: "eq", Value: "accounting"}}
	if len(a.IDs) > 0 {
		filter = append(filter, kernel.Filter{Col: "id", Op: "in", Value: a.IDs})
	}
	body, _ := json.Marshal(map[string]any{"filter": filter, "limit": 5000})
	out, err := k.View(ctx, job.Principal, "connection", body)
	if err != nil {
		return err
	}
	var conns []connection
	if err := json.Unmarshal(out, &conns); err != nil {
		return err
	}
	for _, c := range conns {
		advance := map[string]any{"ids": []int64{c.ID}}
		if cursor, err := pull(ctx, k, job.Principal, c); err != nil {
			advance["pull_error"] = err.Error()
		} else {
			advance["cursor"] = cursor
		}
		body, _ := json.Marshal(advance)
		if _, err := k.Call(ctx, job.Principal, "connection.advance", body); err != nil {
			return err
		}
	}
	return nil
}

// pull upserts the items of every feed changed since the connection's cursor and returns the new cursor,
// {"<feed>": "<cursor>"}.
func pull(ctx context.Context, k *kernel.Kernel, p *kernel.Principal, c connection) (string, error) {
	cursor := map[string]string{}
	if c.Cursor != nil {
		json.Unmarshal([]byte(*c.Cursor), &cursor)
	}
	for _, f := range feeds {
		var input []string
		for _, in := range k.CommandByName(f.command).Input {
			input = append(input, in.Name)
		}
		for {
			var page struct {
				Items  []map[string]any `json:"items"`
				Cursor string           `json:"cursor"`
			}
			if err := get(ctx, c, f.name+"?since="+url.QueryEscape(cursor[f.name]), &page); err != nil {
				return "", err
			}
			for _, item := range page.Items {
				in := map[string]any{}
				for _, name := range input {
					in[name] = item[name]
				}
				body, _ := json.Marshal(in)
				if _, err := k.Call(ctx, p, f.command, body); err != nil {
					return "", fmt.Errorf("%s %v: %w", f.command, item["external_id"], err)
				}
			}
			if len(page.Items) == 0 || page.Cursor == cursor[f.name] {
				break
			}
			cursor[f.name] = page.Cursor
		}
	}
	b, _ := json.Marshal(cursor)
	return string(b), nil
}

func get(ctx context.Context, c connection, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.Endpoint, "/")+"/"+path, nil)
	if err != nil {
		return err
	}
	if c.SecretName != nil {
		req.Header.Set("Authorization", "Bearer "+os.Getenv("SECRET_"+*c.SecretName))
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 500))
		return fmt.Errorf("GET %s: %s %s", path, res.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(res.Body).Decode(dst)
}
