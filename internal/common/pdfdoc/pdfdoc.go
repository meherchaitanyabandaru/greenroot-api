// Package pdfdoc writes GreenRoot's business documents (quotations, orders)
// as plain PDF 1.4 with the standard Helvetica fonts: no font files, small
// output, opens everywhere. Text is measured with the real Helvetica metrics,
// so it wraps and right-aligns exactly instead of being guessed and cut.
package pdfdoc

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// A4 in points.
const (
	PageW = 595.0
	PageH = 842.0
)

type Font int

const (
	Regular Font = iota
	Bold
	Italic
)

func (f Font) name() string {
	switch f {
	case Bold:
		return "F2"
	case Italic:
		return "F3"
	}
	return "F1"
}

// ── Colours ─────────────────────────────────────────────────────────────────

type Color struct{ R, G, B float64 }

func RGB(r, g, b int) Color { return Color{float64(r) / 255, float64(g) / 255, float64(b) / 255} }

var (
	Forest     = RGB(0x16, 0x65, 0x34)
	Ink        = RGB(0x11, 0x18, 0x27)
	Dark       = RGB(0x1F, 0x29, 0x37)
	Muted      = RGB(0x6B, 0x72, 0x80)
	Faint      = RGB(0x9C, 0xA3, 0xAF)
	Light      = RGB(0xF8, 0xFA, 0xFC)
	Border     = RGB(0xE5, 0xE7, 0xEB)
	White      = RGB(0xFF, 0xFF, 0xFF)
	Amber      = RGB(0xB4, 0x53, 0x09)
	AmberLight = RGB(0xFE, 0xF3, 0xC7)
	Red        = RGB(0x99, 0x1B, 0x1B)
	RedLight   = RGB(0xFE, 0xE2, 0xE2)
	Blue       = RGB(0x1D, 0x4E, 0xD8)
	BlueLight  = RGB(0xDB, 0xEA, 0xFE)
	GreenLight = RGB(0xDC, 0xFC, 0xE7)
	Gray       = RGB(0x37, 0x41, 0x51)
	GrayLight  = RGB(0xE5, 0xE7, 0xEB)
)

// Hex parses "#RRGGBB"; anything else gives fallback.
func Hex(s *string, fallback Color) Color {
	if s == nil {
		return fallback
	}
	b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(*s), "#"))
	if err != nil || len(b) != 3 {
		return fallback
	}
	return RGB(int(b[0]), int(b[1]), int(b[2]))
}

// Tint mixes c with white: 0 = c, 1 = white. For soft panel fills.
func Tint(c Color, amount float64) Color {
	return Color{c.R + (1-c.R)*amount, c.G + (1-c.G)*amount, c.B + (1-c.B)*amount}
}

// ── Text: WinAnsi encoding and Helvetica metrics ────────────────────────────

// winAnsi maps the non-ASCII characters we use to their WinAnsi byte.
var winAnsi = map[rune]byte{
	'•': 0x95, '–': 0x96, '—': 0x97, '‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94,
	'·': 0xB7, '×': 0xD7, '…': 0x85,
}

// encode turns text into WinAnsi bytes; ₹ becomes "Rs." (Helvetica has no
// rupee glyph) and other characters it cannot show are dropped.
func encode(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r == '₹':
			out = append(out, "Rs."...)
		case r >= 32 && r <= 126:
			out = append(out, byte(r))
		case r == '\n' || r == '\t':
			out = append(out, ' ')
		default:
			if b, ok := winAnsi[r]; ok {
				out = append(out, b)
			}
		}
	}
	return out
}

// Widths per 1000 em, chars 32..126 (Adobe AFM, WinAnsi).
var helvetica = [95]int{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556,
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584,
}

var helveticaBold = [95]int{
	278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611,
	975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556,
	333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611,
	611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584,
}

var extraWidths = map[byte][2]int{ // regular, bold
	0x95: {350, 350}, 0x96: {556, 556}, 0x97: {1000, 1000}, 0x91: {222, 278}, 0x92: {222, 278},
	0x93: {333, 500}, 0x94: {333, 500}, 0xB7: {278, 278}, 0xD7: {584, 584}, 0x85: {1000, 1000},
}

