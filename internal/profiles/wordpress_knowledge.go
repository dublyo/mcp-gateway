package profiles

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type WordPressKnowledgeProfile struct {
	mu     sync.RWMutex
	cache  map[string]*wpKnowledgeSource
	client *http.Client
}

type wpKnowledgeSource struct {
	URL          string
	FetchedAt    time.Time
	ETag         string
	LastModified string
	ContentChars int
	Chunks       []wpKnowledgeChunk
}

type wpKnowledgeChunk struct {
	Heading string
	Content string
	lower   string
}

type wpKnowledgeMatch struct {
	Chunk wpKnowledgeChunk
	Score float64
}

func (p *WordPressKnowledgeProfile) ID() string { return "wordpress-knowledge" }

func (p *WordPressKnowledgeProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "search_knowledge",
			Description: "Search the connected WordPress llms.txt knowledge and return the most relevant sections",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Question or search query",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of matches to return (default 5)",
					},
					"max_chars": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum characters per returned snippet (default 900)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "source_status",
			Description: "Show llms.txt source/index status (URL, fetch time, sections/chunks)",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "list_sections",
			Description: "List section headings detected in the llms.txt source",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of headings to return (default 50)",
					},
				},
			},
		},
		{
			Name:        "refresh_source",
			Description: "Force-refresh the llms.txt source immediately and rebuild the search index",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

func (p *WordPressKnowledgeProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "search_knowledge":
		return p.searchKnowledge(args, env)
	case "source_status":
		return p.sourceStatus(env, false)
	case "list_sections":
		return p.listSections(args, env)
	case "refresh_source":
		return p.sourceStatus(env, true)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *WordPressKnowledgeProfile) searchKnowledge(args map[string]interface{}, env map[string]string) (string, error) {
	query := strings.TrimSpace(getStr(args, "query"))
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	source, warning, err := p.ensureSource(env, false)
	if err != nil {
		return "", err
	}

	limit := int(getFloat(args, "limit"))
	if limit <= 0 {
		limit = envInt(env["MAX_RESULTS"], 5)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 20 {
		limit = 20
	}

	maxChars := int(getFloat(args, "max_chars"))
	if maxChars <= 0 {
		maxChars = 900
	}
	if maxChars < 200 {
		maxChars = 200
	}
	if maxChars > 3000 {
		maxChars = 3000
	}

	queryLower := strings.ToLower(query)
	terms := uniqueTerms(tokenize(query))
	if len(terms) == 0 {
		return "", fmt.Errorf("query must contain letters or numbers")
	}

	matches := make([]wpKnowledgeMatch, 0, len(source.Chunks))
	for _, chunk := range source.Chunks {
		score := scoreKnowledgeChunk(chunk, queryLower, terms)
		if score <= 0 {
			continue
		}
		matches = append(matches, wpKnowledgeMatch{Chunk: chunk, Score: score})
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return len(matches[i].Chunk.Content) > len(matches[j].Chunk.Content)
		}
		return matches[i].Score > matches[j].Score
	})

	if len(matches) > limit {
		matches = matches[:limit]
	}

	var out strings.Builder
	out.WriteString(fmt.Sprintf("Source: %s\n", source.URL))
	out.WriteString(fmt.Sprintf("Fetched: %s\n", source.FetchedAt.UTC().Format(time.RFC3339)))
	out.WriteString(fmt.Sprintf("Indexed chunks: %d\n", len(source.Chunks)))
	if warning != "" {
		out.WriteString(fmt.Sprintf("Note: %s\n", warning))
	}

	if len(matches) == 0 {
		out.WriteString("\nNo relevant matches found for this query.")
		return out.String(), nil
	}

	for i, match := range matches {
		snippet := normalizeWhitespace(match.Chunk.Content)
		snippet = truncateRunes(snippet, maxChars)
		out.WriteString(fmt.Sprintf(
			"\n\n%d) %s\nScore: %.2f\n%s",
			i+1,
			match.Chunk.Heading,
			match.Score,
			snippet,
		))
	}

	return out.String(), nil
}

func (p *WordPressKnowledgeProfile) sourceStatus(env map[string]string, forceRefresh bool) (string, error) {
	source, warning, err := p.ensureSource(env, forceRefresh)
	if err != nil {
		return "", err
	}

	headings := uniqueHeadings(source.Chunks)
	resp := map[string]interface{}{
		"sourceUrl":      source.URL,
		"fetchedAt":      source.FetchedAt.UTC().Format(time.RFC3339),
		"contentChars":   source.ContentChars,
		"sectionsCount":  len(headings),
		"chunksCount":    len(source.Chunks),
		"sampleHeadings": truncateStrings(headings, 10),
	}
	if warning != "" {
		resp["warning"] = warning
	}

	b, _ := json.MarshalIndent(resp, "", "  ")
	return string(b), nil
}

