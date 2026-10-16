package sms

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf16"
)

// DLT typed variable tags (TRAI variable pre-tagging). The legacy untyped
// {#var#} is rejected: operators no longer approve new templates using it.
const (
	TagNumeric      = "numeric"
	TagAlphanumeric = "alphanumeric"
	TagURL          = "url"
	TagURLOTT       = "urlott"
	TagCBN          = "cbn"
	TagEmail        = "email"
)

var (
	anyTagRe      = regexp.MustCompile(`\{#([^{}#]*)#\}`)
	varNameRe     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	numericRe     = regexp.MustCompile(`^[0-9]+$`)
	alphanumRe    = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	emailRe       = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)
	cbnRe         = regexp.MustCompile(`^[0-9]{3,14}$`)
	optOutHintsRe = regexp.MustCompile(`(?i)\b(opt[ -]?out|unsubscribe|stop)\b`)
)

// Conservative per-tag caps. Operators publish slightly different limits;
// these sit at or below every published figure.
var tagMaxLen = map[string]int{
	TagNumeric:      30,
	TagAlphanumeric: 40,
	TagEmail:        40,
	TagCBN:          14,
	TagURL:          120,
	TagURLOTT:       120,
}

// gsmBasic / gsmExtended are the GSM 03.38 default alphabet; extended
// characters cost two septets.
const (
	gsmBasic    = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"
	gsmExtended = "^{}\\[~]|€\f"
)