// Width is the rendered width of s in points.
func Width(s string, size float64, f Font) float64 {
	units := 0
	for _, b := range encode(s) {
		switch {
		case b >= 32 && b <= 126:
			if f == Bold {
				units += helveticaBold[b-32]
			} else {
				units += helvetica[b-32]
			}
		default:
			if w, ok := extraWidths[b]; ok {
				if f == Bold {
					units += w[1]
				} else {
					units += w[0]
				}
			}
		}
	}
	return float64(units) * size / 1000
}

// Wrap breaks s into lines no wider than maxW, at spaces; a word longer
// than a line is split. Blank input gives no lines.
func Wrap(s string, size float64, f Font, maxW float64) []string {
	var lines []string
	for _, para := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		words := strings.Fields(para)
		cur := ""
		for _, w := range words {
			for Width(w, size, f) > maxW { // split an over-long word
				n := len([]rune(w))
				cut := n
				for cut > 1 && Width(string([]rune(w)[:cut]), size, f) > maxW {
					cut--
				}
				if cur != "" {
					lines = append(lines, cur)
					cur = ""
				}
				lines = append(lines, string([]rune(w)[:cut]))
				w = string([]rune(w)[cut:])
			}
			switch {
			case cur == "":
				cur = w
			case Width(cur+" "+w, size, f) <= maxW:
				cur += " " + w
			default:
				lines = append(lines, cur)
				cur = w
			}
		}
		if cur != "" {
			lines = append(lines, cur)
		}
	}
	return lines
}

// Fit shortens s with "…" until it is no wider than maxW.
func Fit(s string, size float64, f Font, maxW float64) string {
	if Width(s, size, f) <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && Width(string(r)+"…", size, f) > maxW {
		r = r[:len(r)-1]
	}
	return strings.TrimSpace(string(r)) + "…"
}

// ── Page drawing ────────────────────────────────────────────────────────────

// Page is one page's content stream. Coordinates are PDF-native: origin at
// the bottom left, y grows upward.
type Page struct{ buf bytes.Buffer }

func (p *Page) fill(c Color)   { fmt.Fprintf(&p.buf, "%.3f %.3f %.3f rg\n", c.R, c.G, c.B) }
func (p *Page) stroke(c Color) { fmt.Fprintf(&p.buf, "%.3f %.3f %.3f RG\n", c.R, c.G, c.B) }

// Text draws s with its baseline at y.
func (p *Page) Text(x, y, size float64, f Font, c Color, s string) {
	b := encode(s)
	if len(bytes.TrimSpace(b)) == 0 {
		return
	}
	p.fill(c)
	fmt.Fprintf(&p.buf, "BT /%s %.2f Tf %.2f %.2f Td (%s) Tj ET\n", f.name(), size, x, y, escape(b))
}

// Spaced draws s with extra letter spacing (small caps labels).
func (p *Page) Spaced(x, y, size, spacing float64, f Font, c Color, s string) {
	p.fill(c)
	fmt.Fprintf(&p.buf, "BT %.2f Tc /%s %.2f Tf %.2f %.2f Td (%s) Tj 0 Tc ET\n", spacing, f.name(), size, x, y, escape(encode(s)))
}

// SpacedWidth is Width plus the letter spacing Spaced adds.
func SpacedWidth(s string, size, spacing float64, f Font) float64 {
	return Width(s, size, f) + spacing*float64(len(encode(s)))
}

// TextRight draws s ending at rightX.
func (p *Page) TextRight(rightX, y, size float64, f Font, c Color, s string) {
	p.Text(rightX-Width(s, size, f), y, size, f, c, s)
}

func (p *Page) Rect(x, y, w, h float64, c Color) {
	p.fill(c)
	fmt.Fprintf(&p.buf, "%.2f %.2f %.2f %.2f re f\n", x, y, w, h)
}