func (p *WordPressKnowledgeProfile) listSections(args map[string]interface{}, env map[string]string) (string, error) {
	source, warning, err := p.ensureSource(env, false)
	if err != nil {
		return "", err
	}

	limit := int(getFloat(args, "limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	type sectionInfo struct {
		Heading string `json:"heading"`
		Chunks  int    `json:"chunks"`
	}

	counter := map[string]int{}
	for _, chunk := range source.Chunks {
		counter[chunk.Heading]++
	}

	sections := make([]sectionInfo, 0, len(counter))
	for heading, chunks := range counter {
		sections = append(sections, sectionInfo{Heading: heading, Chunks: chunks})
	}
	sort.Slice(sections, func(i, j int) bool {
		if sections[i].Chunks == sections[j].Chunks {
			return sections[i].Heading < sections[j].Heading
		}
		return sections[i].Chunks > sections[j].Chunks
	})

	if len(sections) > limit {
		sections = sections[:limit]
	}

	resp := map[string]interface{}{
		"sourceUrl": source.URL,
		"count":     len(sections),
		"sections":  sections,
	}
	if warning != "" {
		resp["warning"] = warning
	}

	b, _ := json.MarshalIndent(resp, "", "  ")
	return string(b), nil
}

func (p *WordPressKnowledgeProfile) ensureSource(env map[string]string, force bool) (*wpKnowledgeSource, string, error) {
	rawURL := strings.TrimSpace(env["LLMS_TXT_URL"])
	if rawURL == "" {
		return nil, "", fmt.Errorf("LLMS_TXT_URL is not configured")
	}
	parsedURL, err := validateKnowledgeURL(rawURL)
	if err != nil {
		return nil, "", err
	}
	rawURL = parsedURL.String()

	cacheKey := p.cacheKey(rawURL, env["LLMS_TXT_AUTH_TOKEN"])
	refreshSeconds := envInt(env["REFRESH_INTERVAL_SECONDS"], 300)
	if refreshSeconds < 10 {
		refreshSeconds = 10
	}
	if refreshSeconds > 86400 {
		refreshSeconds = 86400
	}

	p.mu.RLock()
	current := p.cache[cacheKey]
	p.mu.RUnlock()

	if !force && current != nil && time.Since(current.FetchedAt) < time.Duration(refreshSeconds)*time.Second {
		return current, "", nil
	}

	maxBytes := envInt(env["MAX_DOWNLOAD_BYTES"], 26214400)
	if maxBytes < 1024 {
		maxBytes = 1024
	}
	if maxBytes > 100*1024*1024 {
		maxBytes = 100 * 1024 * 1024
	}

	client := p.httpClient()
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build request: %s", err)
	}

	userAgent := strings.TrimSpace(env["USER_AGENT"])
	if userAgent == "" {
		userAgent = "Dublyo-WP-Knowledge/1.0"
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/plain,text/markdown;q=0.9,*/*;q=0.1")

	if token := strings.TrimSpace(env["LLMS_TXT_AUTH_TOKEN"]); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if current != nil {
		if current.ETag != "" {
			req.Header.Set("If-None-Match", current.ETag)
		}
		if current.LastModified != "" {
			req.Header.Set("If-Modified-Since", current.LastModified)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		if current != nil {
			return current, fmt.Sprintf("using cached source because refresh failed: %s", err), nil
		}
		return nil, "", fmt.Errorf("failed to fetch llms.txt source: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && current != nil {
		refreshed := *current
		refreshed.FetchedAt = time.Now()
		p.mu.Lock()
		p.ensureCacheLocked()
		p.cache[cacheKey] = &refreshed
		p.mu.Unlock()
		return &refreshed, "", nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if current != nil {
			return current, fmt.Sprintf("using cached source because endpoint returned HTTP %d", resp.StatusCode), nil
		}
		return nil, "", fmt.Errorf("llms.txt endpoint returned HTTP %d", resp.StatusCode)
	}

	body, err := readWithLimit(resp.Body, maxBytes)
	if err != nil {
		if current != nil {
			return current, fmt.Sprintf("using cached source because response read failed: %s", err), nil
		}
		return nil, "", err
	}

	if !utf8.Valid(body) {
		body = []byte(strings.ToValidUTF8(string(body), " "))
	}
	content := normalizeNewlines(string(body))
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, "", fmt.Errorf("llms.txt source is empty")
	}

	chunks := splitKnowledgeChunks(content)
	if len(chunks) == 0 {
		return nil, "", fmt.Errorf("llms.txt source has no indexable content")
	}

	source := &wpKnowledgeSource{
		URL:          rawURL,
		FetchedAt:    time.Now(),
		ETag:         strings.TrimSpace(resp.Header.Get("ETag")),
		LastModified: strings.TrimSpace(resp.Header.Get("Last-Modified")),
		ContentChars: len([]rune(content)),
		Chunks:       chunks,
	}

	p.mu.Lock()
	p.ensureCacheLocked()
	p.cache[cacheKey] = source
	p.mu.Unlock()

	return source, "", nil
}

func (p *WordPressKnowledgeProfile) ensureCacheLocked() {
	if p.cache == nil {
		p.cache = map[string]*wpKnowledgeSource{}
	}
}

func (p *WordPressKnowledgeProfile) httpClient() *http.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client == nil {
		p.client = &http.Client{Timeout: 30 * time.Second}
	}
	return p.client
}

func (p *WordPressKnowledgeProfile) cacheKey(urlVal, token string) string {
	return urlVal + "|" + token
}

func validateKnowledgeURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid LLMS_TXT_URL: %s", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("LLMS_TXT_URL must use http or https")
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("LLMS_TXT_URL must include a hostname")
	}

	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		return nil, fmt.Errorf("localhost is not allowed for LLMS_TXT_URL")
	}

	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return nil, fmt.Errorf("private or local IPs are not allowed for LLMS_TXT_URL")
		}
	}

	return u, nil
}

func readWithLimit(r io.Reader, maxBytes int) ([]byte, error) {
	limited := io.LimitReader(r, int64(maxBytes)+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed reading llms.txt source: %s", err)
	}
	if len(body) > maxBytes {
		return nil, fmt.Errorf("llms.txt source exceeds MAX_DOWNLOAD_BYTES (%d)", maxBytes)
	}
	return body, nil
}

func splitKnowledgeChunks(content string) []wpKnowledgeChunk {
	lines := strings.Split(content, "\n")
	currentHeading := "Overview"
	var current strings.Builder
	var chunks []wpKnowledgeChunk

	flush := func() {
		text := strings.TrimSpace(current.String())
		current.Reset()
		if text == "" {
			return
		}
		for _, c := range splitChunkBySize(currentHeading, text, 1800) {
			chunks = append(chunks, c)
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isMarkdownHeading(trimmed) {
			flush()
			currentHeading = normalizeHeading(trimmed)
			continue
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	flush()

	if len(chunks) == 0 {
		content = strings.TrimSpace(content)
		if content != "" {
			chunks = splitChunkBySize("Content", content, 1800)
		}
	}

	return chunks
}

func splitChunkBySize(heading, text string, maxChars int) []wpKnowledgeChunk {
	parts := splitParagraphs(text)
	if len(parts) == 0 {
		return nil
	}

	var chunks []wpKnowledgeChunk
	var current strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		nextLen := runeLen(current.String()) + runeLen(part) + 2
		if current.Len() > 0 && nextLen > maxChars {
			chunks = append(chunks, buildKnowledgeChunk(heading, current.String()))
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(part)
	}
	if current.Len() > 0 {
		chunks = append(chunks, buildKnowledgeChunk(heading, current.String()))
	}
	return chunks
}

func buildKnowledgeChunk(heading, content string) wpKnowledgeChunk {
	clean := normalizeWhitespace(content)
	return wpKnowledgeChunk{
		Heading: heading,
		Content: clean,
		lower:   strings.ToLower(heading + "\n" + clean),
	}
}

func splitParagraphs(text string) []string {
	raw := strings.Split(text, "\n\n")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func isMarkdownHeading(line string) bool {
	if line == "" || !strings.HasPrefix(line, "#") {
		return false
	}
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	return i > 0 && i < len(line) && line[i] == ' '
}

func normalizeHeading(line string) string {
	line = strings.TrimLeft(line, "#")
	line = strings.TrimSpace(line)
	if line == "" {
		return "Section"
	}
	return line
}

func scoreKnowledgeChunk(chunk wpKnowledgeChunk, queryLower string, terms []string) float64 {
	score := 0.0
	if strings.Contains(chunk.lower, queryLower) {
		score += 8
	}
	headingLower := strings.ToLower(chunk.Heading)
	for _, term := range terms {
		if term == "" {
			continue
		}
		occ := strings.Count(chunk.lower, term)
		if occ > 0 {
			score += float64(occ)
		}
		if strings.Contains(headingLower, term) {
			score += 2.5
		}
	}
	if len(chunk.Content) < 1200 {
		score += 0.3
	}
	return score
}

func tokenize(input string) []string {
	var b strings.Builder
	for _, r := range strings.ToLower(input) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Fields(b.String())
}

func uniqueTerms(input []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(input))
	for _, term := range input {
		if runeLen(term) <= 1 {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}
	return out
}

func uniqueHeadings(chunks []wpKnowledgeChunk) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, chunk := range chunks {
		if _, ok := seen[chunk.Heading]; ok {
			continue
		}
		seen[chunk.Heading] = struct{}{}
		out = append(out, chunk.Heading)
	}
	sort.Strings(out)
	return out
}

func truncateStrings(items []string, max int) []string {
	if max <= 0 || len(items) <= max {
		return items
	}
	return items[:max]
}

func normalizeWhitespace(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}

func runeLen(s string) int {
	return len([]rune(s))
}

func envInt(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return v
}
