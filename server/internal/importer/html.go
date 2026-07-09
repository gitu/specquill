package importer

import (
	"html"
	"regexp"
	"strings"
)

var (
	reScript   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle    = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reHeading  = regexp.MustCompile(`(?is)<h([1-6])[^>]*>(.*?)</h[1-6]>`)
	reListItem = regexp.MustCompile(`(?is)<li[^>]*>(.*?)</li>`)
	reBlock    = regexp.MustCompile(`(?i)</?(p|div|section|article|br|tr|table|ul|ol|h[1-6]|blockquote)[^>]*>`)
	reTag      = regexp.MustCompile(`(?s)<[^>]+>`)
	reWS       = regexp.MustCompile(`[ \t]+`)
	reBlankRun = regexp.MustCompile(`\n{3,}`)
)

// htmlToText reduces HTML (or Confluence storage XHTML) to readable markdown-ish
// text: headings become `#`, list items `- `, block tags become line breaks,
// remaining tags are stripped and entities decoded. It is intentionally
// dependency-free and lossy — enough for the copilot to ground on the prose.
func htmlToText(s string) string {
	s = reScript.ReplaceAllString(s, "")
	s = reStyle.ReplaceAllString(s, "")
	s = reHeading.ReplaceAllStringFunc(s, func(m string) string {
		g := reHeading.FindStringSubmatch(m)
		level := strings.Repeat("#", int(g[1][0]-'0'))
		return "\n\n" + level + " " + strings.TrimSpace(reTag.ReplaceAllString(g[2], "")) + "\n\n"
	})
	s = reListItem.ReplaceAllString(s, "\n- $1")
	s = reBlock.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	// tidy whitespace
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(reWS.ReplaceAllString(ln, " "), " ")
	}
	s = strings.Join(lines, "\n")
	s = reBlankRun.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
