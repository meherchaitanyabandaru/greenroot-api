package pdfdoc

import (
	"fmt"
	"strings"
	"time"
)

// Business is a quotation or order, already filtered for what may be printed.
// Render lays it out on as many A4 pages as it needs.
type Business struct {
	Kind    string // "QUOTATION", "ORDER"
	Code    string
	Created time.Time
	Brand   Color

	SellerName  string
	SellerLines []string // phone · email, address — wrapped under the name

	StatusLabel  string
	StatusBG     Color
	StatusFG     Color
	Facts        []KV     // the strip under the header: Date, Valid until, …
	Banner       *Callout // e.g. "Internal planning document"
	Recipient    *Party
	Columns      []Column // numeric columns after "#" and the item column
	ItemsTitle   string   // header of the item column
	Rows         []Row
	ItemsSummary string // under the table, left: "14 items · 538 plants"
	Totals       []KV   // above the grand total: Ordered value, Not loaded, …
	TotalLabel   string
	Total        float64
	Callouts     []Callout
	Verify       *Verify
	Terms        []string
	Signatory    []string // first line bold: who issues it
	FooterNote   string
}

type KV struct{ Label, Value string }

type Party struct {
	Label string
	Name  string
	Lines []string
}

type Column struct {
	Title string
	Width float64
}

type Row struct {
	Title    string   // common name, bold
	Subtitle string   // scientific name, italic
	Detail   string   // description / size, wrapped
	Cells    []string // one per Column, right-aligned
	Muted    []bool   // optional: draw a cell in muted colour
}

type Callout struct {
	Title string
	Text  string
	Tone  string // "warn", "danger", "info"
}

type Verify struct {
	Title    string
	Subtitle string
	URL      string
	Facts    []KV
}

// ── Layout constants ────────────────────────────────────────────────────────

const (
	left     = 40.0
	right    = PageW - 40
	contentW = right - left
	flowTop  = PageH - 92 // continuation pages
	flowEnd  = 72.0       // keep clear of the footer
	numColW  = 24.0
)

type flow struct {
	d    *Doc
	b    *Business
	page *Page
	y    float64
}

// need moves to a new page when h points don't fit; reports whether it did.
func (f *flow) need(h float64) bool {
	if f.y-h >= flowEnd {
		return false
	}
	f.page = f.d.NewPage()
	f.y = f.b.continuationHeader(f.page)
	return true
}

// Render returns the PDF.
func (b *Business) Render() []byte {
	d := &Doc{
		Title:   strings.TrimSpace(titleCase(b.Kind) + " " + b.Code),
		Author:  b.SellerName,
		Subject: Join(" · ", b.SellerName, b.Code),
		Created: b.Created,
	}
	f := &flow{d: d, b: b, page: d.NewPage()}
	f.y = b.firstHeader(f.page)
	b.drawFacts(f)
	if b.Banner != nil {
		b.drawCallout(f, *b.Banner)
	}
	if b.Recipient != nil {
		b.drawParty(f, *b.Recipient)
	}
	b.drawItems(f)
	b.drawTotals(f)
	for _, c := range b.Callouts {
		b.drawCallout(f, c)
	}
	if b.Verify != nil {
		b.drawVerify(f, *b.Verify)
	}
	b.drawTermsAndSignatory(f)
	for i, p := range d.Pages {
		b.footer(p, i+1, len(d.Pages))
	}
	return d.Bytes()
}

// ── Header ──────────────────────────────────────────────────────────────────

