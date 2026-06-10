// internal/ui/messages/links.go
//
// ExtractLinks pulls http(s)/mailto links out of a message's mrkdwn
// text using the same regexes the renderer uses (render.go), so the
// "open link" keybinding sees exactly the links the user sees.
package messages

import (
	"sort"
	"strings"
)

// Link is one link found in a message's text.
type Link struct {
	URL   string
	Label string // empty for bare <url> links
}

// ExtractLinks returns the links in text in order of appearance,
// deduplicated by URL (first occurrence wins). Returns nil when text
// has no links.
func ExtractLinks(text string) []Link {
	type posLink struct {
		start int
		link  Link
	}
	var found []posLink
	for _, m := range linkWithLabelRe.FindAllStringSubmatchIndex(text, -1) {
		found = append(found, posLink{
			start: m[0],
			link:  Link{URL: text[m[2]:m[3]], Label: text[m[4]:m[5]]},
		})
	}
	for _, m := range linkBareRe.FindAllStringSubmatchIndex(text, -1) {
		url := text[m[2]:m[3]]
		// linkBareRe also matches the labeled form (its [^>]+ spans
		// the "|label" part); those were already captured above.
		if strings.Contains(url, "|") {
			continue
		}
		found = append(found, posLink{start: m[0], link: Link{URL: url}})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].start < found[j].start })
	var out []Link
	seen := make(map[string]bool, len(found))
	for _, f := range found {
		if seen[f.link.URL] {
			continue
		}
		seen[f.link.URL] = true
		out = append(out, f.link)
	}
	return out
}
