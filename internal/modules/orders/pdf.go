package orders

import (
	"fmt"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/pdfdoc"
)

// buildOrderPDF renders the order document. It is the same for the nursery and
// the buyer — the nursery shares it, and the QR verifies one canonical copy —
// so internal notes and who handles the order stay in the app.
func buildOrderPDF(o Order, nursery PDFNurseryExtras, verifyURL string) []byte {
	brand := pdfdoc.Hex(nursery.BrandColor, pdfdoc.Forest)
	label := orderStatusBadgeLabel(o.Status)
	bg, fg := statusBadgeColors(label)
	seller := textOr(o.SellerNursery, textOr(o.NurseryName, "GreenRoot Nursery"))

	// After loading the order is billed on what was loaded (the total was
	// recalculated); rows must add up to that total.
	billedOnLoaded := o.LoadingCompletedAt != nil
	showLoaded := false
	for _, it := range o.Items {
		if it.LoadedQuantity != nil {
			showLoaded = true
			break
		}
	}

	doc := pdfdoc.Business{
		Kind:        "ORDER",
		Code:        o.OrderCode,
		Created:     o.OrderDate,
		Brand:       brand,
		SellerName:  seller,
		SellerLines: []string{pdfdoc.Join("  ·  ", nursery.Phone, nursery.Email), nursery.Address},
		StatusLabel: label,
		StatusBG:    bg,
		StatusFG:    fg,
		Facts:       orderFacts(o),
		ItemsTitle:  "Plant",
		TotalLabel:  "Total",
		Total:       o.TotalAmount,
		Signatory:   []string{seller, "Authorised signatory"},
		FooterNote:  "GreenRoot provides the software only. Prices, stock and delivery are handled by " + seller + ".",
	}
	if showLoaded {
		doc.Columns = []pdfdoc.Column{{Title: "Ordered", Width: 54}, {Title: "Loaded", Width: 52}, {Title: "Rate", Width: 80}, {Title: "Amount", Width: 92}}
	} else {
		doc.Columns = []pdfdoc.Column{{Title: "Qty", Width: 52}, {Title: "Rate", Width: 86}, {Title: "Amount", Width: 96}}
	}
	doc.Recipient = deliverTo(o)

	var ordered, orderedQty, loadedQty float64
	for _, it := range o.Items {
		title, sub := plantNames(it.ScientificName, it.CommonName)
		if it.IsCustom && it.CustomPlantName != nil {
			title, sub = *it.CustomPlantName, "Not in GreenRoot's plant list" // as the customer named it
		}
		amount := it.TotalPrice
		if billedOnLoaded && it.LoadedQuantity != nil {
			amount = *it.LoadedQuantity * it.UnitPrice
		}
		ordered += it.TotalPrice
		orderedQty += it.Quantity
		row := pdfdoc.Row{Title: title, Subtitle: sub}
		if s := textOr(it.SizeName, ""); s != "" {
			row.Detail = "Size: " + s
		}
		if showLoaded {
			loaded, muted := "-", true
			if it.LoadedQuantity != nil {
				loaded = pdfdoc.Qty(*it.LoadedQuantity)
				muted = *it.LoadedQuantity == it.Quantity // short loads stand out
				loadedQty += *it.LoadedQuantity
			}
			row.Cells = []string{pdfdoc.Qty(it.Quantity), loaded, FormatINR(it.UnitPrice), FormatINR(amount)}
			row.Muted = []bool{false, muted}
		} else {
			row.Cells = []string{pdfdoc.Qty(it.Quantity), FormatINR(it.UnitPrice), FormatINR(amount)}
		}
		doc.Rows = append(doc.Rows, row)
	}

	noun := "items"
	if len(o.Items) == 1 {
		noun = "item"
	}
	doc.ItemsSummary = fmt.Sprintf("%d %s  ·  %s plants", len(o.Items), noun, pdfdoc.Qty(orderedQty))
	if billedOnLoaded {
		doc.ItemsSummary = fmt.Sprintf("%d %s  ·  %s of %s plants loaded", len(o.Items), noun, pdfdoc.Qty(loadedQty), pdfdoc.Qty(orderedQty))
		if short := ordered - o.TotalAmount; short > 0.005 {
			doc.Totals = []pdfdoc.KV{
				{Label: "Ordered value", Value: FormatINR(ordered)},
				{Label: "Not loaded", Value: "- " + FormatINR(short)},
			}
		}
	}

	switch {
	case o.Status == "CANCELLED":
		text := "This order was cancelled"
		if o.CancelledAt != nil {
			text += " on " + pdfdoc.Date(*o.CancelledAt)
		}
		if r := textOr(o.CancelReason, ""); r != "" {
			text += ". Reason: " + r
		}
		doc.Callouts = append(doc.Callouts, pdfdoc.Callout{Title: "Cancelled", Text: text + ".", Tone: "danger"})
	case o.Status == "PARTIALLY_FULFILLED":
		doc.Callouts = append(doc.Callouts, pdfdoc.Callout{Title: "Partially fulfilled",
			Text: "Some plants could not be loaded. The total covers only what was loaded.", Tone: "warn"})
	}

	if verifyURL != "" {
		doc.Verify = &pdfdoc.Verify{
			Title:    "Verify this order",
			Subtitle: "Scan with any phone camera to confirm it is genuine and see its current status.",
			URL:      verifyURL,
			Facts: []pdfdoc.KV{
				{Label: "Order", Value: o.OrderCode},
				{Label: "Order date", Value: pdfdoc.Date(o.OrderDate)},
				{Label: "Status", Value: titleWords(label)},
			},
		}
	}
	doc.Terms = []string{
		"This is a system-generated order document.",
		"Loaded quantities can differ from ordered quantities; the total reflects what was loaded.",
		"Please check the plants at delivery and raise any issue with the nursery the same day.",
		"For questions about this order, contact the nursery directly.",
	}
	return doc.Render()
}

