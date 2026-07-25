package quotations

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

type pdfCanvas struct {
	bytes.Buffer
}

// ── Page geometry ────────────────────────────────────────────────────────────
// All Y coordinates are PDF-native (origin bottom-left, increasing upward) on an
// A4 page (595 x 842 pt). "tableY" parameters follow the convention used by
// itemsTableChunk: they are the BOTTOM edge of the green header bar; rows are
// drawn below that.

const (
	partyBoxTop       = 674.0 // fixed top edge of the FROM/TO boxes (page 1 only) -- just below the header divider
	p1InternalTableY  = 566.0 // item table Y for INTERNAL-type quotations (banner instead of party boxes)
	contTableY        = 630.0 // item table Y on continuation pages
	contLabelY        = 670.0 // "ITEMS - CONTINUED" baseline on continuation pages
	summaryOnlyTableY = 630.0 - itemHdrH // where a dedicated summary-only page starts its content
	itemRowH          = 40.0
	itemHdrH          = 33.0 // 30pt header bar + 3pt gap before first row
	itemsBottomStop   = 105.0
	pageBottomMargin  = 90.0
	verificationBoxH  = 134.0
)

func buildQuotationPDF(q Quotation, verifyURL string, extras PDFContactExtras) []byte {
	brand := parseBrandColor(q.NurseryBrandColor)
	hasDesc := false
	for _, item := range q.Items {
		if item.Description != nil && strings.TrimSpace(*item.Description) != "" {
			hasDesc = true
			break
		}
	}

	chunks := planPages(q, extras, brand, verifyURL)
	totalPages := len(chunks)

	pageContents := make([]string, 0, totalPages)
	for i, chunk := range chunks {
		var c pdfCanvas
		drawPageHeader(&c, q, brand, i+1, totalPages)

		var tableBottom float64
		switch {
		case chunk.isFirst:
			tableBottom = drawFirstPageBody(&c, q, extras, brand, hasDesc, chunk.items)
		case len(chunk.items) > 0:
			c.text(50, contLabelY, 11, true, brand, "ITEMS - CONTINUED")
			tableBottom = c.itemsTableChunk(chunk.items, chunk.startIndex, contTableY, brand, hasDesc)
		default:
			tableBottom = summaryOnlyTableY
		}

		if chunk.hasSummary {
			drawSummaryChain(&c, tableBottom, q, brand, verifyURL)
		}

		drawFooter(&c, i+1, totalPages)
		pageContents = append(pageContents, c.String())
	}

	return wrapMultiPagePDF(pageContents)
}

// ── Pagination planning ──────────────────────────────────────────────────────

type pageChunk struct {
	items      []QuotationItem
	startIndex int // 1-based item number of the first item in this chunk
	isFirst    bool
	hasSummary bool
}

// planPages partitions items across pages using the exact same Y-coordinate math
// the render pass uses, then decides whether the totals/verification/terms block
// fits on the last items page or needs a dedicated trailing page.
func planPages(q Quotation, extras PDFContactExtras, brand pdfColor, verifyURL string) []pageChunk {
	items := q.Items
	startY := firstPageTableY(q, extras)

	var chunks []pageChunk
	idx := 0
	first := true
	for {
		tableY := contTableY
		if first {
			tableY = startY
		}
		rowY := tableY - itemHdrH
		startIdx := idx
		for idx < len(items) && rowY >= itemsBottomStop {
			rowY -= itemRowH
			idx++
		}
		chunks = append(chunks, pageChunk{items: items[startIdx:idx], startIndex: startIdx + 1, isFirst: first})
		first = false
		if idx >= len(items) || len(chunks) > 500 {
			break
		}
	}

	last := &chunks[len(chunks)-1]
	lastTableY := contTableY
	if last.isFirst {
		lastTableY = startY
	}
	lastTableBottom := lastTableY - itemHdrH - float64(len(last.items))*itemRowH

	required := summaryBlockHeight(q, brand, verifyURL)
	if lastTableBottom-required < pageBottomMargin {
		chunks = append(chunks, pageChunk{isFirst: false, hasSummary: true})
	} else {
		last.hasSummary = true
	}
	return chunks
}

