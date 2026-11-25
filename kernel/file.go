package kernel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// Storage is the object storage of the blobs (SPEC §11).
type Storage struct {
	Endpoint       string // what the app reaches
	PublicEndpoint string // what the browser reaches; signed URLs point there
	Bucket         string
	AccessKey      string
	SecretKey      string
}

type store struct {
	s3      *s3.Client
	presign *s3.PresignClient
	bucket  string
}

const signedFor = 5 * time.Minute

var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// OpenStorage connects the object storage and creates its bucket if missing.
func (k *Kernel) OpenStorage(ctx context.Context, s Storage) error {
	client := func(endpoint string) *s3.Client {
		return s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String(endpoint), UsePathStyle: true,
			Credentials: credentials.NewStaticCredentialsProvider(s.AccessKey, s.SecretKey, "")})
	}
	st := &store{s3: client(s.Endpoint), presign: s3.NewPresignClient(client(s.PublicEndpoint)), bucket: s.Bucket}
	if _, err := st.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &s.Bucket}); err != nil {
		if _, err := st.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &s.Bucket}); err != nil {
			return err
		}
	}
	k.store = st
	return nil
}

// head returns the size and type of a blob, or found false.
func (st *store) head(ctx context.Context, hash string) (size int64, typ string, found bool, err error) {
	out, err := st.s3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &st.bucket, Key: &hash})
	var ae smithy.APIError
	if errors.As(err, &ae) && (ae.ErrorCode() == "NotFound" || ae.ErrorCode() == "NoSuchKey") {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	return aws.ToInt64(out.ContentLength), aws.ToString(out.ContentType), true, nil
}

func checksum(hash string) string {
	b, _ := hex.DecodeString(hash)
	return base64.StdEncoding.EncodeToString(b)
}

// Blob opens the content of a file by its hash.
func (k *Kernel) Blob(ctx context.Context, hash string) (io.ReadCloser, error) {
	out, err := k.store.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: &k.store.bucket, Key: &hash})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

// PutFile stores data as a blob unless present and returns its file row: the same bytes always give
// the same row.
func (k *Kernel) PutFile(ctx context.Context, data []byte, name, typ string) (int64, error) {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	_, _, found, err := k.store.head(ctx, hash)
	if err != nil {
		return 0, err
	}
	if !found {
		_, err := k.store.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: &k.store.bucket, Key: &hash, Body: bytes.NewReader(data),
			ContentType: &typ, ChecksumSHA256: aws.String(checksum(hash))})
		if err != nil {
			return 0, err
		}
	}
	return k.fileRow(ctx, hash, int64(len(data)), typ, name)
}

func (k *Kernel) fileRow(ctx context.Context, hash string, size int64, typ, name string) (int64, error) {
	var id int64
	err := k.Pool.QueryRow(ctx, `insert into file (hash, size, type, name) values ($1, $2, $3, $4)
on conflict (hash) do update set hash = excluded.hash returning id`, hash, strconv.FormatInt(size, 10), typ, name).Scan(&id)
	return id, err
}

