package tools

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

var (
	scriptRe = regexp.MustCompile(`(?is)<script.*?>.*?</script>`)
	styleRe  = regexp.MustCompile(`(?is)<style.*?>.*?</style>`)
	svgRe    = regexp.MustCompile(`(?is)<svg.*?>.*?</svg>`)
	tagRe    = regexp.MustCompile(`(?is)<[^>]+>`)
	spaceRe  = regexp.MustCompile(`\s+`)
)

func ToolFetchLocalURL(args map[string]interface{}) (string, error) {
	url := str(args, "url")
	if url == "" {
		return "", fmt.Errorf("url is required")
	}
	
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}

	client := &http.Client{
		Timeout: envTimeout("GRUMP_FETCH_TIMEOUT", 5),
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	html := string(body)

	// Clean up HTML to create a readable DOM snapshot
	clean := scriptRe.ReplaceAllString(html, "")
	clean = styleRe.ReplaceAllString(clean, "[STYLE]")
	clean = svgRe.ReplaceAllString(clean, "[SVG ICON]")
	
	// Strip all other HTML tags
	text := tagRe.ReplaceAllString(clean, " ")
	
	// Normalize spaces
	text = spaceRe.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	if len(text) > 10000 {
		text = text[:10000] + "\n... [truncated]"
	}

	result := fmt.Sprintf("Status: %d %s\n\nRendered Content / DOM Snapshot:\n----------------------------------------\n%s", resp.StatusCode, resp.Status, text)
	return result, nil
}

func ToolSearchWeb(args map[string]interface{}) (string, error) {
	query := str(args, "query")
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	url := "https://html.duckduckgo.com/html/?q=" + strings.ReplaceAll(query, " ", "+")

	client := &http.Client{
		Timeout: envTimeout("GRUMP_FETCH_TIMEOUT", 5),
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create search request: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to search web: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read search results: %v", err)
	}

	html := string(body)
	
	// A very rudimentary extraction of the result snippets from DDG HTML
	var results []string
	
	// Find all result snippets (DuckDuckGo HTML puts titles in 'result__a' and snippets in 'result__snippet')
	titleRe := regexp.MustCompile(`(?is)<a class="result__url" href="([^"]+)">([^<]+)</a>`)
	snippetRe := regexp.MustCompile(`(?is)<a class="result__snippet[^>]*>(.*?)</a>`)
	
	titles := titleRe.FindAllStringSubmatch(html, 5)
	snippets := snippetRe.FindAllStringSubmatch(html, 5)
	
	for i := 0; i < len(titles) && i < len(snippets); i++ {
		link := titles[i][1]
		// Clean up the duckduckgo redirect link if possible
		if strings.HasPrefix(link, "//duckduckgo.com/l/?uddg=") {
			link = strings.TrimPrefix(link, "//duckduckgo.com/l/?uddg=")
			link = strings.Split(link, "&rut=")[0]
			// Try to unescape roughly
			link = strings.ReplaceAll(link, "%3A", ":")
			link = strings.ReplaceAll(link, "%2F", "/")
		}
		
		desc := tagRe.ReplaceAllString(snippets[i][1], "")
		desc = spaceRe.ReplaceAllString(desc, " ")
		
		results = append(results, fmt.Sprintf("%d. %s\n   URL: %s\n   %s\n", i+1, titles[i][2], link, strings.TrimSpace(desc)))
	}

	if len(results) == 0 {
		return "No results found or search blocked.", nil
	}

	return "Search Results:\n" + strings.Join(results, "\n"), nil
}