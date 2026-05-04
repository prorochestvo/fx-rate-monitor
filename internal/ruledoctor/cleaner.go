package ruledoctor

import "regexp"

var (
	reScript    = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`)
	reStyle     = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style\s*>`)
	reNoScript  = regexp.MustCompile(`(?is)<noscript\b[^>]*>.*?</noscript\s*>`)
	reIframe    = regexp.MustCompile(`(?is)<iframe\b[^>]*>.*?</iframe\s*>`)
	reSVG       = regexp.MustCompile(`(?is)<svg\b[^>]*>.*?</svg\s*>`)
	reComment   = regexp.MustCompile(`(?s)<!--.*?-->`)
	reSelfClose = regexp.MustCompile(`(?is)<(link|meta)\b[^>]*/?>`)
	reSpaces    = regexp.MustCompile(`[ \t]+`)
	reBlanks    = regexp.MustCompile(`\n\s*\n+`)
)

// Clean strips elements that carry no signal for rate extraction:
// scripts, styles, comments, link/meta tags, iframes, noscript, inline SVG.
// The result is still valid HTML and preserves the document tree the LLM
// will reason about. Whitespace is collapsed to keep token count down.
func Clean(htmlStr string) string {
	s := reScript.ReplaceAllString(htmlStr, "")
	s = reStyle.ReplaceAllString(s, "")
	s = reNoScript.ReplaceAllString(s, "")
	s = reIframe.ReplaceAllString(s, "")
	s = reSVG.ReplaceAllString(s, "")
	s = reComment.ReplaceAllString(s, "")
	s = reSelfClose.ReplaceAllString(s, "")
	s = reSpaces.ReplaceAllString(s, " ")
	s = reBlanks.ReplaceAllString(s, "\n")
	return s
}