func (p *Page) StrokeRect(x, y, w, h, lw float64, c Color) {
	p.stroke(c)
	fmt.Fprintf(&p.buf, "%.2f w %.2f %.2f %.2f %.2f re S\n", lw, x, y, w, h)
}

func (p *Page) Line(x1, y1, x2, y2, lw float64, c Color) {
	p.stroke(c)
	fmt.Fprintf(&p.buf, "%.2f w %.2f %.2f m %.2f %.2f l S\n", lw, x1, y1, x2, y2)
}

func (p *Page) roundPath(x, y, w, h, r float64) {
	r = min(r, w/2, h/2)
	const k = 0.55228475
	fmt.Fprintf(&p.buf, "%.2f %.2f m %.2f %.2f l %.2f %.2f %.2f %.2f %.2f %.2f c\n", x+r, y, x+w-r, y, x+w-r+k*r, y, x+w, y+r-k*r, x+w, y+r)
	fmt.Fprintf(&p.buf, "%.2f %.2f l %.2f %.2f %.2f %.2f %.2f %.2f c\n", x+w, y+h-r, x+w, y+h-r+k*r, x+w-r+k*r, y+h, x+w-r, y+h)
	fmt.Fprintf(&p.buf, "%.2f %.2f l %.2f %.2f %.2f %.2f %.2f %.2f c\n", x+r, y+h, x+r-k*r, y+h, x, y+h-r+k*r, x, y+h-r)
	fmt.Fprintf(&p.buf, "%.2f %.2f l %.2f %.2f %.2f %.2f %.2f %.2f c\n", x, y+r, x, y+r-k*r, x+r-k*r, y, x+r, y)
}

func (p *Page) RoundRect(x, y, w, h, r float64, c Color) {
	p.fill(c)
	p.roundPath(x, y, w, h, r)
	p.buf.WriteString("f\n")
}

func (p *Page) StrokeRoundRect(x, y, w, h, r, lw float64, c Color) {
	p.stroke(c)
	fmt.Fprintf(&p.buf, "%.2f w\n", lw)
	p.roundPath(x, y, w, h, r)
	p.buf.WriteString("S\n")
}

// Pill draws a rounded label ending at rightX, centred on y; returns its width.
func (p *Page) Pill(rightX, y float64, label string, bg, fg Color) float64 {
	const size, padX, h = 7.5, 8.0, 15.0
	w := SpacedWidth(label, size, 0.4, Bold) + padX*2
	p.RoundRect(rightX-w, y-h/2, w, h, h/2, bg)
	p.Spaced(rightX-w+padX, y-2.6, size, 0.4, Bold, fg, label)
	return w
}

// QR draws content as a QR code with its bottom-left corner at x, y.
func (p *Page) QR(x, y, size float64, content string, c Color) {
	qr, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return
	}
	qr.DisableBorder = true
	bmp := qr.Bitmap()
	n := len(bmp)
	if n == 0 {
		return
	}
	p.Rect(x, y, size, size, White)
	m := size / float64(n)
	p.fill(c)
	for r, row := range bmp {
		for col, dark := range row {
			if dark {
				fmt.Fprintf(&p.buf, "%.2f %.2f %.2f %.2f re\n", x+float64(col)*m, y+float64(n-1-r)*m, m+0.02, m+0.02)
			}
		}
	}
	p.buf.WriteString("f\n")
}

// Logo draws the GreenRoot horizontal logo at (x, y) with width w.
// Height is automatically calculated to preserve aspect ratio, and the
// calculated height is returned.
func (p *Page) Logo(x, y, w float64) float64 {
	h := w / LogoAspect
	fmt.Fprintf(&p.buf, "q %.2f 0 0 %.2f %.2f %.2f cm /Logo Do Q\n", w, h, x, y)
	return h
}

// LogoH draws the GreenRoot horizontal logo at (x, y) with height h.
// Width is automatically calculated to preserve aspect ratio, and the
// calculated width is returned.
func (p *Page) LogoH(x, y, h float64) float64 {
	w := h * LogoAspect
	fmt.Fprintf(&p.buf, "q %.2f 0 0 %.2f %.2f %.2f cm /Logo Do Q\n", w, h, x, y)
	return w
}