func orderFacts(o Order) []pdfdoc.KV {
	facts := []pdfdoc.KV{{Label: "Order date", Value: pdfdoc.Date(o.OrderDate)}}
	switch {
	case o.Status == "CANCELLED" && o.CancelledAt != nil:
		facts = append(facts, pdfdoc.KV{Label: "Cancelled on", Value: pdfdoc.Date(*o.CancelledAt)})
	case o.LoadingCompletedAt != nil:
		facts = append(facts, pdfdoc.KV{Label: "Loaded on", Value: pdfdoc.Date(*o.LoadingCompletedAt)})
	case o.LoadingStartedAt != nil:
		facts = append(facts, pdfdoc.KV{Label: "Loading since", Value: pdfdoc.Date(*o.LoadingStartedAt)})
	}
	facts = append(facts, pdfdoc.KV{Label: "Items", Value: fmt.Sprint(len(o.Items))})
	if o.OrderNumber != "" && o.OrderNumber != o.OrderCode {
		facts = append(facts, pdfdoc.KV{Label: "Order no.", Value: o.OrderNumber})
	}
	return facts
}

// deliverTo is the order's delivery snapshot, falling back to the buyer.
func deliverTo(o Order) *pdfdoc.Party {
	name := textOr(o.BuyerName, textOr(o.CustomerName, "Walk-in customer"))
	mobile := textOr(o.CustomerMobile, "")
	var lines []string
	if s := o.DeliverySnapshot; s != nil {
		name = textOr(s.ContactName, name)
		mobile = textOr(s.ContactMobile, mobile)
		phones := pdfdoc.Join("  ·  ", mobile, prefixed("Alt. ", s.AlternateMobile))
		street := pdfdoc.Join(", ", textOr(s.AddressLine1, ""), textOr(s.AddressLine2, ""))
		place := pdfdoc.Join(", ", textOr(s.City, ""), textOr(s.State, ""))
		place = pdfdoc.Join(" ", place, textOr(s.PostalCode, ""))
		lines = []string{phones, pdfdoc.Join(", ", street, place), prefixed("Landmark: ", s.Landmark), prefixed("Delivery note: ", s.DeliveryInstructions)}
	} else {
		lines = []string{mobile}
	}
	return &pdfdoc.Party{Label: "Deliver to", Name: name, Lines: lines}
}

func prefixed(prefix string, v *string) string {
	if t := textOr(v, ""); t != "" {
		return prefix + t
	}
	return ""
}

// plantNames puts the name buyers know first: "Mango" over "Mangifera indica".
func plantNames(scientific string, common *string) (title, subtitle string) {
	if c := textOr(common, ""); c != "" && !strings.EqualFold(c, scientific) {
		return c, scientific
	}
	return scientific, ""
}

func orderStatusBadgeLabel(status string) string {
	if strings.EqualFold(status, "PARTIALLY_FULFILLED") {
		return "PARTIAL"
	}
	return strings.ToUpper(status)
}

func statusBadgeColors(label string) (bg, fg pdfdoc.Color) {
	switch label {
	case "PENDING", "PARTIAL":
		return pdfdoc.AmberLight, pdfdoc.Amber
	case "CONFIRMED", "LOADING":
		return pdfdoc.BlueLight, pdfdoc.Blue
	case "LOADED":
		return pdfdoc.GreenLight, pdfdoc.Forest
	case "COMPLETED":
		return pdfdoc.Forest, pdfdoc.White
	case "CANCELLED":
		return pdfdoc.RedLight, pdfdoc.Red
	default:
		return pdfdoc.Light, pdfdoc.Muted
	}
}

func titleWords(s string) string {
	s = strings.ToLower(s)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// FormatINR: FormatINR(101275) == "Rs.1,01,275.00".
func FormatINR(amount float64) string { return pdfdoc.INR(amount) }

func textOr(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return strings.TrimSpace(*value)
}
