package api

import (
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

// htmlPolicy is the allowlist we run every admin-authored rich-text
// body through before it lands in the database. Built once and reused
// (bluemonday policies are goroutine-safe to apply but expensive to
// construct).
//
// The list mirrors what the TipTap toolbar can produce — paragraphs,
// headings, lists, blockquote, hr, links, images, tables, and basic
// inline formatting. <script>, <style>, and event-handler attributes
// are stripped, defusing the stored-XSS surface that comes with the
// editor's raw-HTML mode.
var (
	htmlPolicy     *bluemonday.Policy
	htmlPolicyOnce sync.Once
)

func policy() *bluemonday.Policy {
	htmlPolicyOnce.Do(func() {
		p := bluemonday.UGCPolicy()

		// Tables are produced by the table extension we added in the
		// editor. UGCPolicy already allows the core table tags; we
		// add the alignment + width attributes the editor emits.
		p.AllowAttrs("colspan", "rowspan").OnElements("td", "th")
		p.AllowAttrs("align", "valign").OnElements("td", "th", "tr")
		p.AllowAttrs("style").Matching(bluemonday.Number).OnElements("col")
		p.AllowElements("colgroup", "col")

		// Images: same-origin uploads and full https URLs. (Inline
		// data: URIs are deliberately blocked — they're a vector
		// for sneaking payloads past inspection.)
		p.AllowImages()
		p.AllowAttrs("loading").Matching(bluemonday.SpaceSeparatedTokens).OnElements("img")

		// Hard-fail dangerous URI schemes regardless of context.
		p.AllowStandardURLs()
		p.RequireParseableURLs(true)

		// Links open in a new tab with safe rel set by the editor; we
		// also force noopener / noreferrer here in case the source HTML
		// didn't have it. AddTargetBlankToFullyQualifiedLinks ensures
		// external links get target="_blank".
		p.AllowAttrs("target", "rel").OnElements("a")
		p.AddTargetBlankToFullyQualifiedLinks(true)
		p.RequireNoFollowOnLinks(false)

		htmlPolicy = p
	})
	return htmlPolicy
}

// sanitizeHTML strips disallowed tags and attributes from an admin-
// authored body. Returns "" for empty input so callers don't need
// to guard.
func sanitizeHTML(in string) string {
	if in == "" {
		return ""
	}
	return policy().Sanitize(in)
}
