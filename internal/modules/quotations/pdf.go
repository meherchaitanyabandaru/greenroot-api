package quotations

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

type pdfCanvas struct {
	bytes.Buffer
}

func buildQuotationPDF(q Quotation, verifyURL string) []byte {
	brand := parseBrandColor(q.NurseryBrandColor)
	var c pdfCanvas

	// ── Header ───────────────────────────────────────────────────────────────
	c.rectFill(38, 680, 6, 128, brand) // left brand accent stripe
	c.rectFill(50, 805, 495, 3, brand) // top accent line
	c.text(50, 760, 22, true, pdfDark, textOr(q.NurseryName, "GreenRoot Quotation"))
	c.text(50, 741, 10, false, pdfMuted, textOr(q.NurseryPhone, ""))
	// Right column (quotation identity)
	c.text(395, 775, 9, true, pdfMuted, "QUOTATION")
	c.text(372, 748, 18, true, pdfDark, q.QuotationCode)
	c.meta(414, 725, "Date", formatPDFDateTime(q.CreatedAt))
	c.meta(384, 707, "Valid Until", validUntilText(q))
	c.line(50, 680, 545, 680, pdfBorder)

	// ── Party boxes ───────────────────────────────────────────────────────────
	if strings.EqualFold(q.QuotationType, "INTERNAL") {
		c.rectFill(50, 645, 495, 48, pdfSoftGreen)
		c.rectStroke(50, 645, 495, 48, brand)
		c.text(64, 674, 9, true, brand, "INTERNAL PLANNING DOCUMENT")
		c.text(64, 657, 9, false, pdfDark, "Not intended for external customer distribution.")
	} else {
		c.partyBox(50, 610, 220, "FROM", textOr(q.NurseryName, "-"), textOr(q.NurseryPhone, ""), "Prepared by "+textOr(q.CreatedByName, "-"), brand)
		c.partyBox(295, 610, 250, "TO", textOr(q.RecipientName, "Customer details protected"), textOr(q.RecipientMobile, ""), "", pdfMuted)
	}

	// ── Items table ───────────────────────────────────────────────────────────
	tableY := 560.0
	if strings.EqualFold(q.QuotationType, "INTERNAL") {
		tableY = 585
	}
	tableBottom := c.itemsTable(q, tableY, brand)

	// ── Grand total ───────────────────────────────────────────────────────────
	totalY := tableBottom - 52
	c.rectFill(50, totalY, 495, 44, pdfSoftGreen)
	c.rectStroke(50, totalY, 495, 44, brand)
	c.text(62, totalY+28, 10, true, brand, "GRAND TOTAL")
	c.text(430, totalY+26, 18, true, pdfDark, formatPDFMoney(q.TotalAmount))
	c.text(430, totalY+10, 8, false, pdfMuted, amountInWords(q.TotalAmount)+" Only")

	// ── Notes ─────────────────────────────────────────────────────────────────
	nextY := totalY - 34
	if q.Notes != nil && strings.TrimSpace(*q.Notes) != "" {
		c.rectFill(50, nextY-34, 495, 42, pdfLight)
		c.rectStroke(50, nextY-34, 495, 42, pdfBorder)
		c.text(62, nextY-8, 8, true, pdfMuted, "NOTES")
		c.text(62, nextY-24, 9, false, pdfDark, truncatePDFText(strings.TrimSpace(*q.Notes), 88))
		nextY -= 54
	}

	// ── Price disclaimer ──────────────────────────────────────────────────────
	c.rectFill(50, nextY-28, 495, 28, pdfAmberLight)
	c.rectStroke(50, nextY-28, 495, 28, pdfAmber)
	c.text(62, nextY-18, 8, true, pdfAmber, "!  Prices subject to availability. All prices are provided by the issuing nursery.")
	nextY -= 40

	// ── Document Verification section ─────────────────────────────────────────
	if verifyURL != "" && nextY-128 > 90 {
		c.verificationSection(nextY-8, brand, q, verifyURL)
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	c.line(50, 76, 545, 76, pdfBorder)
	c.text(50, 62, 8, false, pdfMuted, "Powered by GreenRoot - www.greenroot.app")
	c.text(470, 62, 8, true, pdfMuted, "Page 1 of 1")
	c.text(50, 50, 7, false, pdfMuted,
		"GreenRoot provides quotation management software only. All quotation information is provided by the issuing nursery.")

	return wrapPDF(c.String())
}

// ── Document Verification section ────────────────────────────────────────────

func (c *pdfCanvas) verificationSection(topY float64, brand pdfColor, q Quotation, verifyURL string) {
	const boxH = 120.0
	const x = 50.0
	const w = 495.0

	// Box background + left accent bar
	c.rectFill(x, topY-boxH, w, boxH, pdfLight)
	c.rectStroke(x, topY-boxH, w, boxH, pdfBorder)
	c.rectFill(x, topY-boxH, 3, boxH, brand)

	// Section title
	c.text(x+14, topY-14, 8, true, brand, "DOCUMENT VERIFICATION")
	c.line(x+14, topY-20, x+w-14, topY-20, pdfBorder)

	// QR code (left of the section)
	const qrSize = 72.0
	qrX := x + 14.0
	qrY := topY - boxH + 18.0 // bottom-left of QR square in PDF coords; 18pt reserves space for fallback text
	c.qrCode(qrX, qrY, qrSize, verifyURL)
	// Label below QR showing the quotation code
	c.text(qrX, qrY-10, 7, false, pdfMuted, q.QuotationCode)

	// Quote metadata (right of QR)
	mx := qrX + qrSize + 14.0
	c.text(mx, topY-30, 7, true, pdfMuted, "QUOTE ID")
	c.text(mx, topY-42, 9, true, pdfDark, q.QuotationCode)
	c.text(mx, topY-57, 7, true, pdfMuted, "CREATED")
	c.text(mx, topY-69, 8, false, pdfDark, formatPDFDate(q.CreatedAt))
	c.text(mx, topY-82, 7, false, pdfMuted, "Digitally generated - No physical signature required.")

	// Validated by (right column)
	var validatorName, validatorRole string
	var validatedAt time.Time
	switch {
	case q.AssignedManagerName != nil && *q.AssignedManagerName != "":
		validatorName = *q.AssignedManagerName
		validatorRole = "Nursery Manager"
		if q.SentAt != nil {
			validatedAt = *q.SentAt
		} else {
			validatedAt = q.UpdatedAt
		}
	case q.CreatedByName != nil && *q.CreatedByName != "":
		validatorName = *q.CreatedByName
		validatorRole = "Nursery"
		validatedAt = q.UpdatedAt
	}
	if validatorName != "" {
		vx := x + w/2 + 20
		c.text(vx, topY-30, 7, true, pdfMuted, "VALIDATED BY")
		c.text(vx, topY-42, 9, true, pdfDark, toPDFASCII(validatorName))
		c.text(vx, topY-56, 8, false, pdfMuted, validatorRole)
		if !validatedAt.IsZero() {
			c.text(vx, topY-68, 8, false, pdfMuted, formatPDFDate(validatedAt))
		}
	}
}

// qrCode draws a QR code as filled vector rectangles using the brand forest-green color.
// x, y is the bottom-left corner in PDF coordinates. size is the total side length in pts.
func (c *pdfCanvas) qrCode(x, y, size float64, content string) {
	qr, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return
	}
	qr.DisableBorder = true
	bmp := qr.Bitmap()
	rows := len(bmp)
	if rows == 0 {
		return
	}
	c.rectFill(x, y, size, size, pdfWhite)
	ms := size / float64(rows)
	for r, row := range bmp {
		for col, dark := range row {
			if dark {
				mx := x + float64(col)*ms
				my := y + float64(rows-1-r)*ms
				c.rectFill(mx, my, ms, ms, pdfQRGreen)
			}
		}
	}
	// Center brand marker (eco-leaf style): white square + green dot
	ctr := size / 2
	const m = 5.0
	c.rectFill(x+ctr-m, y+ctr-m, m*2, m*2, pdfWhite)
	c.rectFill(x+ctr-3.5, y+ctr-3.5, 7, 7, pdfQRGreen)
}