func (b *Business) firstHeader(p *Page) float64 {
	p.Rect(0, PageH-6, PageW, 6, b.Brand)

	// Right: kind, code, status.
	p.Spaced(right-SpacedWidth(b.Kind, 9, 1.6, Bold), PageH-44, 9, 1.6, Bold, b.Brand, b.Kind)
	p.TextRight(right, PageH-64, 15, Bold, Ink, b.Code)
	if b.StatusLabel != "" {
		p.Pill(right, PageH-82, b.StatusLabel, b.StatusBG, b.StatusFG)
	}

	// Left: seller identity, wrapped to the space the right block leaves.
	nameW := contentW - max(Width(b.Code, 15, Bold), 150) - 24
	y := PageH - 50
	for i, line := range Wrap(b.SellerName, 19, Bold, nameW) {
		if i == 2 {
			break
		}
		p.Text(left, y, 19, Bold, Ink, line)
		y -= 22
	}
	y += 6
	for _, s := range b.SellerLines {
		for _, line := range Wrap(s, 8.5, Regular, nameW) {
			y -= 12
			p.Text(left, y, 8.5, Regular, Muted, line)
		}
	}
	bottom := min(y, PageH-92) - 14
	p.Line(left, bottom, right, bottom, 0.6, Border)
	return bottom - 14
}

func (b *Business) continuationHeader(p *Page) float64 {
	p.Rect(0, PageH-6, PageW, 6, b.Brand)
	p.Text(left, PageH-36, 11, Bold, Ink, Fit(b.SellerName, 11, Bold, contentW/2))
	label := titleCase(b.Kind) + " " + b.Code + " · continued"
	p.TextRight(right, PageH-36, 8.5, Regular, Muted, label)
	p.Line(left, PageH-48, right, PageH-48, 0.6, Border)
	return PageH - 64
}

func (b *Business) footer(p *Page, n, total int) {
	p.Line(left, 50, right, 50, 0.5, Border)
	p.Leaf(left+3, 31, 9, Forest)
	p.Text(left+13, 34, 7.5, Bold, Dark, "GreenRoot")
	p.Text(left+13+Width("GreenRoot", 7.5, Bold)+4, 34, 7.5, Regular, Muted, "· greenroot.app")
	p.TextRight(right, 34, 7.5, Regular, Muted, fmt.Sprintf("%s  ·  Page %d of %d", b.Code, n, total))
	if b.FooterNote != "" {
		p.Text(left, 22, 6.5, Regular, Faint, Fit(b.FooterNote, 6.5, Regular, contentW))
	}
}

// ── Facts strip, party, callouts ────────────────────────────────────────────

func (b *Business) drawFacts(f *flow) {
	if len(b.Facts) == 0 {
		return
	}
	const h = 40.0
	f.need(h)
	top := f.y
	f.page.RoundRect(left, top-h, contentW, h, 6, Light)
	colW := contentW / float64(len(b.Facts))
	for i, kv := range b.Facts {
		x := left + 14 + float64(i)*colW
		if i > 0 {
			f.page.Line(left+float64(i)*colW, top-h+9, left+float64(i)*colW, top-9, 0.5, Border)
		}
		f.page.Spaced(x, top-15, 6.5, 0.8, Bold, Muted, strings.ToUpper(kv.Label))
		f.page.Text(x, top-30, 9.5, Bold, Dark, Fit(kv.Value, 9.5, Bold, colW-22))
	}
	f.y = top - h - 14
}

func (b *Business) drawParty(f *flow, party Party) {
	const pad = 14.0
	var lines []string
	for _, s := range party.Lines {
		lines = append(lines, Wrap(s, 8.5, Regular, contentW-pad*2)...)
	}
	h := 22 + 16 + float64(len(lines))*12 + 10
	f.need(h)
	top := f.y
	f.page.StrokeRoundRect(left, top-h, contentW, h, 6, 0.6, Border)
	f.page.Rect(left, top-h+6, 3, h-12, b.Brand)
	f.page.Spaced(left+pad, top-17, 6.5, 0.8, Bold, b.Brand, strings.ToUpper(party.Label))
	f.page.Text(left+pad, top-33, 11.5, Bold, Ink, Fit(party.Name, 11.5, Bold, contentW-pad*2))
	y := top - 33
	for _, l := range lines {
		y -= 12
		f.page.Text(left+pad, y, 8.5, Regular, Muted, l)
	}
	f.y = top - h - 16
}

func toneColors(tone string, brand Color) (bg, fg Color) {
	switch tone {
	case "warn":
		return AmberLight, Amber
	case "danger":
		return RedLight, Red
	}
	return Tint(brand, 0.92), brand
}

