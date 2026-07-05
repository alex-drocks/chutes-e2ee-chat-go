package chutes

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	webSearchMaxResults       = 6
	webSearchResponseMaxBytes = 5 * 1024 * 1024
)

var webSearchClient = &http.Client{Timeout: 12 * time.Second}

func WebSearch(ctx context.Context, query string, deepSearch bool) (map[string]interface{}, error) {
	normalizedQuery := strings.TrimSpace(query)
	now := time.Now().UTC().Format(time.RFC3339)
	if normalizedQuery == "" {
		return nil, fmt.Errorf("search query is required")
	}

	form := url.Values{}
	form.Set("q", normalizedQuery)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://lite.duckduckgo.com/lite/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "text/html")
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("user-agent", "ChutesE2EEChat-Go/0.1")

	res, err := webSearchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("search failed: HTTP %d", res.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, webSearchResponseMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > webSearchResponseMaxBytes {
		return nil, fmt.Errorf("DuckDuckGo response exceeds %d byte limit", webSearchResponseMaxBytes)
	}

	results := parseDuckDuckGoLiteResults(string(body))
	if len(results) > webSearchMaxResults {
		results = results[:webSearchMaxResults]
	}

	return map[string]interface{}{
		"ok":                       true,
		"results":                  results,
		"fetchedAt":                now,
		"provider":                 "DuckDuckGo",
		"deepSearch":               deepSearch,
		"extractedCount":           0,
		"extractionAttemptedCount": 0,
		"totalResults":             len(results),
		"errors":                   0,
	}, nil
}

func parseDuckDuckGoLiteResults(source string) []map[string]interface{} {
	type result struct {
		title   string
		rawURL  string
		snippet string
	}

	anchors := regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	rawResults := []result{}
	for _, match := range anchors.FindAllStringSubmatch(source, -1) {
		attrs := match[1]
		className := getHTMLAttr(attrs, "class")
		if !classListContains(className, "result-link") {
			continue
		}
		href := getHTMLAttr(attrs, "href")
		title := htmlFragmentToText(match[2])
		if href == "" || title == "" {
			continue
		}
		rawResults = append(rawResults, result{title: title, rawURL: href})
	}

	snippets := []string{}
	snippetTags := regexp.MustCompile(`(?is)<(?:td|a|div|span)\b([^>]*)>(.*?)</(?:td|a|div|span)>`)
	for _, match := range snippetTags.FindAllStringSubmatch(source, -1) {
		if classListContains(getHTMLAttr(match[1], "class"), "result-snippet") {
			if snippet := htmlFragmentToText(match[2]); snippet != "" {
				snippets = append(snippets, snippet)
			}
		}
	}

	out := []map[string]interface{}{}
	seen := map[string]bool{}
	for i, raw := range rawResults {
		normalized, err := normalizeSafeExternalHTTPURL(normalizeDuckDuckGoURL(raw.rawURL))
		if err != nil || seen[normalized] {
			continue
		}
		seen[normalized] = true
		snippet := raw.snippet
		if i < len(snippets) {
			snippet = snippets[i]
		}
		out = append(out, map[string]interface{}{
			"title":   raw.title,
			"url":     normalized,
			"snippet": snippet,
		})
	}
	return out
}

func getHTMLAttr(attrs, name string) string {
	attrRE := regexp.MustCompile(`(?is)([^\s=]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>]+))`)
	needle := strings.ToLower(name)
	for _, match := range attrRE.FindAllStringSubmatch(attrs, -1) {
		if strings.ToLower(match[1]) == needle {
			for _, value := range match[2:] {
				if value != "" {
					return html.UnescapeString(value)
				}
			}
		}
	}
	return ""
}

func classListContains(className, target string) bool {
	for _, class := range strings.Fields(className) {
		if class == target {
			return true
		}
	}
	return false
}

func htmlFragmentToText(value string) string {
	noScripts := regexp.MustCompile(`(?is)<script\b.*?</script>|<style\b.*?</style>|<noscript\b.*?</noscript>`).ReplaceAllString(value, " ")
	noTags := regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(noScripts, " ")
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(html.UnescapeString(noTags), " "))
}

func normalizeDuckDuckGoURL(value string) string {
	decoded := html.UnescapeString(value)
	parsed, err := url.Parse(decoded)
	if err != nil {
		return decoded
	}
	if !parsed.IsAbs() {
		base, _ := url.Parse("https://duckduckgo.com")
		parsed = base.ResolveReference(parsed)
	}
	if uddg := parsed.Query().Get("uddg"); uddg != "" {
		if unescaped, err := url.QueryUnescape(uddg); err == nil {
			return unescaped
		}
		return uddg
	}
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		return parsed.String()
	}
	return decoded
}

func normalizeSafeExternalHTTPURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("URL must use http or https")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("blocked URL credentials")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "", fmt.Errorf("blocked hostname: %s", host)
	}
	if ip := net.ParseIP(host); ip != nil && isPrivateOrReservedIP(ip) {
		return "", fmt.Errorf("blocked private or reserved address: %s", host)
	}
	return parsed.String(), nil
}

func isPrivateOrReservedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}