// ── Items table ───────────────────────────────────────────────────────────────

func (c *pdfCanvas) itemsTable(q Quotation, y float64, brand pdfColor) float64 {
	// Only show DESC column when at least one item actually has a description.
	hasDesc := false
	for _, item := range q.Items {
		if item.Description != nil && strings.TrimSpace(*item.Description) != "" {
			hasDesc = true
			break
		}
	}

	var xs []float64
	var headers []string
	if hasDesc {
		xs = []float64{50, 78, 323, 381, 433, 495}
		headers = []string{"#", "PLANT / ITEM", "DESC", "QTY", "UNIT PRICE", "AMOUNT"}
	} else {
		xs = []float64{50, 78, 381, 433, 495}
		headers = []string{"#", "PLANT / ITEM", "QTY", "UNIT PRICE", "AMOUNT"}
	}

	const hdrH = 30.0
	c.rectFill(50, y, 495, hdrH, brand)
	for i, h := range headers {
		c.text(xs[i]+6, y+11, 8, true, pdfWhite, h)
	}

	rowY := y - (hdrH + 3)
	for i, item := range q.Items {
		rowH := 40.0
		if rowY < 105 {
			break
		}
		if i%2 == 0 {
			c.rectFill(50, rowY, 495, rowH, pdfWhite)
		} else {
			c.rectFill(50, rowY, 495, rowH, pdfLight)
		}
		c.rectStroke(50, rowY, 495, rowH, pdfBorder)
		for _, x := range xs[1:] {
			c.line(x, rowY, x, rowY+rowH, pdfBorder)
		}
		c.text(58, rowY+22, 9, false, pdfMuted, fmt.Sprintf("%d", i+1))
		if hasDesc {
			c.text(86, rowY+23, 10, true, pdfDark, truncatePDFText(item.ScientificName, 34))
			if item.CommonName != nil {
				c.text(86, rowY+10, 8, false, pdfMuted, truncatePDFText(*item.CommonName, 34))
			}
			if item.Description != nil && strings.TrimSpace(*item.Description) != "" {
				c.text(331, rowY+20, 9, false, pdfMuted, truncatePDFText(toPDFASCII(strings.TrimSpace(*item.Description)), 10))
			}
		} else {
			// DESC column hidden — give extra width to plant name
			c.text(86, rowY+23, 10, true, pdfDark, truncatePDFText(item.ScientificName, 46))
			if item.CommonName != nil {
				c.text(86, rowY+10, 8, false, pdfMuted, truncatePDFText(*item.CommonName, 46))
			}
		}
		c.text(397, rowY+20, 10, false, pdfDark, formatPDFQty(item.Quantity))
		c.text(448, rowY+20, 10, false, pdfDark, formatPDFMoney(item.UnitPrice))
		c.text(504, rowY+20, 10, true, pdfDark, formatPDFMoney(item.TotalPrice))
		rowY -= rowH
	}
	return rowY
}