func (b *Business) drawCallout(f *flow, c Callout) {
	bg, fg := toneColors(c.Tone, b.Brand)
	const pad = 12.0
	lines := Wrap(c.Text, 8.5, Regular, contentW-pad*2)
	h := 10 + float64(len(lines))*11.5 + 8
	if c.Title != "" {
		h += 13
	}
	f.need(h)
	top := f.y
	f.page.RoundRect(left, top-h, contentW, h, 6, bg)
	y := top - 6
	if c.Title != "" {
		y -= 11
		f.page.Spaced(left+pad, y, 6.5, 0.8, Bold, fg, strings.ToUpper(c.Title))
		y -= 2
	}
	for _, l := range lines {
		y -= 11.5
		f.page.Text(left+pad, y, 8.5, Regular, Dark, l)
	}
	f.y = top - h - 12
}

// ── Items table ─────────────────────────────────────────────────────────────

func (b *Business) itemColW() float64 {
	w := contentW - numColW
	for _, c := range b.Columns {
		w -= c.Width
	}
	return w
}

func (b *Business) tableHeader(f *flow) {
	const h = 22.0
	top := f.y
	f.page.RoundRect(left, top-h, contentW, h, 4, b.Brand)
	ty := top - 14.5
	f.page.Spaced(left+8, ty, 6.5, 0.6, Bold, White, "#")
	f.page.Spaced(left+numColW+6, ty, 6.5, 0.6, Bold, White, strings.ToUpper(b.ItemsTitle))
	x := left + numColW + b.itemColW()
	for _, c := range b.Columns {
		x += c.Width
		t := strings.ToUpper(c.Title)
		f.page.Spaced(x-8-SpacedWidth(t, 6.5, 0.6, Bold), ty, 6.5, 0.6, Bold, White, t)
	}
	f.y = top - h
}

func (b *Business) rowLayout(r Row) (title, sub, detail []string, h float64) {
	w := b.itemColW() - 12
	title = Wrap(r.Title, 9.5, Bold, w)
	sub = Wrap(r.Subtitle, 8, Italic, w)
	detail = Wrap(r.Detail, 8, Regular, w)
	h = 9 + float64(len(title))*12 + float64(len(sub))*10.5 + float64(len(detail))*10.5 + 7
	return title, sub, detail, max(h, 30)
}

func (b *Business) drawItems(f *flow) {
	f.need(22 + 30)
	b.tableHeader(f)
	for i, r := range b.Rows {
		title, sub, detail, h := b.rowLayout(r)
		if f.need(h) {
			b.tableHeader(f)
		}
		top := f.y
		if i%2 == 1 {
			f.page.Rect(left, top-h, contentW, h, Light)
		}
		f.page.Line(left, top-h, right, top-h, 0.5, Border)

		first := top - 9 - 9.5
		f.page.Text(left+8, first, 8.5, Regular, Faint, fmt.Sprint(i+1))
		x := left + numColW + 6
		y := first
		for _, l := range title {
			f.page.Text(x, y, 9.5, Bold, Ink, l)
			y -= 12
		}
		y += 1.5
		for _, l := range sub {
			f.page.Text(x, y, 8, Italic, Muted, l)
			y -= 10.5
		}
		for _, l := range detail {
			f.page.Text(x, y, 8, Regular, Muted, l)
			y -= 10.5
		}

		cx := left + numColW + b.itemColW()
		for j, c := range b.Columns {
			cx += c.Width
			if j >= len(r.Cells) {
				break
			}
			font, color := Regular, Dark
			if j == len(b.Columns)-1 {
				font = Bold
			}
			if j < len(r.Muted) && r.Muted[j] {
				color = Muted
			}
			f.page.TextRight(cx-8, first, 9.5, font, color, r.Cells[j])
		}
		f.y = top - h
	}
	f.y -= 14
}

// ── Totals ──────────────────────────────────────────────────────────────────

