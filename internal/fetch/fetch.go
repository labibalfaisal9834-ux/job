package fetch

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	maxBodyBytes = 3 * 1024 * 1024 // 3MB safety cap on downloaded HTML
	maxTextChars = 20000           // cap on cleaned text sent to Gemini (keeps token usage sane)
	userAgent    = "jobwatch-bot/1.0 (+personal career-page monitor)"
)

var (
	scriptStyleRe = regexp.MustCompile(`(?is)<(script|style|noscript)\b[^>]*>.*?</(script|style|noscript)>`)
	anchorRe      = regexp.MustCompile(`(?is)<a\s+[^>]*?href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
	tagRe         = regexp.MustCompile(`(?s)<[^>]+>`)
	whitespaceRe  = regexp.MustCompile(`\s+`)
)

// PageText downloads pageURL and returns a cleaned, plain-text version of
// its visible content, with links rewritten inline as "text [absolute-url]"
// so an LLM can still associate job titles with their posting links even
// after all other tags are stripped.
//
// NOTE: this is a plain HTTP GET — it does not execute JavaScript. Career
// pages that render their job list client-side (common with some Workday
// or custom React/Vue setups) will come back looking empty. Greenhouse and
// Lever pages are handled separately via their public APIs and never reach
// this code path.
func PageText(pageURL string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", pageURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching %s: unexpected status %d", pageURL, resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, maxBodyBytes)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("reading body of %s: %w", pageURL, err)
	}

	return cleanHTML(string(raw), pageURL), nil
}

func cleanHTML(doc, baseURL string) string {
	doc = scriptStyleRe.ReplaceAllString(doc, " ")
	doc = rewriteAnchors(doc, baseURL)
	doc = tagRe.ReplaceAllString(doc, " ")
	doc = html.UnescapeString(doc)
	doc = whitespaceRe.ReplaceAllString(doc, " ")
	doc = strings.TrimSpace(doc)

	if len(doc) > maxTextChars {
		doc = doc[:maxTextChars]
	}
	return doc
}

// rewriteAnchors turns <a href="...">Text</a> into " Text [https://resolved/url] "
// so link targets survive tag-stripping. Relative hrefs are resolved against
// baseURL.
func rewriteAnchors(doc, baseURL string) string {
	base, baseErr := url.Parse(baseURL)

	matches := anchorRe.FindAllStringSubmatchIndex(doc, -1)
	if matches == nil {
		return doc
	}

	var sb strings.Builder
	last := 0
	for _, m := range matches {
		sb.WriteString(doc[last:m[0]])

		href := doc[m[2]:m[3]]
		linkText := tagRe.ReplaceAllString(doc[m[4]:m[5]], " ")
		linkText = html.UnescapeString(linkText)
		linkText = whitespaceRe.ReplaceAllString(linkText, " ")
		linkText = strings.TrimSpace(linkText)

		resolved := href
		if baseErr == nil {
			if u, err := url.Parse(href); err == nil {
				resolved = base.ResolveReference(u).String()
			}
		}

		fmt.Fprintf(&sb, " %s [%s] ", linkText, resolved)
		last = m[1]
	}
	sb.WriteString(doc[last:])
	return sb.String()
}
