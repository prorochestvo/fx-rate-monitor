package ruledoctor

import (
	"errors"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// VerifyResult records whether the LLM-suggested rules and value reproduce
// the expected rate when replayed against the original HTML.
type VerifyResult struct {
	ValueMatches bool
	CSSMatches   bool
	RegexMatches bool

	CSSResult   string
	RegexResult string

	CSSError   error
	RegexError error
}

// Verify replays an Extraction against the original HTML and the known expected value.
// Each of the three checks is independent: a parse error in regex does not affect CSS
// verification, and so on.
func Verify(htmlStr, expected string, ex *Extraction) VerifyResult {
	r := VerifyResult{
		ValueMatches: normalizeValue(ex.Value) == normalizeValue(expected),
	}

	if css := strings.TrimSpace(ex.CSSSelector); css != "" {
		got, err := applyCSS(htmlStr, css)
		switch {
		case err != nil:
			r.CSSError = err
		default:
			r.CSSResult = got
			r.CSSMatches = normalizeValue(got) == normalizeValue(expected)
		}
	}

	if rx := strings.TrimSpace(ex.Regex); rx != "" {
		got, err := applyRegex(htmlStr, rx)
		switch {
		case err != nil:
			r.RegexError = err
		default:
			r.RegexResult = got
			r.RegexMatches = normalizeValue(got) == normalizeValue(expected)
		}
	}

	return r
}

func applyCSS(htmlStr, selector string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		return "", err
	}
	sel, err := safeFind(doc, selector)
	if err != nil {
		return "", err
	}
	if sel.Length() == 0 {
		return "", errors.New("css selector matched no elements")
	}
	return strings.TrimSpace(sel.First().Text()), nil
}

// safeFind wraps goquery.Find so that an invalid selector becomes an error
// rather than a panic from the underlying cascadia parser.
func safeFind(doc *goquery.Document, selector string) (sel *goquery.Selection, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("invalid css selector")
		}
	}()
	return doc.Find(selector), nil
}

func applyRegex(htmlStr, pattern string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	m := re.FindStringSubmatch(htmlStr)
	if len(m) < 2 {
		return "", errors.New("regex produced no capture group 1 match")
	}
	return strings.TrimSpace(m[1]), nil
}

// normalizeValue trims whitespace and any stray surrounding quotes the model
// might have included around a numeric string. Decimal digits are NOT changed:
// "0.4" and "0.40" remain different, intentionally — that is part of what we measure.
func normalizeValue(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	return s
}
