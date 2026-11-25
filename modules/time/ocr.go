package time

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/DorsazWilliam/sor-framework-poc/kernel"
)

// ocrRead is the job reading the receipts of the activities of an expense.attach_receipt: blob → tesseract
// → amount → expense.apply_ocr. A refused or vanished expense ends the job.
func ocrRead(ctx context.Context, k *kernel.Kernel, job kernel.Job) error {
	var a kernel.ReactionArgs
	if err := json.Unmarshal(job.Args, &a); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"params": map[string]any{"activity_ids": a.IDs}, "limit": 5000})
	out, err := k.View(ctx, job.Principal, "expense_receipt", body)
	if err != nil {
		return err
	}
	var receipts []struct {
		ID   int64  `json:"id"`
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(out, &receipts); err != nil {
		return err
	}
	for _, r := range receipts {
		text, err := readReceipt(ctx, k, r.Hash)
		if err != nil {
			return err
		}
		amount, ok := receiptAmount(text)
		if !ok {
			continue
		}
		body, _ := json.Marshal(map[string]any{"ids": []int64{r.ID}, "ocr_amount": json.Number(amount)})
		_, err = k.Call(ctx, job.Principal, "expense.apply_ocr", body)
		if pr := (*kernel.Problem)(nil); errors.As(err, &pr) {
			slog.Info("ocr.read: expense not updated", "expense", r.ID, "code", pr.Code, "conditions", pr.Rejected)
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func readReceipt(ctx context.Context, k *kernel.Kernel, hash string) (string, error) {
	blob, err := k.Blob(ctx, hash)
	if err != nil {
		return "", err
	}
	defer blob.Close()
	img, err := io.ReadAll(blob)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "tesseract", "stdin", "stdout", "-l", "fra+deu+eng", "--psm", "6")
	cmd.Env = append(os.Environ(), "OMP_THREAD_LIMIT=1")
	cmd.Stdin = bytes.NewReader(img)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	text, err := cmd.Output()
	if err != nil {
		return "", errors.New("tesseract: " + err.Error() + ": " + stderr.String())
	}
	return string(text), nil
}

var (
	amountRe   = regexp.MustCompile(`(\d{1,3}(?:[ '’.,\x{a0}\x{202f}]\d{3})+|\d+)([.,])\d{2}`)
	totalRe    = regexp.MustCompile(`total|montant|summe|betrag|gesamt|à payer|a payer|zu zahlen|amount due`)
	notTotalRe = regexp.MustCompile(`sous-total|sous total|subtotal|sub-total|sub total|zwischensumme|tva|mwst|vat`)
)

// receiptAmount reads the amount paid on a receipt (SPEC §17): the largest amount on the lines naming a
// total, other than subtotal and VAT lines; else the largest amount. It returns it as a decimal string.
func receiptAmount(text string) (string, bool) {
	var best, all int64 = -1, -1
	for line := range strings.SplitSeq(strings.ToLower(text), "\n") {
		total := totalRe.MatchString(line) && !notTotalRe.MatchString(line)
		for _, cents := range amounts(line) {
			all = max(all, cents)
			if total {
				best = max(best, cents)
			}
		}
	}
	if best < 0 {
		best = all
	}
	if best < 0 {
		return "", false
	}
	return strconv.FormatInt(best/100, 10) + "." + strconv.FormatInt(best%100/10, 10) + strconv.FormatInt(best%10, 10), true
}

// amounts are the amounts of a line in cents: digits with optional thousands separators, then a decimal
// separator and two decimals, not part of a longer number (a date such as 25.11.2026).
func amounts(line string) []int64 {
	var out []int64
	digit := func(r rune) bool { return r >= '0' && r <= '9' }
	for _, m := range amountRe.FindAllStringSubmatchIndex(line, -1) {
		units, dec := line[m[2]:m[3]], line[m[4]:m[5]]
		seps := strings.FieldsFunc(units, digit)
		if len(seps) > 0 && (seps[0] == dec || slices.ContainsFunc(seps, func(s string) bool { return s != seps[0] })) {
			continue // mixed separators, or the decimal one used for thousands: not an amount
		}
		if strings.ContainsAny(lastRune(line[:m[0]]), "0123456789.,'’") || startsNumber(line[m[1]:]) {
			continue
		}
		digits := strings.Map(func(r rune) rune {
			if digit(r) {
				return r
			}
			return -1
		}, line[m[0]:m[1]])
		if n, err := strconv.ParseInt(digits, 10, 64); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func lastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[len(r)-1])
}

// startsNumber: s goes on with a digit, or a separator then a digit.
func startsNumber(s string) bool {
	if s == "" {
		return false
	}
	if s[0] >= '0' && s[0] <= '9' {
		return true
	}
	return (s[0] == '.' || s[0] == ',') && len(s) > 1 && s[1] >= '0' && s[1] <= '9'
}