// Leaf draws the small GreenRoot leaf mark.
func (p *Page) Leaf(x, y, size float64, c Color) {
	p.fill(c)
	w := size * 0.6
	fmt.Fprintf(&p.buf, "%.2f %.2f m %.2f %.2f %.2f %.2f %.2f %.2f c %.2f %.2f %.2f %.2f %.2f %.2f c f\n",
		x, y, x+w, y+size*0.15, x+w, y+size*0.85, x, y+size, x-w*0.3, y+size*0.85, x-w*0.3, y+size*0.15, x, y)
	p.Line(x, y+size*0.12, x, y+size*0.88, 0.6, White)
}

func escape(b []byte) string {
	var s strings.Builder
	for _, c := range b {
		switch {
		case c == '\\' || c == '(' || c == ')':
			s.WriteByte('\\')
			s.WriteByte(c)
		case c > 126:
			fmt.Fprintf(&s, "\\%03o", c)
		default:
			s.WriteByte(c)
		}
	}
	return s.String()
}

// ── Document ────────────────────────────────────────────────────────────────

type Doc struct {
	Pages   []*Page
	Title   string
	Author  string
	Subject string
	Created time.Time
}

func (d *Doc) NewPage() *Page {
	p := &Page{}
	d.Pages = append(d.Pages, p)
	return p
}

// Bytes writes the PDF.
func (d *Doc) Bytes() []byte {
	if len(d.Pages) == 0 {
		d.NewPage()
	}
	n := len(d.Pages)
	// 1 catalog, 2 pages, 3-5 fonts, 6 info, 7 logo XObject, then pages, then contents.
	const logoObjID = 7
	const pageStart = 8
	contentStart := pageStart + n
	font := func(base string) string {
		return fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /%s /Encoding /WinAnsiEncoding >>", base)
	}
	kids := make([]string, n)
	for i := range kids {
		kids[i] = fmt.Sprintf("%d 0 R", pageStart+i)
	}
	created := d.Created
	if created.IsZero() {
		created = time.Now()
	}
	var objects [][]byte
	objects = append(objects,
		[]byte("<< /Type /Catalog /Pages 2 0 R /ViewerPreferences << /DisplayDocTitle true >> >>"),
		[]byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n)),
		[]byte(font("Helvetica")),
		[]byte(font("Helvetica-Bold")),
		[]byte(font("Helvetica-Oblique")),
		[]byte(fmt.Sprintf("<< /Title (%s) /Author (%s) /Subject (%s) /Creator (GreenRoot) /Producer (GreenRoot) /CreationDate (%s) >>",
			escape(encode(d.Title)), escape(encode(d.Author)), escape(encode(d.Subject)), pdfDate(created))),
	)

	// 7: GreenRoot Logo Image XObject
	var logoObj bytes.Buffer
	fmt.Fprintf(&logoObj, "<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n",
		logoWidth, logoHeight, len(logoJPEG))
	logoObj.Write(logoJPEG)
	logoObj.WriteString("\nendstream")
	objects = append(objects, logoObj.Bytes())

	// Pages
	for i := 0; i < n; i++ {
		pageObj := fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 3 0 R /F2 4 0 R /F3 5 0 R >> /XObject << /Logo %d 0 R >> >> /Contents %d 0 R >>",
			PageW, PageH, logoObjID, contentStart+i)
		objects = append(objects, []byte(pageObj))
	}

	// Page contents
	for _, p := range d.Pages {
		var contentObj bytes.Buffer
		fmt.Fprintf(&contentObj, "<< /Length %d >>\nstream\n", p.buf.Len())
		contentObj.Write(p.buf.Bytes())
		contentObj.WriteString("\nendstream")
		objects = append(objects, contentObj.Bytes())
	}

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n", i+1)
		out.Write(obj)
		out.WriteString("\nendobj\n")
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R /Info 6 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}

func pdfDate(t time.Time) string {
	t = t.In(IST)
	return t.Format("D:20060102150405") + "+05'30'"
}