func (b *Business) drawTotals(f *flow) {
	const boxW = 250.0
	words := Wrap("Amount in words: "+AmountInWords(b.Total)+" Only", 8, Italic, contentW-boxW-24)
	h := float64(len(b.Totals))*16 + 40
	h = max(h, 16+float64(len(words))*10.5+20)
	f.need(h)
	top := f.y
	x := right - boxW

	y := top - 2
	for _, kv := range b.Totals {
		y -= 14
		f.page.Text(x+12, y, 9, Regular, Muted, kv.Label)
		f.page.TextRight(right-12, y, 9, Regular, Dark, kv.Value)
	}
	if len(b.Totals) > 0 {
		y -= 8
	}
	f.page.RoundRect(x, y-34, boxW, 34, 6, b.Brand)
	f.page.Spaced(x+12, y-21, 7.5, 1, Bold, White, strings.ToUpper(b.TotalLabel))
	f.page.TextRight(right-12, y-22.5, 15, Bold, White, INR(b.Total))

	ly := top - 14
	if b.ItemsSummary != "" {
		f.page.Text(left, ly, 9, Bold, Dark, b.ItemsSummary)
		ly -= 14
	}
	for _, l := range words {
		f.page.Text(left, ly, 8, Italic, Muted, l)
		ly -= 10.5
	}
	f.y = min(y-34, ly) - 16
}

// ── Verify, terms, signatory ────────────────────────────────────────────────

func (b *Business) drawVerify(f *flow, v Verify) {
	const h, qr = 104.0, 76.0
	f.need(h)
	top := f.y
	f.page.RoundRect(left, top-h, contentW, h, 6, Light)
	f.page.QR(left+14, top-h+14, qr, v.URL, Ink)

	x := left + 14 + qr + 18
	f.page.Spaced(x, top-20, 7, 0.8, Bold, b.Brand, strings.ToUpper(v.Title))
	for i, l := range Wrap(v.Subtitle, 8, Regular, right-x-14) {
		f.page.Text(x, top-34-float64(i)*10.5, 8, Regular, Muted, l)
	}
	colW := (right - x - 14) / 3
	for i, kv := range v.Facts {
		cx := x + float64(i%3)*colW
		cy := top - 64 - float64(i/3)*26
		f.page.Spaced(cx, cy, 6, 0.6, Bold, Faint, strings.ToUpper(kv.Label))
		f.page.Text(cx, cy-12, 9, Bold, Dark, Fit(kv.Value, 9, Bold, colW-8))
	}
	f.y = top - h - 14
}

func (b *Business) drawTermsAndSignatory(f *flow) {
	if len(b.Terms) == 0 && len(b.Signatory) == 0 {
		return
	}
	const sigW = 170.0
	termsW := contentW - sigW - 20
	var lines []string
	for i, t := range b.Terms {
		for j, l := range Wrap(t, 7.5, Regular, termsW-14) {
			prefix := "    "
			if j == 0 {
				prefix = fmt.Sprintf("%d.  ", i+1)
			}
			lines = append(lines, prefix+l)
		}
	}
	h := max(20+float64(len(lines))*10.5, 20+float64(len(b.Signatory))*12+30) + 6
	f.need(h)
	top := f.y
	f.page.Spaced(left, top-10, 6.5, 0.8, Bold, b.Brand, "TERMS & CONDITIONS")
	y := top - 10
	for _, l := range lines {
		y -= 10.5
		f.page.Text(left, y, 7.5, Regular, Muted, l)
	}

	sx := right - sigW
	f.page.Spaced(sx, top-10, 6.5, 0.8, Bold, b.Brand, "ISSUED BY")
	sy := top - 26
	for i, s := range b.Signatory {
		font, size, color := Regular, 8.0, Muted
		if i == 0 {
			font, size, color = Bold, 10, Ink
		}
		f.page.Text(sx, sy, size, font, color, Fit(s, size, font, sigW))
		sy -= 12
	}
	f.page.Line(sx, sy-6, right, sy-6, 0.6, Border)
	f.page.Text(sx, sy-17, 6.5, Italic, Faint, "Digitally generated - no physical signature required")
	f.y = top - h
}

func titleCase(s string) string {
	s = strings.ToLower(s)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
