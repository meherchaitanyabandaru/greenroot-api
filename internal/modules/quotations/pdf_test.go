package quotations

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFormatINR(t *testing.T) {
	cases := []struct {
		amount float64
		want   string
	}{
		{684, "Rs.684.00"},
		{50, "Rs.50.00"},
		{12500, "Rs.12,500.00"},
		{101275, "Rs.1,01,275.00"},
		{1234567.5, "Rs.12,34,567.50"},
		{0, "Rs.0.00"},
		{-500, "-Rs.500.00"},
	}
	for _, c := range cases {
		if got := FormatINR(c.amount); got != c.want {
			t.Errorf("FormatINR(%v) = %q, want %q", c.amount, got, c.want)
		}
	}
}

func TestAmountInWordsHyphenation(t *testing.T) {
	cases := []struct {
		amount float64
		want   string
	}{
		{684, "Six Hundred Eighty-Four Rupees"},
		{101275, "One Lakh One Thousand Two Hundred Seventy-Five Rupees"},
		{100, "One Hundred Rupees"},
		{20, "Twenty Rupees"},
	}
	for _, c := range cases {
		if got := amountInWords(c.amount); got != c.want {
			t.Errorf("amountInWords(%v) = %q, want %q", c.amount, got, c.want)
		}
	}
}

// buildTestQuotation returns a valid CUSTOMER quotation with n items, useful for
// exercising pagination at various sizes.
func buildTestQuotation(n int) Quotation {
	items := make([]QuotationItem, n)
	for i := 0; i < n; i++ {
		qty := float64(5 + i%20)
		price := float64(50 + i*3)
		items[i] = QuotationItem{
			ID:             int64(i + 1),
			ScientificName: fmt.Sprintf("Plantus scientificus %d", i+1),
			CommonName:     strPtr(fmt.Sprintf("Common Plant %d", i+1)),
			Quantity:       qty,
			UnitPrice:      price,
			TotalPrice:     qty * price,
		}
	}
	total := 0.0
	for _, it := range items {
		total += it.TotalPrice
	}
	notes := "Sample notes for this quotation."
	validUntil := time.Now().Add(15 * 24 * time.Hour)
	return Quotation{
		ID:              1,
		QuotationCode:   "QUO-20260725-0099",
		QuotationType:   "CUSTOMER",
		CreatedByUserID: 2,
		CreatedByName:   strPtr("Adam"),
		NurseryID:       int64Ptr(1),
		NurseryName:     strPtr("eden gardens"),
		NurseryPhone:    strPtr("9100000000"),
		RecipientName:   strPtr("Raju"),
		RecipientMobile: strPtr("9400000000"),
		Notes:           &notes,
		TotalAmount:     total,
		Status:          "CUSTOMER_DRAFT",
		ValidUntil:      &validUntil,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
		Items:           items,
	}
}

func strPtr(s string) *string { return &s }
func int64Ptr(i int64) *int64 { return &i }

var pdfPageCountRe = regexp.MustCompile(`/Count (\d+)`)

func pdfPageCount(t *testing.T, data []byte) int {
	t.Helper()
	m := pdfPageCountRe.FindSubmatch(data)
	if m == nil {
		t.Fatal("could not find /Count in generated PDF")
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("bad page count: %v", err)
	}
	return n
}

