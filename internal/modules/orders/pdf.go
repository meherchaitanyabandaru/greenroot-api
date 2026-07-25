package orders

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// ── RBAC content rules ────────────────────────────────────────────────────────
// Owner, Manager, and Admin see the full order document — they operationally
// need delivery contact/address to fulfil it, unlike quotations where managers
// are pre-sale and deliberately don't see customer identity. Buyers see their
// own order in full except internal-only fields: notes (nursery-internal
// remarks, never meant for the customer) and who internally handled it.
type orderPDFVisibility struct {
	showInternalNotes   bool
	showAssignedHandler bool
}

func visibilityFor(actor ActorContext) orderPDFVisibility {
	isManage := actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER") ||
		actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN")
	return orderPDFVisibility{showInternalNotes: isManage, showAssignedHandler: isManage}
}

type pdfCanvas struct {
	bytes.Buffer
}

// ── Page geometry ────────────────────────────────────────────────────────────
const (
	partyBoxTop      = 674.0
	contTableY       = 630.0
	contLabelY       = 670.0
	itemRowH         = 40.0
	itemHdrH         = 33.0
	itemsBottomStop  = 105.0
	pageBottomMargin = 90.0
	verificationBoxH = 134.0
)

func buildOrderPDF(o Order, nursery PDFNurseryExtras, vis orderPDFVisibility, verifyURL string) []byte {
	brand := pdfForest
	hasLoaded := false
	if o.Status == "LOADING" || o.Status == "LOADED" || o.Status == "PARTIALLY_FULFILLED" || o.Status == "COMPLETED" {
		for _, item := range o.Items {
			if item.LoadedQuantity != nil {
				hasLoaded = true
				break
			}
		}
	}

	chunks := planOrderPages(o, nursery, brand, vis, verifyURL)
	totalPages := len(chunks)

	pageContents := make([]string, 0, totalPages)
	for i, chunk := range chunks {
		var c pdfCanvas
		drawOrderPageHeader(&c, o, brand, i+1, totalPages)

		var tableBottom float64
		switch {
		case chunk.isFirst:
			tableBottom = drawOrderFirstPageBody(&c, o, nursery, brand, hasLoaded, chunk.items)
		case len(chunk.items) > 0:
			c.text(50, contLabelY, 11, true, brand, "ITEMS - CONTINUED")
			tableBottom = c.orderItemsTable(chunk.items, chunk.startIndex, contTableY, brand, hasLoaded)
		default:
			tableBottom = contTableY - itemHdrH
		}

		if chunk.hasSummary {
			drawOrderSummary(&c, tableBottom, o, brand, vis, verifyURL)
		}

		drawOrderFooter(&c, i+1, totalPages)
		pageContents = append(pageContents, c.String())
	}

	return wrapMultiPagePDF(pageContents)
}

// ── Pagination planning (measure-then-render, mirrors quotations/pdf.go) ─────

type orderPageChunk struct {
	items      []OrderItem
	startIndex int
	isFirst    bool
	hasSummary bool
}

func planOrderPages(o Order, nursery PDFNurseryExtras, brand pdfColor, vis orderPDFVisibility, verifyURL string) []orderPageChunk {
	items := o.Items
	startY := firstOrderPageTableY(o, nursery)

	var chunks []orderPageChunk
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
		chunks = append(chunks, orderPageChunk{items: items[startIdx:idx], startIndex: startIdx + 1, isFirst: first})
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

	required := orderSummaryHeight(o, brand, vis, verifyURL)
	if lastTableBottom-required < pageBottomMargin {
		chunks = append(chunks, orderPageChunk{isFirst: false, hasSummary: true})
	} else {
		last.hasSummary = true
	}
	return chunks
}

func firstOrderPageTableY(o Order, nursery PDFNurseryExtras) float64 {
	fromLines, toLines := orderPartyLinesFor(o, nursery)
	maxLines := len(fromLines)
	if len(toLines) > maxLines {
		maxLines = len(toLines)
	}
	return partyBoxTop - partyBoxHeight(maxLines) - 50
}

