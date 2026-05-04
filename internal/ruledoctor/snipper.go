package ruledoctor

import "strings"

// SnipForPair returns the smallest meaningful fragment of htmlStr containing
// the pair label.
//
// Strategy, in priority order:
//  1. Single <tr>...</tr> row enclosing the pair label (NBK-style table layouts).
//  2. Forward-biased character window: a small chunk before the pair label and
//     a longer chunk after it, since rate values almost always follow the label
//     in document order. This handles div-card layouts (BCC, Bankffin) where the
//     immediate enclosing div holds only the pair text and the values are sibling
//     elements further down the DOM.
//
// We deliberately do NOT walk up the div tree to find the smallest containing
// block: such a walk is O(n*d) on minified HTML and was empirically taking
// ~2 minutes per call on a 290 KB cleaned page. The window approach is O(n)
// once and produces a snippet that is small enough for an LLM and large enough
// to include the rate values.
//
// Returns the empty string if pair is not found.
func SnipForPair(htmlStr, pair string) string {
	idx := strings.Index(htmlStr, pair)
	if idx == -1 {
		return ""
	}

	if start, end, ok := enclosingTr(htmlStr, idx); ok {
		return htmlStr[start:end]
	}
	return windowAround(htmlStr, idx, 200, 1500)
}

// enclosingTr returns the byte offsets of the smallest <tr ...>...</tr> region
// containing idx, if one exists.
func enclosingTr(htmlStr string, idx int) (int, int, bool) {
	trStart := strings.LastIndex(htmlStr[:idx], "<tr")
	trEndRel := strings.Index(htmlStr[idx:], "</tr>")
	if trStart == -1 || trEndRel == -1 {
		return 0, 0, false
	}
	return trStart, idx + trEndRel + len("</tr>"), true
}

// windowAround returns a slice of s spanning `back` bytes before idx and
// `forward` bytes from idx, clamped to the bounds of s.
func windowAround(s string, idx, back, forward int) string {
	start := idx - back
	if start < 0 {
		start = 0
	}
	end := idx + forward
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}