func TestBuildQuotationPDF_RowCounts(t *testing.T) {
	extras := PDFContactExtras{
		NurseryEmail:     "adam@gmail.com",
		NurseryAddress:   "1-34, Antivalasa, Andhra Pradesh 500032",
		RecipientEmail:   "raju@gmail.com",
		RecipientAddress: "",
	}
	for _, n := range []int{1, 10, 30, 100, 500} {
		t.Run(fmt.Sprintf("items_%d", n), func(t *testing.T) {
			q := buildTestQuotation(n)
			pdf := buildQuotationPDF(q, "https://greenroot.app/verify/abc123", extras)

			if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) {
				t.Fatal("output does not start with a PDF header")
			}
			if !bytes.Contains(pdf, []byte("%%EOF")) {
				t.Fatal("output missing EOF trailer marker")
			}

			pages := pdfPageCount(t, pdf)
			if pages < 1 {
				t.Fatalf("expected at least 1 page, got %d", pages)
			}
			// Every item row is ~40pt; a page can't physically hold more than ~13 rows,
			// so page count must scale with item count (loose upper/lower sanity bounds).
			minExpected := n / 20 // generous — items could pack ~13/page
			if pages < minExpected {
				t.Errorf("suspiciously few pages (%d) for %d items", pages, n)
			}

			// Every row's numeric text must have been generated without an obvious
			// truncation artifact (e.g. an empty amount).
			text := string(pdf)
			if strings.Contains(text, "Rs.NaN") || strings.Contains(text, "Rs.+Inf") {
				t.Error("found invalid currency formatting in output")
			}
		})
	}
}

func TestBuildQuotationPDF_MissingOptionalFields(t *testing.T) {
	q := buildTestQuotation(3)
	q.Notes = nil
	q.RecipientName = nil // simulates manager-masked recipient
	q.RecipientMobile = nil

	pdf := buildQuotationPDF(q, "", PDFContactExtras{}) // no email/address, no verify token
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) {
		t.Fatal("output does not start with a PDF header")
	}
	if pdfPageCount(t, pdf) != 1 {
		t.Errorf("expected 1 page for a small quotation with no extras, got %d", pdfPageCount(t, pdf))
	}
}

func TestBuildQuotationPDF_LongNamesAndAddress(t *testing.T) {
	q := buildTestQuotation(2)
	q.Items[0].ScientificName = strings.Repeat("Verylongscientificplantname ", 5)
	extras := PDFContactExtras{
		NurseryEmail:   "very.long.email.address.for.testing@example-nursery-domain.com",
		NurseryAddress: strings.Repeat("Very Long Address Line, ", 6) + "500032",
	}
	pdf := buildQuotationPDF(q, "https://greenroot.app/verify/abc123", extras)
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) {
		t.Fatal("output does not start with a PDF header")
	}
	if len(pdf) == 0 {
		t.Fatal("empty PDF output")
	}
}

func TestBuildQuotationPDF_StatusBadgeVariants(t *testing.T) {
	statuses := []string{
		"INTERNAL_DRAFT", "CUSTOMER_DRAFT", "CUSTOMER_SENT",
		"CUSTOMER_ACCEPTED", "CUSTOMER_REJECTED", "CONVERTED", "UNKNOWN_STATUS",
	}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			q := buildTestQuotation(1)
			q.Status = status
			label := quotationStatusBadgeLabel(q)
			if strings.TrimSpace(label) == "" {
				t.Error("badge label must never be empty")
			}
			bg, fg := statusBadgeColors(label)
			if bg == fg {
				t.Error("badge background and text color must differ for legibility")
			}
		})
	}
}

func TestBuildQuotationPDF_ExpiredOverridesSentBadge(t *testing.T) {
	q := buildTestQuotation(1)
	q.Status = "CUSTOMER_SENT"
	expired := true
	q.ExpirySummary = &QuotationExpirySummary{IsExpired: expired}
	if got := quotationStatusBadgeLabel(q); got != "EXPIRED" {
		t.Errorf("expected EXPIRED badge for expired CUSTOMER_SENT quotation, got %q", got)
	}
}

func TestGroupIndian(t *testing.T) {
	cases := map[string]string{
		"684":     "684",
		"12500":   "12,500",
		"101275":  "1,01,275",
		"1234567": "12,34,567",
	}
	for in, want := range cases {
		if got := groupIndian(in); got != want {
			t.Errorf("groupIndian(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVerifyURLFormat(t *testing.T) {
	s := &Service{}
	url := s.verifyURL("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	if !strings.Contains(url, "/verify/") {
		t.Errorf("verifyURL must contain /verify/ segment for the mobile QR classifier, got %q", url)
	}
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		t.Errorf("verifyURL must be a full URL, got %q", url)
	}
}
