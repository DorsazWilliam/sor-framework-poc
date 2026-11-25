package kernel_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
)

// storage opens the object storage of $TEST_S3_ENDPOINT in a bucket of the test's own.
func storage(t *testing.T, k *kernel.Kernel) {
	t.Helper()
	ep := os.Getenv("TEST_S3_ENDPOINT")
	if ep == "" {
		t.Skip("TEST_S3_ENDPOINT not set")
	}
	bucket := "t-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")) + "-" + time.Now().Format("150405")
	if err := k.OpenStorage(context.Background(), kernel.Storage{Endpoint: ep, PublicEndpoint: ep, Bucket: bucket,
		AccessKey: "sor", SecretKey: "sor-dev-secret"}); err != nil {
		t.Fatal(err)
	}
}

// issuer is the identity provider of the tests, $TEST_ISSUER (deploy/idp.json: a client id is the subject of its token,
// agent-user acts for user).
func issuer(t *testing.T) string {
	t.Helper()
	iss := os.Getenv("TEST_ISSUER")
	if iss == "" {
		t.Skip("TEST_ISSUER not set")
	}
	return iss
}

var tokens sync.Map

// jwt is a token of the identity provider for client id sub (client credentials).
func jwt(t *testing.T, sub string) string {
	t.Helper()
	if tok, ok := tokens.Load(sub); ok {
		return tok.(string)
	}
	res, err := http.PostForm(issuer(t)+"/token", url.Values{"grant_type": {"client_credentials"}, "client_id": {sub}, "client_secret": {"secret"}})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body.AccessToken == "" {
		t.Fatalf("token of %s: %d %v", sub, res.StatusCode, err)
	}
	tokens.Store(sub, body.AccessToken)
	return body.AccessToken
}

// server serves the API, authenticating with the identity provider of the tests.
func server(t *testing.T, k *kernel.Kernel) *httptest.Server {
	h, _ := k.API(kernel.Config{Issuer: issuer(t)})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, srv *httptest.Server, path, token string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(string(b)))
	req.Header.Set("Authorization", "Bearer "+jwt(t, token))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	json.NewDecoder(res.Body).Decode(&m)
	return res.StatusCode, m
}

func TestFiles(t *testing.T) {
	k := setup(t)
	storage(t, k)
	srv := server(t, k)
	ctx := context.Background()
	data := "receipt 42.50"
	sum := sha256.Sum256([]byte(data))
	hash := hex.EncodeToString(sum[:])

	_, m := post(t, srv, "/api/files/presign", "user", map[string]any{"hash": hash, "size": len(data), "type": "text/plain"})
	if m["exists"] != false || m["url"] == nil {
		t.Fatalf("presign: %v", m)
	}
	put := func(body string) int {
		req, _ := http.NewRequest("PUT", m["url"].(string), strings.NewReader(body))
		for h, v := range m["headers"].(map[string]any) {
			req.Header.Set(h, v.(string))
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if st := put("receipt 99.99"); st < 400 {
		t.Fatalf("a different content was stored: %d", st)
	}
	if st := put(data); st != 200 {
		t.Fatalf("put: %d", st)
	}
	if _, m := post(t, srv, "/api/files/presign", "user", map[string]any{"hash": hash, "size": len(data), "type": "text/plain"}); m["exists"] != true {
		t.Fatalf("presign of a stored blob: %v", m)
	}
	st, m := post(t, srv, "/api/files", "user", map[string]any{"hash": hash, "name": "receipt.txt", "type": "text/plain"})
	if st != 200 || m["id"] == nil {
		t.Fatalf("register: %d %v", st, m)
	}
	id := int64(m["id"].(float64))
	call(t, k, principal(t, k, "user"), "entry.update", `{"ids":[1],"file_id":`+itoa(id)+`}`)

	req := map[string]any{"view": "entry", "params": map[string]any{}, "id": 1, "column": "file_id"}
	st, m = post(t, srv, "/api/file", "user", req)
	if st != 200 || m["name"] != "receipt.txt" || m["type"] != "text/plain" {
		t.Fatalf("download: %d %v", st, m)
	}
	res, err := http.Get(m["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(got) != data {
		t.Fatalf("downloaded %q", got)
	}
	if st, m := post(t, srv, "/api/file", "bob", req); st != 404 || m["code"] != "not_found" {
		t.Fatalf("download out of reach: %d %v", st, m)
	}
	if st, _ := post(t, srv, "/api/file", "user", map[string]any{"view": "entry", "id": 1, "column": "worked_min"}); st != 422 {
		t.Fatalf("download of a column that is not a file: %d", st)
	}

	// PutFile: the same bytes give the same row; an unreferenced file is collected with its blob.
	a, err := k.PutFile(ctx, []byte("generated"), "a.pdf", "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	b, err := k.PutFile(ctx, []byte("generated"), "b.pdf", "application/pdf")
	if err != nil || a != b {
		t.Fatalf("PutFile twice: %d %d %v", a, b, err)
	}
	if n := count(t, k, "select count(*) from file"); n != 2 {
		t.Fatalf("file rows: %d", n)
	}
	k.Pool.Exec(ctx, "update file set created_at = created_at - interval '2 days'")
	start(t, k) // file.gc runs on start
	eventually(t, 10*time.Second, "file.gc", func() bool { return count(t, k, "select count(*) from file where id = $1", itoa(a)) == 0 })
	if n := count(t, k, "select count(*) from file where id = $1", itoa(id)); n != 1 {
		t.Fatal("file.gc removed a referenced file")
	}
	sum = sha256.Sum256([]byte("generated"))
	if _, err := k.Blob(ctx, hex.EncodeToString(sum[:])); err == nil {
		t.Fatal("blob of the collected file is still there")
	}
	if r, err := k.Blob(ctx, hash); err != nil {
		t.Fatal(err)
	} else {
		r.Close()
	}
}
