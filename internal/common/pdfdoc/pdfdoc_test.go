package pdfdoc

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAmountInWords(t *testing.T) {
	cases := map[float64]string{
		684:    "Six Hundred Eighty-Four Rupees",
		101275: "One Lakh One Thousand Two Hundred Seventy-Five Rupees",
		100:    "One Hundred Rupees",
		20:     "Twenty Rupees",
		95.5:   "Ninety-Five Rupees and Fifty Paise",
		0:      "Zero Rupees",
	}
	for in, want := range cases {
		if got := AmountInWords(in); got != want {
			t.Errorf("AmountInWords(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestINRAndGrouping(t *testing.T) {
	cases := map[float64]string{684: "Rs.684.00", 12500: "Rs.12,500.00", 101275: "Rs.1,01,275.00", 1234567.5: "Rs.12,34,567.50", -500: "-Rs.500.00"}
	for in, want := range cases {
		if got := INR(in); got != want {
			t.Errorf("INR(%v) = %q, want %q", in, got, want)
		}
	}
	if got := GroupIndian("1234567"); got != "12,34,567" {
		t.Errorf("GroupIndian = %q", got)
	}
}

func TestWidthUsesHelveticaMetrics(t *testing.T) {
	// "Hello" in Helvetica: H722 e556 l222 l222 o556 = 2278 units.
	if got := Width("Hello", 10, Regular); got != 22.78 {
		t.Errorf("Width(Hello) = %v, want 22.78", got)
	}
	if Width("Hello", 10, Bold) <= Width("Hello", 10, Regular) {
		t.Error("bold must be wider")
	}
}

func TestWrapAndFit(t *testing.T) {
	text := "Prices include loading at the nursery. Transport extra at actuals."
	lines := Wrap(text, 9, Regular, 120)
	if len(lines) < 2 {
		t.Fatalf("want several lines, got %q", lines)
	}
	for _, l := range lines {
		if Width(l, 9, Regular) > 120 {
			t.Errorf("line too wide: %q", l)
		}
	}
	if strings.Join(lines, " ") != text {
		t.Errorf("wrap lost words: %q", lines)
	}
	long := Wrap(strings.Repeat("x", 300), 9, Regular, 100) // one unbreakable word
	if len(long) < 2 {
		t.Errorf("over-long word must be split: %q", long)
	}
	if got := Fit("Rajamahendravaram, Andhra Pradesh", 9, Regular, 80); !strings.HasSuffix(got, "…") || Width(got, 9, Regular) > 80 {
		t.Errorf("Fit = %q", got)
	}
	if Wrap("   ", 9, Regular, 100) != nil {
		t.Error("blank text gives no lines")
	}
}

func TestEncodeWinAnsi(t *testing.T) {
	got := encode("₹5 · Tulsi — “ok” é")
	want := append([]byte("Rs.5 "), 0xB7, ' ', 'T', 'u', 'l', 's', 'i', ' ', 0x97, ' ', 0x93, 'o', 'k', 0x94, ' ')
	if !bytes.Equal(got, want) {
		t.Errorf("encode = %q, want %q", got, want)
	}
	if e := escape([]byte{'(', 0xB7, ')'}); e != `\(\267\)` {
		t.Errorf("escape = %q", e)
	}
}

var countRe = regexp.MustCompile(`/Count (\d+)`)

func sample(rows int) Business {
	b := Business{
		Kind: "QUOTATION", Code: "QUO-20261023-0001", Created: time.Now(), Brand: Forest,
		SellerName: "Suresh Green Nursery", SellerLines: []string{"9811100001  ·  a@b.in", "Plot 12, DLF Road, Hyderabad 500032"},
		StatusLabel: "SENT", StatusBG: GreenLight, StatusFG: Forest,
		Facts:     []KV{{"Issued", "23 Oct 2026"}, {"Valid until", "07 Nov 2026"}},
		Recipient: &Party{Label: "Quotation for", Name: "Kiran", Lines: []string{"9811100004"}},
		Columns:   []Column{{"Qty", 52}, {"Rate", 86}, {"Amount", 96}}, ItemsTitle: "Plant",
		TotalLabel: "Total", Total: 12345.5, Terms: []string{"Valid until the date above."}, Signatory: []string{"Suresh", "For Suresh Green Nursery"},
		Verify: &Verify{Title: "Verify", URL: "https://greenroot.app/verify/x", Facts: []KV{{"Quotation", "QUO-1"}}},
	}
	for i := 0; i < rows; i++ {
		b.Rows = append(b.Rows, Row{Title: fmt.Sprintf("Plant %d", i+1), Subtitle: "Plantus scientificus", Detail: "Grafted, 2 ft, black bag",
			Cells: []string{"10", "Rs.180.00", "Rs.1,800.00"}})
	}
	return b
}

func pages(t *testing.T, pdf []byte) int {
	m := countRe.FindSubmatch(pdf)
	if m == nil {
		t.Fatal("no /Count")
	}
	n, _ := strconv.Atoi(string(m[1]))
	return n
}

func TestRenderPaginates(t *testing.T) {
	b := sample(3)
	small := b.Render()
	if !bytes.HasPrefix(small, []byte("%PDF-1.4")) || !bytes.Contains(small, []byte("%%EOF")) {
		t.Fatal("not a PDF")
	}
	if n := pages(t, small); n != 1 {
		t.Errorf("3 rows: %d pages, want 1", n)
	}
	b = sample(200)
	big := b.Render()
	if n := pages(t, big); n < 8 || n > 20 {
		t.Errorf("200 rows: %d pages", n)
	}
	if !bytes.Contains(big, []byte("/Title (Quotation QUO-20261023-0001)")) {
		t.Error("document title missing")
	}
	if !bytes.Contains(big, []byte("continued")) {
		t.Error("continuation header missing")
	}
}

func TestXrefOffsetsAreExact(t *testing.T) {
	b := sample(5)
	pdf := b.Render()
	m := regexp.MustCompile(`startxref\n(\d+)`).FindSubmatch(pdf)
	off, _ := strconv.Atoi(string(m[1]))
	if !bytes.HasPrefix(pdf[off:], []byte("xref")) {
		t.Fatal("startxref does not point at the xref table")
	}
	entries := regexp.MustCompile(`(\d{10}) 00000 n `).FindAllSubmatch(pdf[off:], -1)
	for i, e := range entries {
		o, _ := strconv.Atoi(string(e[1]))
		if !bytes.HasPrefix(pdf[o:], []byte(fmt.Sprintf("%d 0 obj", i+1))) {
			t.Fatalf("object %d offset wrong", i+1)
		}
	}
}