// ── Party box ─────────────────────────────────────────────────────────────────

func (c *pdfCanvas) partyBox(x, y, w float64, label, name, phone, foot string, labelColor pdfColor) {
	c.rectFill(x, y, w, 84, pdfLight)
	c.rectStroke(x, y, w, 84, pdfBorder)
	c.text(x+16, y+61, 9, true, labelColor, label)
	c.text(x+16, y+38, 13, true, pdfDark, truncatePDFText(name, 28))
	if phone != "" {
		c.text(x+16, y+22, 9, false, pdfMuted, phone)
	}
	if foot != "" {
		c.text(x+16, y+8, 8, false, pdfMuted, truncatePDFText(foot, 36))
	}
}

// ── Drawing primitives ────────────────────────────────────────────────────────

func (c *pdfCanvas) meta(x, y float64, label, value string) {
	c.text(x, y, 9, true, pdfMuted, label)
	c.text(x+58, y, 9, true, pdfDark, value)
}

func (c *pdfCanvas) text(x, y, size float64, bold bool, color pdfColor, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	font := "F1"
	if bold {
		font = "F2"
	}
	c.setFill(color)
	fmt.Fprintf(c, "BT /%s %.1f Tf %.1f %.1f Td (%s) Tj ET\n", font, size, x, y, escapePDFText(toPDFASCII(text)))
}

func (c *pdfCanvas) rectFill(x, y, w, h float64, color pdfColor) {
	c.setFill(color)
	fmt.Fprintf(c, "%.1f %.1f %.1f %.1f re f\n", x, y, w, h)
}

func (c *pdfCanvas) rectStroke(x, y, w, h float64, color pdfColor) {
	c.setStroke(color)
	fmt.Fprintf(c, "%.1f %.1f %.1f %.1f re S\n", x, y, w, h)
}

func (c *pdfCanvas) line(x1, y1, x2, y2 float64, color pdfColor) {
	c.setStroke(color)
	fmt.Fprintf(c, "%.1f %.1f m %.1f %.1f l S\n", x1, y1, x2, y2)
}

func (c *pdfCanvas) setFill(color pdfColor) {
	fmt.Fprintf(c, "%.3f %.3f %.3f rg\n", color.r, color.g, color.b)
}

func (c *pdfCanvas) setStroke(color pdfColor) {
	fmt.Fprintf(c, "%.3f %.3f %.3f RG\n", color.r, color.g, color.b)
}

// ── PDF structure ─────────────────────────────────────────────────────────────

func wrapPDF(stream string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 4 0 R /F2 5 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}

// ── Colors ────────────────────────────────────────────────────────────────────

type pdfColor struct{ r, g, b float64 }