// ParseTags returns the tag type of every placeholder in body, in order.
func ParseTags(body string) ([]string, error) {
	matches := anyTagRe.FindAllStringSubmatch(body, -1)
	tags := make([]string, 0, len(matches))
	for _, m := range matches {
		tag := m[1]
		if _, ok := tagMaxLen[tag]; !ok {
			if tag == "var" {
				return nil, fmt.Errorf("untyped {#var#} is no longer accepted by DLT; use {#numeric#}, {#alphanumeric#}, {#url#}, {#urlott#}, {#cbn#} or {#email#}")
			}
			return nil, fmt.Errorf("unknown variable tag {#%s#}", tag)
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

// ValidateValue checks one variable value against its DLT tag type.
func ValidateValue(tag, value string, urls, numbers []string) error {
	if value == "" {
		return fmt.Errorf("value is empty")
	}
	if max := tagMaxLen[tag]; len(value) > max {
		return fmt.Errorf("longer than %d characters", max)
	}
	switch tag {
	case TagNumeric:
		if !numericRe.MatchString(value) {
			return fmt.Errorf("{#numeric#} allows digits only")
		}
	case TagAlphanumeric:
		if !alphanumRe.MatchString(value) {
			return fmt.Errorf("{#alphanumeric#} allows letters and digits only (no spaces or symbols)")
		}
	case TagEmail:
		if !emailRe.MatchString(value) {
			return fmt.Errorf("not a valid email address")
		}
	case TagCBN:
		if !cbnRe.MatchString(value) || !contains(numbers, value) {
			return fmt.Errorf("callback number is not whitelisted on this template")
		}
	case TagURL, TagURLOTT:
		if !urlWhitelisted(value, urls) {
			return fmt.Errorf("URL is not whitelisted on this template")
		}
	}
	return nil
}

// urlWhitelisted accepts an exact match or a path under a whitelisted base, so
// "https://greenroot.in" does not admit "https://greenroot.in.evil.com".
func urlWhitelisted(value string, urls []string) bool {
	for _, base := range urls {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		if value == base || strings.HasPrefix(value, strings.TrimSuffix(base, "/")+"/") {
			return true
		}
	}
	return false
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == value {
			return true
		}
	}
	return false
}

// TemplateSpec is the part of a template that rendering and validation need.
type TemplateSpec struct {
	Body       string
	Variables  []string
	Category   string
	IsUnicode  bool
	URLs       []string
	Numbers    []string
	HeaderType string // empty when no header is linked
}

// ValidateSpec reports problems that would get the template rejected on DLT
// (errors) or cost money / need attention (warnings).
func ValidateSpec(spec TemplateSpec) (errs []string, warnings []string) {
	if strings.TrimSpace(spec.Body) == "" {
		return []string{"body is required"}, nil
	}
	tags, err := ParseTags(spec.Body)
	if err != nil {
		errs = append(errs, err.Error())
	}
	if err == nil && len(tags) != len(spec.Variables) {
		errs = append(errs, fmt.Sprintf("body has %d variable tags but %d variable names were given", len(tags), len(spec.Variables)))
	}
	seen := map[string]bool{}
	for _, name := range spec.Variables {
		if !varNameRe.MatchString(name) {
			errs = append(errs, fmt.Sprintf("variable name %q must be lower_snake_case", name))
		}
		if seen[name] {
			errs = append(errs, fmt.Sprintf("variable name %q is used twice", name))
		}
		seen[name] = true
	}
	if !spec.IsUnicode {
		if bad := nonGSMChars(anyTagRe.ReplaceAllString(spec.Body, "")); bad != "" {
			errs = append(errs, fmt.Sprintf("characters %q are not in the GSM alphabet; remove them or mark the template Unicode", bad))
		}
	}
	for _, tag := range tags {
		if (tag == TagURL || tag == TagURLOTT) && len(spec.URLs) == 0 {
			errs = append(errs, "template uses a URL tag but no URL is whitelisted")
		}
		if tag == TagCBN && len(spec.Numbers) == 0 {
			errs = append(errs, "template uses {#cbn#} but no callback number is whitelisted")
		}
	}
	if spec.HeaderType != "" && !HeaderAllowsCategory(spec.HeaderType, spec.Category) {
		errs = append(errs, fmt.Sprintf("a %s header cannot send %s templates", spec.HeaderType, spec.Category))
	}
	if spec.Category == CategoryPromotional && !optOutHintsRe.MatchString(spec.Body) {
		warnings = append(warnings, "promotional messages should tell the recipient how to opt out")
	}
	if spec.Category == CategoryTransactional {
		warnings = append(warnings, "TRANSACTIONAL is reserved for banks; other businesses register OTP and alerts as SERVICE_IMPLICIT")
	}
	return dedupe(errs), warnings
}

// HeaderAllowsCategory mirrors DLT's header-type / template-category pairing.
func HeaderAllowsCategory(headerType, category string) bool {
	switch headerType {
	case HeaderTransactional:
		return category == CategoryTransactional
	case HeaderService:
		return category == CategoryServiceImplicit || category == CategoryServiceExplicit
	case HeaderPromotional:
		return category == CategoryPromotional
	}
	return false
}

// Rendered is a filled-in template ready to send.
type Rendered struct {
	Body   string // sent to the provider
	Masked string // stored in the log: sensitive values replaced with *
	Chars  int
	Parts  int
}

// Render substitutes values into the template in placeholder order,
// validating each against its tag. sensitive names are masked in Masked.
func Render(spec TemplateSpec, values map[string]string, sensitive map[string]bool) (Rendered, error) {
	tags, err := ParseTags(spec.Body)
	if err != nil {
		return Rendered{}, err
	}
	if len(tags) != len(spec.Variables) {
		return Rendered{}, fmt.Errorf("template variables are misconfigured")
	}
	var body, masked strings.Builder
	last, i := 0, 0
	for _, loc := range anyTagRe.FindAllStringIndex(spec.Body, -1) {
		name := spec.Variables[i]
		value := values[name]
		if err := ValidateValue(tags[i], value, spec.URLs, spec.Numbers); err != nil {
			return Rendered{}, fmt.Errorf("variable %s: %w", name, err)
		}
		body.WriteString(spec.Body[last:loc[0]])
		masked.WriteString(spec.Body[last:loc[0]])
		body.WriteString(value)
		if sensitive[name] {
			masked.WriteString(strings.Repeat("*", len(value)))
		} else {
			masked.WriteString(value)
		}
		last = loc[1]
		i++
	}
	body.WriteString(spec.Body[last:])
	masked.WriteString(spec.Body[last:])
	text := body.String()
	if !spec.IsUnicode {
		if bad := nonGSMChars(text); bad != "" {
			return Rendered{}, fmt.Errorf("characters %q need a Unicode template", bad)
		}
	}
	chars, parts := Segments(text, spec.IsUnicode)
	return Rendered{Body: text, Masked: masked.String(), Chars: chars, Parts: parts}, nil
}

// Segments returns the billable length and SMS part count: GSM-7 is 160 per
// single SMS / 153 per concatenated part, UCS-2 is 70 / 67.
func Segments(text string, unicode bool) (chars int, parts int) {
	single, multi := 160, 153
	if unicode {
		chars = len(utf16.Encode([]rune(text)))
		single, multi = 70, 67
	} else {
		for _, r := range text {
			chars++
			if strings.ContainsRune(gsmExtended, r) {
				chars++
			}
		}
	}
	if chars <= single {
		return chars, 1
	}
	return chars, int(math.Ceil(float64(chars) / float64(multi)))
}

func nonGSMChars(text string) string {
	var bad []rune
	for _, r := range text {
		if !strings.ContainsRune(gsmBasic, r) && !strings.ContainsRune(gsmExtended, r) && !containsRune(bad, r) {
			bad = append(bad, r)
		}
	}
	return string(bad)
}

func containsRune(items []rune, r rune) bool {
	for _, item := range items {
		if item == r {
			return true
		}
	}
	return false
}

func dedupe(items []string) []string {
	seen := map[string]bool{}
	out := items[:0]
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}