func orderPartyLinesFor(o Order, nursery PDFNurseryExtras) (fromLines, toLines []string) {
	fromLines = buildLines(nursery.Phone, nursery.Email, nursery.Address)
	var contactMobile, address string
	if o.DeliverySnapshot != nil {
		contactMobile = strOrEmpty(o.DeliverySnapshot.ContactMobile)
		address = joinAddressParts(
			strOrEmpty(o.DeliverySnapshot.AddressLine1),
			strOrEmpty(o.DeliverySnapshot.City),
			strOrEmpty(o.DeliverySnapshot.State),
			strOrEmpty(o.DeliverySnapshot.PostalCode),
		)
	}
	if contactMobile == "" {
		contactMobile = strOrEmpty(o.CustomerMobile)
	}
	toLines = buildLines(contactMobile, address)
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

func orderSummaryHeight(o Order, brand pdfColor, vis orderPDFVisibility, verifyURL string) float64 {
	var scratch pdfCanvas
	const anchor = 1000.0
	bottom := drawOrderSummary(&scratch, anchor, o, brand, vis, verifyURL)
	return anchor - bottom
}

// ── Header ────────────────────────────────────────────────────────────────────

const (
	metaLabelX = 395.0
	metaValueX = 465.0
)

func drawOrderPageHeader(c *pdfCanvas, o Order, brand pdfColor, pageNum, totalPages int) {
	c.rectFill(38, 680, 6, 128, brand)
	c.rectFill(50, 805, 495, 3, brand)
	c.text(50, 760, 22, true, pdfDark, textOr(o.SellerNursery, textOr(o.NurseryName, "GreenRoot Order")))
	c.text(50, 741, 10, false, pdfMuted, "")

	c.textRightAligned(545, 775, 9, true, pdfMuted, "ORDER")
	c.textRightAligned(545, 748, 18, true, pdfDark, o.OrderCode)

	statusLabel := orderStatusBadgeLabel(o.Status)
	c.text(metaLabelX, 728, 8, true, pdfMuted, "Status")
	c.statusBadge(545, 728, statusLabel)
	c.metaRow(712, "Order Date", formatPDFDate(o.OrderDate))
	if o.Status == "CANCELLED" && o.CancelledAt != nil {
		c.metaRow(696, "Cancelled On", formatPDFDate(*o.CancelledAt))
	} else {
		c.metaRow(696, "Order #", o.OrderNumber)
	}

	c.line(50, 680, 545, 680, pdfBorder)
}

func (c *pdfCanvas) metaRow(y float64, label, value string) {
	c.text(metaLabelX, y, 8, true, pdfMuted, label)
	c.text(metaValueX, y, 8, true, pdfDark, value)
}

func orderStatusBadgeLabel(status string) string {
	switch strings.ToUpper(status) {
	case "PENDING":
		return "PENDING"
	case "CONFIRMED":
		return "CONFIRMED"
	case "LOADING":
		return "LOADING"
	case "LOADED":
		return "LOADED"
	case "PARTIALLY_FULFILLED":
		return "PARTIAL"
	case "COMPLETED":
		return "COMPLETED"
	case "CANCELLED":
		return "CANCELLED"
	default:
		return strings.ToUpper(status)
	}
}

func statusBadgeColors(label string) (bg, fg pdfColor) {
	switch label {
	case "PENDING":
		return rgb(0xFE, 0xF3, 0xC7), rgb(0xB4, 0x53, 0x09)
	case "CONFIRMED":
		return rgb(0xDB, 0xEA, 0xFE), rgb(0x1D, 0x4E, 0xD8)
	case "LOADING":
		return rgb(0xDB, 0xEA, 0xFE), rgb(0x1D, 0x4E, 0xD8)
	case "LOADED":
		return rgb(0xDC, 0xFC, 0xE7), pdfForest
	case "PARTIAL":
		return rgb(0xFE, 0xF3, 0xC7), rgb(0xB4, 0x53, 0x09)
	case "COMPLETED":
		return pdfForest, pdfWhite
	case "CANCELLED":
		return rgb(0xFE, 0xE2, 0xE2), rgb(0x99, 0x1B, 0x1B)
	default:
		return pdfLight, pdfMuted
	}
}

// ── First page body ───────────────────────────────────────────────────────────

func drawOrderFirstPageBody(c *pdfCanvas, o Order, nursery PDFNurseryExtras, brand pdfColor, hasLoaded bool, items []OrderItem) float64 {
	fromLines, toLines := orderPartyLinesFor(o, nursery)
	maxLines := len(fromLines)
	if len(toLines) > maxLines {
		maxLines = len(toLines)
	}
	h := partyBoxHeight(maxLines)

	buyerName := textOr(o.BuyerName, textOr(o.CustomerName, "Walk-in customer"))
	if o.DeliverySnapshot != nil && o.DeliverySnapshot.ContactName != nil && strings.TrimSpace(*o.DeliverySnapshot.ContactName) != "" {
		buyerName = strings.TrimSpace(*o.DeliverySnapshot.ContactName)
	}

	c.partyBox(50, partyBoxTop, 220, h, "FROM", textOr(o.SellerNursery, textOr(o.NurseryName, "-")), fromLines, brand)
	c.partyBox(295, partyBoxTop, 250, h, "TO", buyerName, toLines, pdfMuted)

	tableY := partyBoxTop - h - 50
	return c.orderItemsTable(items, 1, tableY, brand, hasLoaded)
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

func (c *pdfCanvas) orderItemsTable(items []OrderItem, startIndex int, y float64, brand pdfColor, hasLoaded bool) float64 {
	var xs []float64
	var headers []string
	qtyColRight, loadedColRight, priceColRight := 340.0, 0.0, 459.0
	if hasLoaded {
		xs = []float64{50, 78, 300, 345, 395, 465}
		headers = []string{"#", "PLANT / ITEM", "QTY", "LOADED", "UNIT PRICE", "AMOUNT"}
		qtyColRight, loadedColRight = 339.0, 389.0
	} else {
		xs = []float64{50, 78, 340, 385, 465}
		headers = []string{"#", "PLANT / ITEM", "QTY", "UNIT PRICE", "AMOUNT"}
		qtyColRight = 379.0
	}
	headerRightX := map[string]float64{"QTY": qtyColRight, "LOADED": loadedColRight, "UNIT PRICE": priceColRight, "AMOUNT": 539}

	const hdrH = 30.0
	c.rectFill(50, y, 495, hdrH, brand)
	for i, hd := range headers {
		if rx, ok := headerRightX[hd]; ok {
			c.textRightAligned(rx, y+11, 8, true, pdfWhite, hd)
		} else {
			c.text(xs[i]+6, y+11, 8, true, pdfWhite, hd)
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
		c.text(86, rowY+23, 10, true, pdfDark, truncatePDFText(item.ScientificName, 40))
		if item.CommonName != nil {
			c.text(86, rowY+10, 8, false, pdfMuted, truncatePDFText(*item.CommonName, 40))
		}
		c.textRightAligned(qtyColRight, rowY+20, 10, false, pdfDark, formatPDFQty(item.Quantity))
		if hasLoaded {
			loadedText := "-"
			if item.LoadedQuantity != nil {
				loadedText = formatPDFQty(*item.LoadedQuantity)
			}
			c.textRightAligned(loadedColRight, rowY+20, 10, false, pdfDark, loadedText)
		}
		c.textRightAligned(priceColRight, rowY+20, 10, false, pdfDark, FormatINR(item.UnitPrice))
		c.textRightAligned(539, rowY+20, 10, true, pdfDark, FormatINR(item.TotalPrice))
		rowY -= rowH
	}
	return rowY
}

// ── Summary: total → notes (RBAC-gated) → cancellation → terms/authorized ────

func drawOrderSummary(c *pdfCanvas, topY float64, o Order, brand pdfColor, vis orderPDFVisibility, verifyURL string) float64 {
	totalY := topY - 52
	c.rectFill(50, totalY, 495, 44, pdfSoftGreen)
	c.rectStroke(50, totalY, 495, 44, brand)
	c.text(62, totalY+28, 10, true, brand, "GRAND TOTAL")
	c.textRightAligned(533, totalY+26, 18, true, pdfDark, FormatINR(o.TotalAmount))
	c.textRightAligned(533, totalY+10, 8, false, pdfMuted, amountInWords(o.TotalAmount)+" Only")

	nextY := totalY - 34

	if vis.showInternalNotes && o.Notes != nil && strings.TrimSpace(*o.Notes) != "" {
		c.rectFill(50, nextY-34, 495, 42, pdfLight)
		c.rectStroke(50, nextY-34, 495, 42, pdfBorder)
		c.text(62, nextY-8, 8, true, pdfMuted, "NOTES (internal)")
		c.text(62, nextY-24, 9, false, pdfDark, truncatePDFText(strings.TrimSpace(*o.Notes), 88))
		nextY -= 54
	}

	if o.Status == "CANCELLED" && o.CancelReason != nil && strings.TrimSpace(*o.CancelReason) != "" {
		c.rectFill(50, nextY-34, 495, 42, rgb(0xFE, 0xE2, 0xE2))
		c.rectStroke(50, nextY-34, 495, 42, rgb(0x99, 0x1B, 0x1B))
		c.text(62, nextY-8, 8, true, rgb(0x99, 0x1B, 0x1B), "CANCELLATION REASON")
		c.text(62, nextY-24, 9, false, pdfDark, truncatePDFText(strings.TrimSpace(*o.CancelReason), 88))
		nextY -= 54
	}

	if verifyURL != "" {
		c.orderVerificationSection(nextY-8, brand, o, verifyURL)
		nextY -= verificationBoxH + 8
	}

	nextY -= 12
	return c.orderTermsAndAuthBlock(nextY, brand, o, vis)
}

// ── Document Verification section (mirrors quotations/pdf.go exactly) ────────

func (c *pdfCanvas) orderVerificationSection(topY float64, brand pdfColor, o Order, verifyURL string) {
	const boxH = verificationBoxH
	const x = 50.0
	const w = 495.0

	c.rectFill(x, topY-boxH, w, boxH, pdfLight)
	c.rectStroke(x, topY-boxH, w, boxH, pdfBorder)
	c.rectFill(x, topY-boxH, 3, boxH, brand)

	c.text(x+14, topY-14, 8, true, brand, "VERIFY THIS ORDER")
	c.line(x+14, topY-20, x+w-14, topY-20, pdfBorder)
	c.text(x+14, topY-32, 7, false, pdfMuted, "Scan to confirm authenticity, check the live status, and download the original PDF.")

	const qrSize = 72.0
	qrX := x + 14.0
	qrY := topY - boxH + 18.0
	c.qrCode(qrX, qrY, qrSize, verifyURL)
	c.text(qrX, qrY-10, 7, false, pdfMuted, o.OrderCode)

	mx := qrX + qrSize + 14.0
	c.text(mx, topY-50, 7, true, pdfMuted, "ORDER ID")
	c.text(mx, topY-62, 9, true, pdfDark, o.OrderCode)
	c.text(mx, topY-77, 7, true, pdfMuted, "ISSUED ON")
	c.text(mx, topY-89, 8, false, pdfDark, formatPDFDate(o.OrderDate))
	c.text(mx, topY-104, 7, true, pdfMuted, "STATUS")
	c.text(mx, topY-116, 8, false, pdfDark, orderStatusBadgeLabel(o.Status))

	if o.AssignedManagerName != nil && strings.TrimSpace(*o.AssignedManagerName) != "" {
		vx := x + w/2 + 20
		c.text(vx, topY-50, 7, true, pdfMuted, "VALIDATED BY")
		c.text(vx, topY-62, 9, true, pdfDark, toPDFASCII(strings.TrimSpace(*o.AssignedManagerName)))
		c.text(vx, topY-76, 8, false, pdfMuted, "Nursery Manager")
		c.text(vx, topY-104, 7, false, pdfMuted, "Digitally generated -")
		c.text(vx, topY-114, 7, false, pdfMuted, "no physical signature required.")
	}
}

// qrCode draws a QR code as filled vector rectangles using the brand forest-green
// color. x, y is the bottom-left corner in PDF coordinates; size is the side length.
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
	ctr := size / 2
	const m = 5.0
	c.rectFill(x+ctr-m, y+ctr-m, m*2, m*2, pdfWhite)
	c.rectFill(x+ctr-3.5, y+ctr-3.5, 7, 7, pdfQRGreen)
}

func (c *pdfCanvas) orderTermsAndAuthBlock(topY float64, brand pdfColor, o Order, vis orderPDFVisibility) float64 {
	const h = 130.0
	const x = 50.0
	const w = 495.0
	bottom := topY - h
	c.rectFill(x, bottom, w, h, pdfWhite)
	c.rectStroke(x, bottom, w, h, pdfBorder)
	colX := x + 315.0
	c.line(colX, bottom, colX, topY, pdfBorder)

	c.text(x+14, topY-16, 8, true, brand, "TERMS")
	terms := []string{
		"1. This is a system-generated order receipt.",
		"2. Loaded quantities may differ from ordered quantities; totals",
		"   reflect what was actually loaded and delivered.",
		"3. Contact the issuing nursery directly for questions about",
		"   this order.",
		"4. This document does not require a physical signature.",
	}
	ty := topY - 30
	for _, t := range terms {
		c.text(x+14, ty, 7, false, pdfMuted, t)
		ty -= 13
	}

	ax := colX + 16
	c.text(ax, topY-16, 8, true, brand, "PREPARED BY")
	if vis.showAssignedHandler {
		c.text(ax, topY-32, 10, true, pdfDark, textOr(o.AssignedManagerName, "-"))
	} else {
		c.text(ax, topY-32, 10, true, pdfDark, "-")
	}
	c.text(ax, topY-46, 8, false, pdfMuted, "For "+textOr(o.SellerNursery, textOr(o.NurseryName, "GreenRoot")))
	c.text(ax, topY-62, 7, false, pdfMuted, "Digitally generated via GreenRoot")

	return bottom
}

// ── Footer ────────────────────────────────────────────────────────────────────

func drawOrderFooter(c *pdfCanvas, pageNum, totalPages int) {
	c.line(50, 76, 545, 76, pdfBorder)
	c.leafMark(50, 58, 10, pdfForest)
	c.text(66, 65, 8, true, pdfDark, "Powered by GreenRoot")
	c.text(66, 54, 7, false, pdfMuted, "Plant Business Management Platform - www.greenroot.app")
	c.textRightAligned(545, 65, 8, true, pdfMuted, fmt.Sprintf("Page %d of %d", pageNum, totalPages))
	c.text(50, 42, 6.5, false, pdfMuted,
		"GreenRoot provides order management software only. All order information is provided by the issuing nursery.")
}

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

// ── Status badge ──────────────────────────────────────────────────────────────

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

func (c *pdfCanvas) textRightAligned(rightX, y, size float64, bold bool, color pdfColor, text string) {
	c.text(rightX-estimateTextWidth(text, size, bold), y, size, bold, color, text)
}

func estimateTextWidth(text string, size float64, bold bool) float64 {
	factor := 0.58
	if bold {
		factor = 0.64
	}
	return float64(len(toPDFASCII(text))) * size * factor
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
	pdfForest    = rgb(0x16, 0x65, 0x34)
	pdfDark      = rgb(0x1F, 0x29, 0x37)
	pdfMuted     = rgb(0x6B, 0x72, 0x80)
	pdfLight     = rgb(0xF8, 0xFA, 0xFC)
	pdfBorder    = rgb(0xE5, 0xE7, 0xEB)
	pdfSoftGreen = rgb(0xF0, 0xFF, 0xF4)
	pdfWhite     = rgb(0xFF, 0xFF, 0xFF)
	pdfQRGreen   = rgb(0x1A, 0x47, 0x31) // matches Flutter QR eye/module color
)

func rgb(r, g, b int) pdfColor {
	return pdfColor{float64(r) / 255, float64(g) / 255, float64(b) / 255}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func escapePDFText(text string) string {
	text = strings.ReplaceAll(text, `\`, `\\`)
	text = strings.ReplaceAll(text, "(", `\(`)
	text = strings.ReplaceAll(text, ")", `\)`)
	return text
}

func toPDFASCII(text string) string {
	replacer := strings.NewReplacer(
		"₹", "Rs.",
		"—", "-",
		"–", "-",
		"·", "-",
		"•", "-",
		"“", "\"",
		"”", "\"",
		"‘", "'",
		"’", "'",
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

var istZone = time.FixedZone("IST", 5*60*60+30*60)

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

func strOrEmpty(value *string) string {
	if value == nil {
		return ""
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
