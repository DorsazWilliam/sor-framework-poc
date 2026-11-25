package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
)

// invoiceRender is the job of invoice.print, as its principal: the invoice document and the Typst template
// give the PDF, stored by its hash and attached by invoice.attach_pdf. The same document gives the same bytes.
func invoiceRender(ctx context.Context, k *kernel.Kernel, job kernel.Job) error {
	var a kernel.ReactionArgs
	if err := json.Unmarshal(job.Args, &a); err != nil {
		return err
	}
	for _, id := range a.IDs {
		body, _ := json.Marshal(map[string]any{"params": map[string]any{"invoice_id": id}})
		out, err := k.View(ctx, job.Principal, "invoice_document", body)
		if err != nil {
			return err
		}
		var docs []struct {
			Document json.RawMessage `json:"document"`
		}
		if err := json.Unmarshal(out, &docs); err != nil || len(docs) != 1 {
			return fmt.Errorf("invoice %d: no document", id)
		}
		pdf, err := render(ctx, docs[0].Document)
		if err != nil {
			return err
		}
		file, err := k.PutFile(ctx, pdf, "invoice-"+strconv.FormatInt(id, 10)+".pdf", "application/pdf")
		if err != nil {
			return err
		}
		body, _ = json.Marshal(map[string]any{"ids": []int64{id}, "pdf_file_id": file})
		if _, err := k.Call(ctx, job.Principal, "invoice.attach_pdf", body); err != nil {
			if pr := (*kernel.Problem)(nil); errors.As(err, &pr) {
				return nil // the invoice is gone or out of reach: nothing to attach
			}
			return err
		}
	}
	return nil
}

// render compiles the template on the document, dated by the document's epoch, with the fonts built into Typst.
func render(ctx context.Context, document []byte) ([]byte, error) {
	var d struct {
		Epoch int64 `json:"epoch"`
	}
	if err := json.Unmarshal(document, &d); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "invoice-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "data.json"), document, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "invoice.typ"), []byte(Template), 0o600); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "typst", "compile", "--ignore-system-fonts", "--creation-timestamp", strconv.FormatInt(d.Epoch, 10),
		"invoice.typ", "out.pdf")
	cmd.Dir = dir
	if msg, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("typst: %w: %s", err, msg)
	}
	return os.ReadFile(filepath.Join(dir, "out.pdf"))
}
