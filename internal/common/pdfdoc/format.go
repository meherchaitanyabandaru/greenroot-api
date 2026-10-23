package pdfdoc

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

var IST = time.FixedZone("IST", 5*60*60+30*60)

// INR formats with Indian grouping: INR(101275) == "Rs.1,01,275.00".
// "Rs." because the standard PDF fonts have no ₹ glyph.
func INR(amount float64) string {
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	paise := int64(math.Round(amount * 100))
	return fmt.Sprintf("%sRs.%s.%02d", sign, GroupIndian(strconv.FormatInt(paise/100, 10)), paise%100)
}

// GroupIndian: "101275" -> "1,01,275", "1234567" -> "12,34,567".
func GroupIndian(digits string) string {
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
	return strings.Join(append(groups, last3), ",")
}

// Qty prints whole quantities without decimals, others with two.
func Qty(q float64) string {
	if q == math.Trunc(q) {
		return strconv.FormatFloat(q, 'f', 0, 64)
	}
	return strconv.FormatFloat(q, 'f', 2, 64)
}

func Date(t time.Time) string     { return t.In(IST).Format("02 Jan 2006") }
func DateTime(t time.Time) string { return t.In(IST).Format("02 Jan 2006, 3:04 PM") }

// AmountInWords: 101275.5 -> "One Lakh One Thousand Two Hundred Seventy-Five Rupees and Fifty Paise".
func AmountInWords(amount float64) string {
	totalPaise := int(math.Round(math.Abs(amount) * 100))
	rupees, paise := totalPaise/100, totalPaise%100
	if rupees == 0 && paise == 0 {
		return "Zero Rupees"
	}
	var parts []string
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
	for _, s := range []struct {
		v    int
		name string
	}{{10000000, "Crore"}, {100000, "Lakh"}, {1000, "Thousand"}} {
		if n >= s.v {
			parts = append(parts, numberToWords(n/s.v)+" "+s.name)
			n %= s.v
		}
	}
	if n >= 100 {
		parts = append(parts, ones[n/100]+" Hundred")
		n %= 100
	}
	if n >= 20 {
		w := tens[n/10]
		if n%10 > 0 {
			w += "-" + ones[n%10]
		}
		parts = append(parts, w)
		n = 0
	}
	if n > 0 {
		parts = append(parts, ones[n])
	}
	return strings.Join(parts, " ")
}

// Join keeps the non-blank values, separated by sep.
func Join(sep string, vals ...string) string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, sep)
}