// firstPageTableY returns the item-table Y for page 1, which depends on how many
// contact lines the FROM/TO boxes need (email/address grow the box downward).
func firstPageTableY(q Quotation, extras PDFContactExtras) float64 {
	if strings.EqualFold(q.QuotationType, "INTERNAL") {
		return p1InternalTableY
	}
	fromLines, toLines := partyLinesFor(q, extras)
	maxLines := len(fromLines)
	if len(toLines) > maxLines {
		maxLines = len(toLines)
	}
	return partyBoxTop - partyBoxHeight(maxLines) - 50
}

func partyLinesFor(q Quotation, extras PDFContactExtras) (fromLines, toLines []string) {
	fromLines = buildLines(textOr(q.NurseryPhone, ""), extras.NurseryEmail, extras.NurseryAddress, "Prepared by "+textOr(q.CreatedByName, "-"))
	if q.RecipientName != nil {
		toLines = buildLines(textOr(q.RecipientMobile, ""), extras.RecipientEmail, extras.RecipientAddress)
	}
	return
}

func buildLines(vals ...string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

func partyBoxHeight(lineCount int) float64 {
	return 46 + float64(lineCount)*14 + 10
}

// summaryBlockHeight measures the totals/notes/disclaimer/verification/terms block by
// rendering it into a throwaway canvas — this guarantees the measurement can never
// drift from what actually gets drawn.
func summaryBlockHeight(q Quotation, brand pdfColor, verifyURL string) float64 {
	var scratch pdfCanvas
	const anchor = 1000.0
	bottom := drawSummaryChain(&scratch, anchor, q, brand, verifyURL)
	return anchor - bottom
}

// ── Header (repeated on every page) ──────────────────────────────────────────

const (
	metaLabelX = 395.0
	metaValueX = 465.0
)

func drawPageHeader(c *pdfCanvas, q Quotation, brand pdfColor, pageNum, totalPages int) {
	c.rectFill(38, 680, 6, 128, brand) // left brand accent stripe
	c.rectFill(50, 805, 495, 3, brand) // top accent line
	c.text(50, 760, 22, true, pdfDark, textOr(q.NurseryName, "GreenRoot Quotation"))
	c.text(50, 741, 10, false, pdfMuted, textOr(q.NurseryPhone, ""))

	c.textRightAligned(545, 775, 9, true, pdfMuted, "QUOTATION")
	c.textRightAligned(545, 748, 18, true, pdfDark, q.QuotationCode)

	c.text(metaLabelX, 728, 8, true, pdfMuted, "Status")
	c.statusBadge(545, 728, quotationStatusBadgeLabel(q))
	c.metaRow(712, "Date", formatPDFDateTime(q.CreatedAt))
	c.metaRow(696, "Valid Until", validUntilText(q))

	c.line(50, 680, 545, 680, pdfBorder)
}

func (c *pdfCanvas) metaRow(y float64, label, value string) {
	c.text(metaLabelX, y, 8, true, pdfMuted, label)
	c.text(metaValueX, y, 8, true, pdfDark, value)
}

// ── First page body: party boxes / internal banner + item table ─────────────

func drawFirstPageBody(c *pdfCanvas, q Quotation, extras PDFContactExtras, brand pdfColor, hasDesc bool, items []QuotationItem) float64 {
	if strings.EqualFold(q.QuotationType, "INTERNAL") {
		bannerBottom := partyBoxTop - 48
		c.rectFill(50, bannerBottom, 495, 48, pdfSoftGreen)
		c.rectStroke(50, bannerBottom, 495, 48, brand)
		c.text(64, partyBoxTop-20, 9, true, brand, "INTERNAL PLANNING DOCUMENT")
		c.text(64, partyBoxTop-37, 9, false, pdfDark, "Not intended for external customer distribution.")
		return c.itemsTableChunk(items, 1, p1InternalTableY, brand, hasDesc)
	}

	fromLines, toLines := partyLinesFor(q, extras)
	maxLines := len(fromLines)
	if len(toLines) > maxLines {
		maxLines = len(toLines)
	}
	h := partyBoxHeight(maxLines)
	c.partyBox(50, partyBoxTop, 220, h, "FROM", textOr(q.NurseryName, "-"), fromLines, brand)
	c.partyBox(295, partyBoxTop, 250, h, "TO", textOr(q.RecipientName, "Customer details protected"), toLines, pdfMuted)

	tableY := partyBoxTop - h - 50
	return c.itemsTableChunk(items, 1, tableY, brand, hasDesc)
}

func (c *pdfCanvas) partyBox(x, top, w, h float64, label, name string, lines []string, labelColor pdfColor) {
	bottom := top - h
	c.rectFill(x, bottom, w, h, pdfLight)
	c.rectStroke(x, bottom, w, h, pdfBorder)
	c.text(x+16, top-23, 9, true, labelColor, label)
	c.text(x+16, top-46, 13, true, pdfDark, truncatePDFText(name, 30))
	y := top - 46
	for _, line := range lines {
		y -= 14
		c.text(x+16, y, 8, false, pdfMuted, truncatePDFText(line, 46))
	}
}

// ── Items table ───────────────────────────────────────────────────────────────

func (c *pdfCanvas) itemsTableChunk(items []QuotationItem, startIndex int, y float64, brand pdfColor, hasDesc bool) float64 {
	var xs []float64
	var headers []string
	qtyColRight, priceColRight := 379.0, 459.0
	if hasDesc {
		xs = []float64{50, 78, 300, 350, 395, 465}
		headers = []string{"#", "PLANT / ITEM", "DESC", "QTY", "UNIT PRICE", "AMOUNT"}
		qtyColRight = 389.0
	} else {
		xs = []float64{50, 78, 340, 385, 465}
		headers = []string{"#", "PLANT / ITEM", "QTY", "UNIT PRICE", "AMOUNT"}
	}
	// Right-align the three trailing numeric headers so they sit flush over their
	// right-aligned data columns instead of hanging off the left edge.
	headerRightX := map[string]float64{"QTY": qtyColRight, "UNIT PRICE": priceColRight, "AMOUNT": 539}

	const hdrH = 30.0
	c.rectFill(50, y, 495, hdrH, brand)
	for i, h := range headers {
		if rx, ok := headerRightX[h]; ok {
			c.textRightAligned(rx, y+11, 8, true, pdfWhite, h)
		} else {
			c.text(xs[i]+6, y+11, 8, true, pdfWhite, h)
		}
	}

	rowY := y - itemHdrH
	for i, item := range items {
		rowH := itemRowH
		if i%2 == 0 {
			c.rectFill(50, rowY, 495, rowH, pdfWhite)
		} else {
			c.rectFill(50, rowY, 495, rowH, pdfLight)
		}
		c.rectStroke(50, rowY, 495, rowH, pdfBorder)
		for _, x := range xs[1:] {
			c.line(x, rowY, x, rowY+rowH, pdfBorder)
		}
		c.text(58, rowY+22, 9, false, pdfMuted, fmt.Sprintf("%d", startIndex+i))
		if hasDesc {
			c.text(86, rowY+23, 10, true, pdfDark, truncatePDFText(item.ScientificName, 29))
			if item.CommonName != nil {
				c.text(86, rowY+10, 8, false, pdfMuted, truncatePDFText(*item.CommonName, 29))
			}
			if item.Description != nil && strings.TrimSpace(*item.Description) != "" {
				c.text(308, rowY+20, 9, false, pdfMuted, truncatePDFText(toPDFASCII(strings.TrimSpace(*item.Description)), 9))
			}
		} else {
			c.text(86, rowY+23, 10, true, pdfDark, truncatePDFText(item.ScientificName, 40))
			if item.CommonName != nil {
				c.text(86, rowY+10, 8, false, pdfMuted, truncatePDFText(*item.CommonName, 40))
			}
		}
		c.textRightAligned(qtyColRight, rowY+20, 10, false, pdfDark, formatPDFQty(item.Quantity))
		c.textRightAligned(priceColRight, rowY+20, 10, false, pdfDark, FormatINR(item.UnitPrice))
		c.textRightAligned(539, rowY+20, 10, true, pdfDark, FormatINR(item.TotalPrice))
		rowY -= rowH
	}
	return rowY
}

// ── Summary chain: total → notes → disclaimer → verification → terms ────────

func drawSummaryChain(c *pdfCanvas, topY float64, q Quotation, brand pdfColor, verifyURL string) float64 {
	totalY := topY - 52
	c.rectFill(50, totalY, 495, 44, pdfSoftGreen)
	c.rectStroke(50, totalY, 495, 44, brand)
	c.text(62, totalY+28, 10, true, brand, "GRAND TOTAL")
	c.textRightAligned(533, totalY+26, 18, true, pdfDark, FormatINR(q.TotalAmount))
	c.textRightAligned(533, totalY+10, 8, false, pdfMuted, amountInWords(q.TotalAmount)+" Only")

	nextY := totalY - 34
	if q.Notes != nil && strings.TrimSpace(*q.Notes) != "" {
		c.rectFill(50, nextY-34, 495, 42, pdfLight)
		c.rectStroke(50, nextY-34, 495, 42, pdfBorder)
		c.text(62, nextY-8, 8, true, pdfMuted, "NOTES")
		c.text(62, nextY-24, 9, false, pdfDark, truncatePDFText(strings.TrimSpace(*q.Notes), 88))
		nextY -= 54
	}

	c.rectFill(50, nextY-28, 495, 28, pdfAmberLight)
	c.rectStroke(50, nextY-28, 495, 28, pdfAmber)
	c.text(62, nextY-18, 8, true, pdfAmber, "!  Prices subject to availability. All prices are provided by the issuing nursery.")
	nextY -= 40

	if verifyURL != "" {
		c.verificationSection(nextY-8, brand, q, verifyURL)
		nextY -= verificationBoxH + 8
	}

	nextY -= 12
	return c.termsAndAuthBlock(nextY, brand, q)
}

func (c *pdfCanvas) termsAndAuthBlock(topY float64, brand pdfColor, q Quotation) float64 {
	const h = 130.0
	const x = 50.0
	const w = 495.0
	bottom := topY - h
	c.rectFill(x, bottom, w, h, pdfWhite)
	c.rectStroke(x, bottom, w, h, pdfBorder)
	colX := x + 315.0
	c.line(colX, bottom, colX, topY, pdfBorder)

	c.text(x+14, topY-16, 8, true, brand, "TERMS & CONDITIONS")
	terms := []string{
		"1. This quotation is valid until the date shown above.",
		"2. Prices and quantities are subject to stock availability at confirmation.",
		"3. Once sent, the quotation is locked; the nursery can recall it to make edits.",
		"4. Accepted quotations may be converted into an order by the nursery.",
		"5. Accepting or rejecting requires the buyer to be logged in (OTP verified).",
		"6. System-generated document - a physical signature is not required.",
	}
	ty := topY - 30
	for _, t := range terms {
		c.text(x+14, ty, 7, false, pdfMuted, t)
		ty -= 13
	}

	ax := colX + 16
	c.text(ax, topY-16, 8, true, brand, "AUTHORIZED BY")
	c.text(ax, topY-32, 10, true, pdfDark, textOr(q.CreatedByName, "-"))
	c.text(ax, topY-46, 8, false, pdfMuted, "For "+textOr(q.NurseryName, "GreenRoot"))
	c.text(ax, topY-62, 7, false, pdfMuted, "Digitally generated via GreenRoot")

	return bottom
}

// ── Document Verification section ────────────────────────────────────────────

func (c *pdfCanvas) verificationSection(topY float64, brand pdfColor, q Quotation, verifyURL string) {
	const boxH = verificationBoxH
	const x = 50.0
	const w = 495.0

	// Box background + left accent bar
	c.rectFill(x, topY-boxH, w, boxH, pdfLight)
	c.rectStroke(x, topY-boxH, w, boxH, pdfBorder)
	c.rectFill(x, topY-boxH, 3, boxH, brand)

	// Section title + purpose line
	c.text(x+14, topY-14, 8, true, brand, "VERIFY THIS QUOTATION")
	c.line(x+14, topY-20, x+w-14, topY-20, pdfBorder)
	c.text(x+14, topY-32, 7, false, pdfMuted, "Scan to confirm authenticity, check the live status, and download the original PDF.")

	// QR code (left of the section)
	const qrSize = 72.0
	qrX := x + 14.0
	qrY := topY - boxH + 18.0 // bottom-left of QR square in PDF coords; 18pt reserves space for fallback text
	c.qrCode(qrX, qrY, qrSize, verifyURL)
	// Label below QR showing the quotation code
	c.text(qrX, qrY-10, 7, false, pdfMuted, q.QuotationCode)

	// Quote metadata (right of QR)
	mx := qrX + qrSize + 14.0
	c.text(mx, topY-50, 7, true, pdfMuted, "QUOTE ID")
	c.text(mx, topY-62, 9, true, pdfDark, q.QuotationCode)
	c.text(mx, topY-77, 7, true, pdfMuted, "ISSUED ON")
	c.text(mx, topY-89, 8, false, pdfDark, formatPDFDate(q.CreatedAt))
	c.text(mx, topY-104, 7, true, pdfMuted, "VALID UNTIL")
	c.text(mx, topY-116, 8, false, pdfDark, validUntilText(q))

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
		c.text(vx, topY-50, 7, true, pdfMuted, "VALIDATED BY")
		c.text(vx, topY-62, 9, true, pdfDark, toPDFASCII(validatorName))
		c.text(vx, topY-76, 8, false, pdfMuted, validatorRole)
		if !validatedAt.IsZero() {
			c.text(vx, topY-88, 8, false, pdfMuted, formatPDFDate(validatedAt))
		}
		c.text(vx, topY-104, 7, false, pdfMuted, "Digitally generated -")
		c.text(vx, topY-114, 7, false, pdfMuted, "no physical signature required.")
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

// ── Footer ────────────────────────────────────────────────────────────────────

func drawFooter(c *pdfCanvas, pageNum, totalPages int) {
	c.line(50, 76, 545, 76, pdfBorder)
	c.leafMark(50, 58, 10, pdfForest)
	c.text(66, 65, 8, true, pdfDark, "Powered by GreenRoot")
	c.text(66, 54, 7, false, pdfMuted, "Plant Business Management Platform - www.greenroot.app")
	c.textRightAligned(545, 65, 8, true, pdfMuted, fmt.Sprintf("Page %d of %d", pageNum, totalPages))
	c.text(50, 42, 6.5, false, pdfMuted,
		"GreenRoot provides quotation management software only. All quotation information is provided by the issuing nursery.")
}

// leafMark draws a small, restrained monochrome leaf silhouette (asymmetric almond
// shape + a thin center vein) — safe to print in grayscale, never dominant.
func (c *pdfCanvas) leafMark(x, y, size float64, color pdfColor) {
	c.setFill(color)
	w := size * 0.6
	fmt.Fprintf(c, "%.2f %.2f m\n", x, y)
	fmt.Fprintf(c, "%.2f %.2f %.2f %.2f %.2f %.2f c\n", x+w, y+size*0.15, x+w, y+size*0.85, x, y+size)
	fmt.Fprintf(c, "%.2f %.2f %.2f %.2f %.2f %.2f c\n", x-w*0.3, y+size*0.85, x-w*0.3, y+size*0.15, x, y)
	fmt.Fprintf(c, "f\n")
	c.setStroke(pdfWhite)
	fmt.Fprintf(c, "0.6 w\n%.2f %.2f m %.2f %.2f l S\n", x, y+size*0.12, x, y+size*0.88)
}

// ── Drawing primitives ────────────────────────────────────────────────────────

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

// textRightAligned draws text ending at rightX, using an approximate Helvetica
// average-char-width formula so long quotation codes never run off the page edge.
func (c *pdfCanvas) textRightAligned(rightX, y, size float64, bold bool, color pdfColor, text string) {
	c.text(rightX-estimateTextWidth(text, size, bold), y, size, bold, color, text)
}

// estimateTextWidth approximates rendered Helvetica text width. Digits/punctuation
// run close to 0.55-0.6em; use a slightly generous factor so estimates never
// undershoot and cause a column collision.
func estimateTextWidth(text string, size float64, bold bool) float64 {
	factor := 0.58
	if bold {
		factor = 0.64
	}
	return float64(len(toPDFASCII(text))) * size * factor
}

// roundedRectFill fills a rectangle with corner radius r using cubic-bezier corners
// (kappa = 0.5523, the standard circle-approximation constant for a 90-degree arc).
func (c *pdfCanvas) roundedRectFill(x, y, w, h, r float64, color pdfColor) {
	if r > w/2 {
		r = w / 2
	}
	if r > h/2 {
		r = h / 2
	}
	const k = 0.55228475
	c.setFill(color)
	fmt.Fprintf(c, "%.2f %.2f m\n", x+r, y)
	fmt.Fprintf(c, "%.2f %.2f l\n", x+w-r, y)
	fmt.Fprintf(c, "%.2f %.2f %.2f %.2f %.2f %.2f c\n", x+w-r+k*r, y, x+w, y+r-k*r, x+w, y+r)
	fmt.Fprintf(c, "%.2f %.2f l\n", x+w, y+h-r)
	fmt.Fprintf(c, "%.2f %.2f %.2f %.2f %.2f %.2f c\n", x+w, y+h-r+k*r, x+w-r+k*r, y+h, x+w-r, y+h)
	fmt.Fprintf(c, "%.2f %.2f l\n", x+r, y+h)
	fmt.Fprintf(c, "%.2f %.2f %.2f %.2f %.2f %.2f c\n", x+r-k*r, y+h, x, y+h-r+k*r, x, y+h-r)
	fmt.Fprintf(c, "%.2f %.2f l\n", x, y+r)
	fmt.Fprintf(c, "%.2f %.2f %.2f %.2f %.2f %.2f c\n", x, y+r-k*r, x+r-k*r, y, x+r, y)
	fmt.Fprintf(c, "f\n")
}

// statusBadge draws a right-aligned, uppercase, rounded-pill status label.
// Colors are deliberately restrained (light fill + dark text or vice versa) so the
// badge stays legible when the PDF is printed in grayscale.
func (c *pdfCanvas) statusBadge(rightX, baselineY float64, label string) {
	upper := strings.ToUpper(label)
	bg, fg := statusBadgeColors(upper)
	const h = 14.0
	const padX = 8.0
	w := estimateTextWidth(upper, 7, true) + padX*2
	x := rightX - w
	y := baselineY - 3
	c.roundedRectFill(x, y, w, h, h/2, bg)
	c.text(x+padX, y+4.5, 7, true, fg, upper)
}

// quotationStatusBadgeLabel maps the raw DB status (plus the computed valid_until
// expiry, which is not itself a status column) to the short badge label.
func quotationStatusBadgeLabel(q Quotation) string {
	if q.ExpirySummary != nil && q.ExpirySummary.IsExpired && strings.EqualFold(q.Status, "CUSTOMER_SENT") {
		return "EXPIRED"
	}
	switch strings.ToUpper(q.Status) {
	case "INTERNAL_DRAFT", "CUSTOMER_DRAFT":
		return "DRAFT"
	case "CUSTOMER_SENT":
		return "SENT"
	case "CUSTOMER_ACCEPTED":
		return "ACCEPTED"
	case "CUSTOMER_REJECTED":
		return "REJECTED"
	case "CONVERTED":
		return "CONVERTED"
	default:
		return strings.ToUpper(q.Status)
	}
}

func statusBadgeColors(label string) (bg, fg pdfColor) {
	switch label {
	case "DRAFT":
		return rgb(0xE5, 0xE7, 0xEB), rgb(0x37, 0x41, 0x51)
	case "SENT":
		return rgb(0xDC, 0xFC, 0xE7), pdfForest
	case "ACCEPTED":
		return pdfForest, pdfWhite
	case "REJECTED":
		return rgb(0xFE, 0xE2, 0xE2), rgb(0x99, 0x1B, 0x1B)
	case "EXPIRED":
		return rgb(0xE5, 0xE7, 0xEB), rgb(0x37, 0x41, 0x51)
	case "CONVERTED":
		return rgb(0xDB, 0xEA, 0xFE), rgb(0x1D, 0x4E, 0xD8)
	case "RECALLED":
		return pdfAmberLight, pdfAmber
	default:
		return pdfLight, pdfMuted
	}
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

// ── PDF structure (multi-page) ────────────────────────────────────────────────

func wrapMultiPagePDF(pages []string) []byte {
	if len(pages) == 0 {
		pages = []string{""}
	}
	n := len(pages)
	const fontF1Obj = 3
	const fontF2Obj = 4
	const pageObjStart = 5
	contentObjStart := pageObjStart + n

	kids := make([]string, n)
	for i := 0; i < n; i++ {
		kids[i] = fmt.Sprintf("%d 0 R", pageObjStart+i)
	}

	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>",
	}
	for i := 0; i < n; i++ {
		objects = append(objects, fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 %d 0 R /F2 %d 0 R >> >> /Contents %d 0 R >>",
			fontF1Obj, fontF2Obj, contentObjStart+i,
		))
	}
	for i := 0; i < n; i++ {
		stream := pages[i]
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
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
		"₹", "Rs.", // ₹ INDIAN RUPEE SIGN
		"—", "-", // — EM DASH
		"–", "-", // – EN DASH
		"·", "-", // · MIDDLE DOT
		"•", "-", // • BULLET
		"“", "\"", // " LEFT DOUBLE QUOTATION MARK
		"”", "\"", // " RIGHT DOUBLE QUOTATION MARK
		"‘", "'", // ' LEFT SINGLE QUOTATION MARK
		"’", "'", // ' RIGHT SINGLE QUOTATION MARK
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

// FormatINR renders an amount using Indian digit grouping (lakh/crore) with a
// "Rs." prefix and exactly two decimal places, e.g. FormatINR(101275) == "Rs.1,01,275.00".
// A real "₹" glyph is intentionally avoided: this PDF is hand-written using only the
// Standard-14 Helvetica fonts, which have no ₹ glyph (U+20B9) — rendering it would
// require embedding a custom Unicode font.
func FormatINR(amount float64) string {
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	totalPaise := int64(math.Round(amount * 100))
	rupees := totalPaise / 100
	paise := totalPaise % 100
	return fmt.Sprintf("%sRs.%s.%02d", sign, groupIndian(strconv.FormatInt(rupees, 10)), paise)
}

// groupIndian applies Indian digit grouping (last 3 digits, then groups of 2):
// "101275" -> "1,01,275", "1234567" -> "12,34,567".
func groupIndian(digits string) string {
	n := len(digits)
	if n <= 3 {
		return digits
	}
	rest, last3 := digits[:n-3], digits[n-3:]
	var groups []string
	for len(rest) > 2 {
		groups = append([]string{rest[len(rest)-2:]}, groups...)
		rest = rest[:len(rest)-2]
	}
	if rest != "" {
		groups = append([]string{rest}, groups...)
	}
	groups = append(groups, last3)
	return strings.Join(groups, ",")
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
		tensWord := tens[n/10]
		n %= 10
		if n > 0 {
			parts = append(parts, tensWord+"-"+ones[n])
			n = 0
		} else {
			parts = append(parts, tensWord)
		}
	}
	if n > 0 {
		parts = append(parts, ones[n])
	}
	return strings.Join(parts, " ")
}
