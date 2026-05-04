package ruledoctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/seilbekskindirov/monitor/internal"
)

// Extraction is the structured response we ask the LLM to produce.
type Extraction struct {
	Value       string  `json:"value"`
	CSSSelector string  `json:"css_selector"`
	Regex       string  `json:"regex"`
	Confidence  float64 `json:"confidence"`
	Reasoning   string  `json:"reasoning,omitempty"`
}

const promptTemplate = `You are an expert at extracting structured data from HTML.

TASK
Find the exchange rate value for the currency pair "{{PAIR}}" in the HTML fragment below, and produce a CSS selector and a Go RE2 regex that would extract it deterministically when applied to the SAME HTML.

CRITICAL RULES
1. The rate value is located inside the SAME <tr>...</tr> row as the pair label "{{PAIR}}". Never take a value from another row.
2. Preserve the value exactly as printed — keep all decimal digits, do not pad or trim zeros.
3. The CSS selector must be valid for goquery / cascadia. You MAY use :contains("text") and :has(...) — both are supported. Anchoring by the pair text via :contains() is the most robust approach.
4. The regex must be valid Go RE2 — no lookahead, no lookbehind, no backreferences. Use a capture group ( ... ) to capture the value as group 1.
5. Output ONLY one JSON object. No markdown fences, no commentary.

OUTPUT SCHEMA
{
  "value":        "<rate as string, exactly as in HTML>",
  "css_selector": "<cascadia-compatible CSS selector>",
  "regex":        "<Go RE2 regex with capture group 1 = the value>",
  "confidence":   <number 0.0..1.0>
}

EXAMPLE
Pair: "EUR / KZT"
HTML:
<tr><td class="text-start">1 ЕВРО</td><td>EUR / KZT</td><td>542.16</td></tr>

Output:
{"value":"542.16","css_selector":"tr:has(td:contains(\"EUR / KZT\")) td:nth-child(3)","regex":"EUR / KZT</td>\\s*<td>([0-9.]+)</td>","confidence":0.95}

NOW DO THIS ONE
Pair: "{{PAIR}}"
HTML:
{{HTML}}

JSON:
`

// BuildPrompt fills the template with the provided HTML fragment and pair label.
func BuildPrompt(htmlFragment, pair string) string {
	out := strings.ReplaceAll(promptTemplate, "{{PAIR}}", pair)
	out = strings.ReplaceAll(out, "{{HTML}}", htmlFragment)
	return out
}

// ParseExtraction decodes a model response into an Extraction.
// Tolerates leading/trailing whitespace, ```json fences, and prose preceding
// or following the JSON object — any extracted balanced {...} is attempted.
func ParseExtraction(raw string) (*Extraction, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	if !strings.HasPrefix(s, "{") {
		if extracted := extractFirstJSONObject(s); extracted != "" {
			s = extracted
		}
	}

	var ex Extraction
	if err := json.Unmarshal([]byte(s), &ex); err != nil {
		return nil, errors.Join(fmt.Errorf("invalid JSON from model: %w; raw=%q", err, raw), internal.NewTraceError())
	}
	return &ex, nil
}

// extractFirstJSONObject returns the first balanced {...} substring of s,
// respecting double-quoted strings and escaped characters. Returns "" if
// no balanced object is found.
func extractFirstJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start == -1 {
		return ""
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if escape {
			escape = false
			continue
		}
		if ch == '\\' && inStr {
			escape = true
			continue
		}
		if ch == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
