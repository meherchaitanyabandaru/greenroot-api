package quotations

import (
	"fmt"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/pdfdoc"
)

// buildQuotationPDF renders the quotation the customer receives. It is the
// same document for the nursery and the customer (the owner shares it), so it
// never carries internal notes or who handles it internally — those stay in
// the app. A manager's copy hides the customer (q arrives redacted).
func buildQuotationPDF(q Quotation, verifyURL string, extras PDFContactExtras) []byte {
	brand := pdfdoc.Hex(q.NurseryBrandColor, pdfdoc.Forest)
	label := quotationStatusBadgeLabel(q)
	bg, fg := statusBadgeColors(label)
	nursery := textOr(q.NurseryName, "GreenRoot Nursery")

	doc := pdfdoc.Business{
		Kind:        "QUOTATION",
		Code:        q.QuotationCode,
		Created:     q.CreatedAt,
		Brand:       brand,
		SellerName:  nursery,
		SellerLines: []string{pdfdoc.Join("  ·  ", textOr(q.NurseryPhone, ""), extras.NurseryEmail), extras.NurseryAddress},
		StatusLabel: label,
		StatusBG:    bg,
		StatusFG:    fg,
		Facts: []pdfdoc.KV{
			{Label: "Issued", Value: pdfdoc.Date(q.CreatedAt)},
			{Label: "Valid until", Value: validUntilText(q)},
			{Label: "Items", Value: fmt.Sprint(len(q.Items))},
			{Label: "Prepared by", Value: textOr(q.CreatedByName, nursery)},
		},
		ItemsTitle: "Plant",
		Columns: []pdfdoc.Column{
			{Title: "Qty", Width: 52},
			{Title: "Rate", Width: 86},
			{Title: "Amount", Width: 96},
		},
		ItemsSummary: itemsSummary(len(q.Items), sumQty(q.Items)),
		TotalLabel:   "Total",
		Total:        q.TotalAmount,
		Signatory:    []string{textOr(q.CreatedByName, nursery), "For " + nursery},
		FooterNote:   "GreenRoot provides the software only. Prices, stock and terms are set by " + nursery + ".",
	}

	if strings.EqualFold(q.QuotationType, "INTERNAL") {
		doc.Banner = &pdfdoc.Callout{Title: "Internal planning document", Text: "For the nursery's own planning. Not a quotation to a customer.", Tone: "info"}
	} else {
		name := textOr(q.RecipientName, "Customer details protected")
		var lines []string
		if q.RecipientName != nil {
			lines = []string{pdfdoc.Join("  ·  ", textOr(q.RecipientMobile, ""), extras.RecipientEmail), extras.RecipientAddress}
		}
		doc.Recipient = &pdfdoc.Party{Label: "Quotation for", Name: name, Lines: lines}
	}

	for _, it := range q.Items {
		title, sub := plantNames(it.ScientificName, it.CommonName)
		doc.Rows = append(doc.Rows, pdfdoc.Row{
			Title:    title,
			Subtitle: sub,
			Detail:   textOr(it.Description, ""),
			Cells:    []string{pdfdoc.Qty(it.Quantity), FormatINR(it.UnitPrice), FormatINR(it.TotalPrice)},
		})
	}

	doc.Callouts = append(doc.Callouts, pdfdoc.Callout{
		Tone: "warn",
		Text: "Prices are valid until " + validUntilText(q) + " and subject to stock availability when the order is confirmed.",
	})
	if strings.EqualFold(q.Status, "CUSTOMER_REJECTED") && q.RejectionReason != nil && strings.TrimSpace(*q.RejectionReason) != "" {
		doc.Callouts = append(doc.Callouts, pdfdoc.Callout{Title: "Rejected by the customer", Text: strings.TrimSpace(*q.RejectionReason), Tone: "danger"})
	}
	if q.ConvertedOrderCode != nil && *q.ConvertedOrderCode != "" {
		doc.Callouts = append(doc.Callouts, pdfdoc.Callout{Title: "Converted to order", Text: "This quotation became order " + *q.ConvertedOrderCode + ".", Tone: "info"})
	}

	if verifyURL != "" {
		doc.Verify = &pdfdoc.Verify{
			Title:    "Verify this quotation",
			Subtitle: "Scan with any phone camera to confirm it is genuine and see its current status.",
			URL:      verifyURL,
			Facts: []pdfdoc.KV{
				{Label: "Quotation", Value: q.QuotationCode},
				{Label: "Issued", Value: pdfdoc.Date(q.CreatedAt)},
				{Label: "Valid until", Value: validUntilText(q)},
			},
		}
	}
	doc.Terms = []string{
		"This quotation is valid until the date shown above.",
		"Prices and quantities are confirmed against stock when the order is placed.",
		"Accept or decline it in the GreenRoot app. Once accepted, the nursery converts it into an order.",
		"Transport, packing and any other charges are as agreed with the nursery unless shown above.",
	}
	return doc.Render()
}

// plantNames puts the name customers know first: "Mango" over "Mangifera indica".
func plantNames(scientific string, common *string) (title, subtitle string) {
	if c := textOr(common, ""); c != "" && !strings.EqualFold(c, scientific) {
		return c, scientific
	}
	return scientific, ""
}

func sumQty(items []QuotationItem) float64 {
	t := 0.0
	for _, it := range items {
		t += it.Quantity
	}
	return t
}

func itemsSummary(items int, qty float64) string {
	noun := "items"
	if items == 1 {
		noun = "item"
	}
	return fmt.Sprintf("%d %s  ·  %s plants", items, noun, pdfdoc.Qty(qty))
}

// quotationStatusBadgeLabel maps the status (plus the computed expiry) to the badge.
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

func statusBadgeColors(label string) (bg, fg pdfdoc.Color) {
	switch label {
	case "SENT":
		return pdfdoc.GreenLight, pdfdoc.Forest
	case "ACCEPTED":
		return pdfdoc.Forest, pdfdoc.White
	case "REJECTED":
		return pdfdoc.RedLight, pdfdoc.Red
	case "CONVERTED":
		return pdfdoc.BlueLight, pdfdoc.Blue
	case "DRAFT", "EXPIRED":
		return pdfdoc.GrayLight, pdfdoc.Gray
	default:
		return pdfdoc.Light, pdfdoc.Muted
	}
}

func validUntilText(q Quotation) string {
	if q.ValidUntil != nil {
		return pdfdoc.Date(*q.ValidUntil)
	}
	return pdfdoc.Date(q.CreatedAt.Add(15 * 24 * time.Hour))
}

// FormatINR: FormatINR(101275) == "Rs.1,01,275.00".
func FormatINR(amount float64) string { return pdfdoc.INR(amount) }

func textOr(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return strings.TrimSpace(*value)
}
