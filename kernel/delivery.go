package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

var deliveryClient = &http.Client{Timeout: 30 * time.Second}

// deliverySend is the job posting deliveries to their connection (SPEC §12): the payload goes to
// <endpoint>/<kind>s with the delivery id as idempotency key; the outcome is recorded by delivery.result
// as the connection's principal; a failure is retried.
func deliverySend(ctx context.Context, k *Kernel, job Job) error {
	var a ReactionArgs
	if err := json.Unmarshal(job.Args, &a); err != nil {
		return river.JobCancel(err)
	}
	for _, id := range a.IDs {
		if err := k.deliver(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (k *Kernel) deliver(ctx context.Context, id int64) error {
	var kind, payload, endpoint, subject string
	var secret *string
	var sent bool
	err := k.Pool.QueryRow(ctx, `select d.kind, d.payload::text, c.endpoint, c.secret_name, c.subject,
       exists (select from delivery_result r where r.delivery_id = d.id and r.ok)
from delivery d join connection c on c.id = d.connection_id where d.id = $1`, id).Scan(&kind, &payload, &endpoint, &secret, &subject, &sent)
	if errors.Is(err, pgx.ErrNoRows) {
		return river.JobCancel(fmt.Errorf("no delivery %d", id))
	}
	if err != nil || sent {
		return err
	}
	p, err := k.Principal(ctx, []string{subject})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(endpoint, "/")+"/"+kind+"s", strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(id))
	if secret != nil {
		req.Header.Set("Authorization", "Bearer "+os.Getenv("SECRET_"+*secret))
	}
	var status *int
	var detail string
	res, err := deliveryClient.Do(req)
	if err != nil {
		detail = err.Error()
	} else {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2000))
		res.Body.Close()
		status, detail = &res.StatusCode, string(bytes.ToValidUTF8(b, nil))
	}
	ok := status != nil && *status < 300
	body, _ := json.Marshal(map[string]any{"ids": []int64{id}, "ok": ok, "status": status, "detail": detail})
	if _, err := k.Call(ctx, p, "delivery.result", body); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("delivery %d: %s", id, detail)
	}
	return nil
}