var (
	pdfForest     = rgb(0x16, 0x65, 0x34)
	pdfDark       = rgb(0x1F, 0x29, 0x37)
	pdfMuted      = rgb(0x6B, 0x72, 0x80)
	pdfLight      = rgb(0xF8, 0xFA, 0xFC)
	pdfBorder     = rgb(0xE5, 0xE7, 0xEB)
	pdfSoftGreen  = rgb(0xF0, 0xFF, 0xF4)
	pdfAmber      = rgb(0xD9, 0x77, 0x06)
	pdfAmberLight = rgb(0xFE, 0xF3, 0xC7)
	pdfWhite      = rgb(0xFF, 0xFF, 0xFF)
	pdfQRGreen    = rgb(0x1A, 0x47, 0x31) // matches Flutter QR eye/module color
)

var istZone = time.FixedZone("IST", 5*60*60+30*60)

func rgb(r, g, b int) pdfColor {
	return pdfColor{float64(r) / 255, float64(g) / 255, float64(b) / 255}
}

func parseBrandColor(s *string) pdfColor {
	if s == nil || len(*s) == 0 {
		return pdfForest
	}
	clean := strings.TrimPrefix(*s, "#")
	if len(clean) != 6 {
		return pdfForest
	}
	b, err := hex.DecodeString(clean)
	if err != nil || len(b) != 3 {
		return pdfForest
	}
	return rgb(int(b[0]), int(b[1]), int(b[2]))
}

// ── Helpers ───────────────────────────────────────────────────────────────────


func validUntilText(q Quotation) string {
	if q.ValidUntil != nil {
		return q.ValidUntil.In(istZone).Format("02 Jan 2006")
	}
	return q.CreatedAt.In(istZone).Add(15 * 24 * time.Hour).Format("02 Jan 2006")
}

func escapePDFText(text string) string {
	text = strings.ReplaceAll(text, `\`, `\\`)
	text = strings.ReplaceAll(text, "(", `\(`)
	text = strings.ReplaceAll(text, ")", `\)`)
	return text
}

func toPDFASCII(text string) string {
	replacer := strings.NewReplacer(
		"₹", "Rs.",  // ₹ INDIAN RUPEE SIGN
		"—", "-",   // — EM DASH
		"–", "-",   // – EN DASH
		"·", "-",   // · MIDDLE DOT
		"•", "-",   // • BULLET
		"“", "\"",  // " LEFT DOUBLE QUOTATION MARK
		"”", "\"",  // " RIGHT DOUBLE QUOTATION MARK
		"‘", "'",   // ' LEFT SINGLE QUOTATION MARK
		"’", "'",   // ' RIGHT SINGLE QUOTATION MARK
	)
	text = replacer.Replace(text)
	var b strings.Builder
	for _, r := range text {
		if r >= 32 && r <= 126 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func formatPDFMoney(amount float64) string {
	return fmt.Sprintf("Rs.%.2f", amount)
}

func formatPDFQty(qty float64) string {
	if qty == math.Trunc(qty) {
		return fmt.Sprintf("%.0f", qty)
	}
	return fmt.Sprintf("%.2f", qty)
}

func formatPDFDateTime(t time.Time) string {
	return t.In(istZone).Format("02 Jan 2006, 3:04 PM IST")
}

func formatPDFDate(t time.Time) string {
	return t.In(istZone).Format("02 Jan 2006")
}

func truncatePDFText(text string, max int) string {
	if len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return text[:max-3] + "..."
}

func textOr(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return strings.TrimSpace(*value)
}

func amountInWords(amount float64) string {
	totalPaise := int(math.Round(amount * 100))
	rupees := totalPaise / 100
	paise := totalPaise % 100
	if rupees == 0 && paise == 0 {
		return "Zero Rupees"
	}
	parts := make([]string, 0, 2)
	if rupees > 0 {
		parts = append(parts, numberToWords(rupees)+" Rupees")
	}
	if paise > 0 {
		parts = append(parts, numberToWords(paise)+" Paise")
	}
	return strings.Join(parts, " and ")
}

func numberToWords(n int) string {
	if n == 0 {
		return "Zero"
	}
	ones := []string{"", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine",
		"Ten", "Eleven", "Twelve", "Thirteen", "Fourteen", "Fifteen", "Sixteen", "Seventeen", "Eighteen", "Nineteen"}
	tens := []string{"", "", "Twenty", "Thirty", "Forty", "Fifty", "Sixty", "Seventy", "Eighty", "Ninety"}
	var parts []string
	for _, scale := range []struct {
		value int
		name  string
	}{{10000000, "Crore"}, {100000, "Lakh"}, {1000, "Thousand"}} {
		if n >= scale.value {
			parts = append(parts, numberToWords(n/scale.value)+" "+scale.name)
			n %= scale.value
		}
	}
	if n >= 100 {
		parts = append(parts, ones[n/100]+" Hundred")
		n %= 100
	}
	if n >= 20 {
		parts = append(parts, tens[n/10])
		n %= 10
	}
	if n > 0 {
		parts = append(parts, ones[n])
	}
	return strings.Join(parts, " ")
}
