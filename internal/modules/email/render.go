package email

import (
	"fmt"
	"html"
	"net/mail"
	"regexp"
	"sort"
	"strings"
)

// Placeholders are {{ snake_case_name }}. Values are HTML-escaped in the HTML
// body, used as-is in the text body, and must be single-line in the subject.
var (
	anyPlaceholderRe = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
	nameRe           = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
)

const maxValueLen = 500

type TemplateSpec struct {
	Subject  string
	BodyHTML string
	BodyText string
}

// Variables returns the sorted, de-duplicated placeholder names used anywhere
// in the template, or an error naming the first malformed placeholder.
func (t TemplateSpec) Variables() ([]string, error) {
	seen := map[string]bool{}
	for _, part := range []string{t.Subject, t.BodyHTML, t.BodyText} {
		for _, m := range anyPlaceholderRe.FindAllStringSubmatch(part, -1) {
			name := strings.TrimSpace(m[1])
			if !nameRe.MatchString(name) {
				return nil, fmt.Errorf("placeholder {{%s}} must be lower_snake_case, e.g. {{order_code}}", m[1])
			}
			seen[name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// ValidateSpec reports problems that would make the template unusable.
func ValidateSpec(t TemplateSpec) (errs []string, warnings []string) {
	if strings.TrimSpace(t.Subject) == "" {
		errs = append(errs, "subject is required")
	}
	if strings.ContainsAny(t.Subject, "\r\n") {
		errs = append(errs, "subject must be a single line")
	}
	if len(t.Subject) > 200 {
		errs = append(errs, "subject is longer than 200 characters")
	}
	if strings.TrimSpace(t.BodyHTML) == "" && strings.TrimSpace(t.BodyText) == "" {
		errs = append(errs, "an HTML or a text body is required")
	}
	if _, err := t.Variables(); err != nil {
		errs = append(errs, err.Error())
	}
	if strings.TrimSpace(t.BodyText) == "" && strings.TrimSpace(t.BodyHTML) != "" {
		warnings = append(warnings, "no text body: some mail clients and spam filters prefer a plain-text part")
	}
	if strings.Contains(strings.ToLower(t.BodyHTML), "<script") {
		errs = append(errs, "scripts are not allowed in email HTML")
	}
	return errs, warnings
}

type Rendered struct {
	Subject    string
	HTML       string
	Text       string
	MaskedSubj string
	MaskedText string // stored in the log; falls back to the HTML when no text body
}

// Render fills every placeholder. sensitive names are masked in the Masked*
// fields only.
func Render(t TemplateSpec, values map[string]string, sensitive map[string]bool) (Rendered, error) {
	names, err := t.Variables()
	if err != nil {
		return Rendered{}, err
	}
	for _, name := range names {
		v, ok := values[name]
		if !ok || v == "" {
			return Rendered{}, fmt.Errorf("variable %s has no value", name)
		}
		if len(v) > maxValueLen {
			return Rendered{}, fmt.Errorf("variable %s is longer than %d characters", name, maxValueLen)
		}
	}
	fill := func(text string, mask, escape bool) string {
		return anyPlaceholderRe.ReplaceAllStringFunc(text, func(m string) string {
			name := strings.TrimSpace(m[2 : len(m)-2])
			v := values[name]
			if mask && sensitive[name] {
				v = strings.Repeat("*", len(v))
			}
			if escape {
				return html.EscapeString(v)
			}
			return v
		})
	}
	r := Rendered{
		Subject:    fill(t.Subject, false, false),
		HTML:       fill(t.BodyHTML, false, true),
		Text:       fill(t.BodyText, false, false),
		MaskedSubj: fill(t.Subject, true, false),
	}
	// Header injection guard: a value must never add a header line.
	if strings.ContainsAny(r.Subject, "\r\n") {
		return Rendered{}, fmt.Errorf("subject values must be single-line")
	}
	if strings.TrimSpace(t.BodyText) != "" {
		r.MaskedText = fill(t.BodyText, true, false)
	} else {
		r.MaskedText = fill(t.BodyHTML, true, true)
	}
	return r, nil
}

// NormalizeAddress lower-cases and validates a bare address (no display name).
func NormalizeAddress(raw string) (string, bool) {
	addr := strings.ToLower(strings.TrimSpace(raw))
	if addr == "" || len(addr) > 254 || strings.ContainsAny(addr, " \r\n<>\"") {
		return "", false
	}
	parsed, err := mail.ParseAddress(addr)
	if err != nil || parsed.Address != addr || parsed.Name != "" {
		return "", false
	}
	at := strings.LastIndex(addr, "@")
	if at < 1 || !strings.Contains(addr[at+1:], ".") {
		return "", false
	}
	return addr, true
}