// fileRoutes serves the upload and download contract of SPEC §13.
func (k *Kernel) fileRoutes(mux *http.ServeMux) {
	handle := func(path string, fn func(ctx context.Context, p *Principal, body []byte) (any, error)) {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			k.serve(w, r, path, func(p *Principal, body []byte) (any, error) {
				if k.store == nil {
					return nil, problem(404, "not_found", "no storage")
				}
				return fn(r.Context(), p, body)
			})
		})
	}
	handle("/api/files/presign", func(ctx context.Context, _ *Principal, body []byte) (any, error) {
		var in struct {
			Hash string `json:"hash"`
			Size int64  `json:"size"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(body, &in); err != nil || !hashRe.MatchString(in.Hash) || in.Size < 0 {
			return nil, problem(422, "invalid", "hash (sha256 hex), size, type")
		}
		_, _, found, err := k.store.head(ctx, in.Hash)
		if err != nil || found {
			return map[string]any{"exists": true}, err
		}
		sum := checksum(in.Hash)
		req, err := k.store.presign.PresignPutObject(ctx, &s3.PutObjectInput{Bucket: &k.store.bucket, Key: &in.Hash,
			ContentType: &in.Type, ChecksumSHA256: &sum}, s3.WithPresignExpires(signedFor))
		if err != nil {
			return nil, err
		}
		return map[string]any{"exists": false, "url": req.URL,
			"headers": map[string]string{"Content-Type": in.Type, "x-amz-checksum-sha256": sum}}, nil
	})
	handle("/api/files", func(ctx context.Context, _ *Principal, body []byte) (any, error) {
		var in struct {
			Hash string `json:"hash"`
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(body, &in); err != nil || !hashRe.MatchString(in.Hash) || in.Name == "" {
			return nil, problem(422, "invalid", "hash (sha256 hex), name, type")
		}
		size, typ, found, err := k.store.head(ctx, in.Hash)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, problem(404, "not_found", "no blob "+in.Hash)
		}
		if typ == "" {
			typ = in.Type
		}
		id, err := k.fileRow(ctx, in.Hash, size, typ, in.Name)
		return map[string]int64{"id": id}, err
	})
	handle("/api/file", func(ctx context.Context, p *Principal, body []byte) (any, error) {
		var in struct {
			View   string          `json:"view"`
			Params json.RawMessage `json:"params"`
			ID     int64           `json:"id"`
			Column string          `json:"column"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			return nil, problem(422, "invalid", err.Error())
		}
		v := k.views[in.View]
		if v == nil {
			return nil, problem(404, "not_found", "no view "+in.View)
		}
		// the column references file: the view's reach guards the file
		if !slices.ContainsFunc(v.Columns, func(c Col) bool { return c.Name == in.Column && c.Ref == "file" }) {
			return nil, problem(422, "invalid", in.Column+" is not a file column of "+in.View)
		}
		if len(in.Params) == 0 {
			in.Params = json.RawMessage("{}")
		}
		req, _ := json.Marshal(map[string]any{"params": in.Params, "filter": []Filter{{Col: "id", Op: "eq", Value: in.ID}}, "limit": 1})
		out, err := k.View(ctx, p, in.View, req)
		if err != nil {
			return nil, err
		}
		var rows []map[string]any
		json.Unmarshal(out, &rows)
		if len(rows) == 0 || rows[0][in.Column] == nil {
			return nil, problem(404, "not_found", "")
		}
		var hash, name, typ string
		if err := k.Pool.QueryRow(ctx, "select hash, name, type from file where id = $1", pgValue(rows[0][in.Column], "int8")).Scan(&hash, &name, &typ); err != nil {
			return nil, problem(404, "not_found", "")
		}
		disp := `inline; filename="` + strings.ReplaceAll(name, `"`, "") + `"`
		req2, err := k.store.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &k.store.bucket, Key: &hash,
			ResponseContentType: &typ, ResponseContentDisposition: &disp}, s3.WithPresignExpires(signedFor))
		if err != nil {
			return nil, err
		}
		return map[string]string{"url": req2.URL, "name": name, "type": typ}, nil
	})
}

// fileGC is the job deleting the file rows referenced by no foreign key, registered for more than a
// day (an upload is attached by a command after its registration), then their blobs.
func fileGC(ctx context.Context, k *Kernel, _ Job) error {
	if k.store == nil {
		return nil
	}
	rows, err := k.Pool.Query(ctx, `select c.conrelid::regclass::text, a.attname
from pg_constraint c join pg_attribute a on a.attrelid = c.conrelid and a.attnum = c.conkey[1]
where c.contype = 'f' and c.confrelid = 'file'::regclass`)
	if err != nil {
		return err
	}
	var refs []string
	for rows.Next() {
		var t, col string
		if err := rows.Scan(&t, &col); err != nil {
			return err
		}
		refs = append(refs, "not exists (select from "+t+" r where r."+ident(col)+" = f.id)")
	}
	if err := rows.Err(); err != nil {
		return err
	}
	where := "f.created_at < sor_now() - interval '1 day'"
	if len(refs) > 0 {
		where += " and " + strings.Join(refs, " and ")
	}
	rows, err = k.Pool.Query(ctx, "delete from file f where "+where+" returning hash")
	if err != nil {
		return err
	}
	var hashes []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return err
		}
		hashes = append(hashes, h)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, h := range hashes {
		if _, err := k.store.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &k.store.bucket, Key: &h}); err != nil {
			return err
		}
	}
	return nil
}
