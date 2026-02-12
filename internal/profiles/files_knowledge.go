package profiles

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type FilesKnowledgeProfile struct {
	mu     sync.RWMutex
	cache  map[string]*filesKnowledgeSource
	client *http.Client
}

type filesKnowledgeIndexDoc struct {
	ConnectionID string                    `json:"connectionId"`
	Version      int64                     `json:"version"`
	GeneratedAt  string                    `json:"generatedAt"`
	Files        []filesKnowledgeIndexFile `json:"files"`
	Chunks       []filesKnowledgeChunkDoc  `json:"chunks"`
}

type filesKnowledgeIndexFile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Ext        string `json:"ext"`
	Size       int64  `json:"size"`
	ChunkCount int    `json:"chunkCount"`
}

type filesKnowledgeChunkDoc struct {
	ID         string `json:"id"`
	FileID     string `json:"fileId"`
	FileName   string `json:"fileName"`
	Heading    string `json:"heading"`
	Content    string `json:"content"`
	ChunkIndex int    `json:"chunkIndex"`
}

type filesKnowledgeSource struct {
	URL         string
	Version     string
	FetchedAt   time.Time
	Files       []filesKnowledgeIndexFile
	Chunks      []filesKnowledgeChunk
	ChunksCount int
}

type filesKnowledgeChunk struct {
	ID         string
	FileID     string
	FileName   string
	Heading    string
	Content    string
	ChunkIndex int
	lower      string
}

type filesKnowledgeMatch struct {
	Chunk filesKnowledgeChunk
	Score float64
}

func (p *FilesKnowledgeProfile) ID() string { return "files-knowledge" }

func (p *FilesKnowledgeProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "search_files_knowledge",
			Description: "Search uploaded files and return the most relevant passages",
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
			Description: "Show source/index status (URL, version, files, chunks)",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "list_files",
			Description: "List uploaded files currently included in the index",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum files to return (default 50)",
					},
				},
			},
		},
		{
			Name:        "refresh_index",
			Description: "Force-refresh the files index cache immediately",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

func (p *FilesKnowledgeProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "search_files_knowledge":
		return p.searchFilesKnowledge(args, env)
	case "source_status":
		return p.sourceStatus(env, false)
	case "list_files":
		return p.listFiles(args, env)
	case "refresh_index":
		return p.sourceStatus(env, true)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *FilesKnowledgeProfile) searchFilesKnowledge(args map[string]interface{}, env map[string]string) (string, error) {
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

	matches := make([]filesKnowledgeMatch, 0, len(source.Chunks))
	for _, chunk := range source.Chunks {
		score := scoreFilesKnowledgeChunk(chunk, queryLower, terms)
		if score <= 0 {
			continue
		}
		matches = append(matches, filesKnowledgeMatch{Chunk: chunk, Score: score})
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
	if source.Version != "" {
		out.WriteString(fmt.Sprintf("Version: %s\n", source.Version))
	}
	out.WriteString(fmt.Sprintf("Fetched: %s\n", source.FetchedAt.UTC().Format(time.RFC3339)))
	out.WriteString(fmt.Sprintf("Indexed files: %d\n", len(source.Files)))
	out.WriteString(fmt.Sprintf("Indexed chunks: %d\n", source.ChunksCount))
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
			"\n\n%d) %s\nFile: %s\nScore: %.2f\n%s",
			i+1,
			match.Chunk.Heading,
			match.Chunk.FileName,
			match.Score,
			snippet,
		))
	}

	return out.String(), nil
}

func (p *FilesKnowledgeProfile) sourceStatus(env map[string]string, forceRefresh bool) (string, error) {
	source, warning, err := p.ensureSource(env, forceRefresh)
	if err != nil {
		return "", err
	}

	fileNames := make([]string, 0, len(source.Files))
	for _, file := range source.Files {
		fileNames = append(fileNames, file.Name)
	}
	sort.Strings(fileNames)

	resp := map[string]interface{}{
		"sourceUrl":   source.URL,
		"version":     source.Version,
		"fetchedAt":   source.FetchedAt.UTC().Format(time.RFC3339),
		"filesCount":  len(source.Files),
		"chunksCount": source.ChunksCount,
		"sampleFiles": truncateStrings(fileNames, 10),
	}
	if warning != "" {
		resp["warning"] = warning
	}

	b, _ := json.MarshalIndent(resp, "", "  ")
	return string(b), nil
}

func (p *FilesKnowledgeProfile) listFiles(args map[string]interface{}, env map[string]string) (string, error) {
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

	files := append([]filesKnowledgeIndexFile(nil), source.Files...)
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	if len(files) > limit {
		files = files[:limit]
	}

	resp := map[string]interface{}{
		"sourceUrl": source.URL,
		"count":     len(files),
		"files":     files,
	}
	if warning != "" {
		resp["warning"] = warning
	}

	b, _ := json.MarshalIndent(resp, "", "  ")
	return string(b), nil
}

func (p *FilesKnowledgeProfile) ensureSource(env map[string]string, force bool) (*filesKnowledgeSource, string, error) {
	rawURL := strings.TrimSpace(env["FILES_INDEX_URL"])
	if rawURL == "" {
		return nil, "", fmt.Errorf("FILES_INDEX_URL is not configured yet. Upload files from the Dublyo dashboard first")
	}
	parsedURL, err := validateKnowledgeURL(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf(strings.ReplaceAll(err.Error(), "LLMS_TXT_URL", "FILES_INDEX_URL"))
	}
	rawURL = parsedURL.String()

	version := strings.TrimSpace(env["FILES_INDEX_VERSION"])
	refreshSeconds := envInt(env["REFRESH_INTERVAL_SECONDS"], 300)
	if refreshSeconds < 10 {
		refreshSeconds = 10
	}
	if refreshSeconds > 86400 {
		refreshSeconds = 86400
	}

	p.mu.RLock()
	current := p.cache[rawURL]
	p.mu.RUnlock()

	if !force && current != nil && (version == "" || current.Version == version) &&
		time.Since(current.FetchedAt) < time.Duration(refreshSeconds)*time.Second {
		return current, "", nil
	}

	maxBytes := envInt(env["MAX_DOWNLOAD_BYTES"], 50*1024*1024)
	if maxBytes < 1024 {
		maxBytes = 1024
	}
	if maxBytes > 150*1024*1024 {
		maxBytes = 150 * 1024 * 1024
	}

	client := p.httpClient()
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build request: %s", err)
	}

	userAgent := strings.TrimSpace(env["USER_AGENT"])
	if userAgent == "" {
		userAgent = "Dublyo-Files-Knowledge/1.0"
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json,text/plain;q=0.5,*/*;q=0.1")

	resp, err := client.Do(req)
	if err != nil {
		if current != nil {
			return current, fmt.Sprintf("using cached index because refresh failed: %s", err), nil
		}
		return nil, "", fmt.Errorf("failed to fetch files index: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if current != nil {
			return current, fmt.Sprintf("using cached index because endpoint returned HTTP %d", resp.StatusCode), nil
		}
		return nil, "", fmt.Errorf("files index endpoint returned HTTP %d", resp.StatusCode)
	}

	body, err := readWithLimit(resp.Body, maxBytes)
	if err != nil {
		if current != nil {
			return current, fmt.Sprintf("using cached index because response read failed: %s", err), nil
		}
		return nil, "", err
	}

	var doc filesKnowledgeIndexDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		if current != nil {
			return current, fmt.Sprintf("using cached index because JSON parsing failed: %s", err), nil
		}
		return nil, "", fmt.Errorf("invalid files index JSON: %s", err)
	}

	chunkList := make([]filesKnowledgeChunk, 0, len(doc.Chunks))
	for _, c := range doc.Chunks {
		content := strings.TrimSpace(c.Content)
		if content == "" {
			continue
		}
		heading := strings.TrimSpace(c.Heading)
		if heading == "" {
			heading = "Section"
		}
		fileName := strings.TrimSpace(c.FileName)
		if fileName == "" {
			fileName = "unknown"
		}
		chunkList = append(chunkList, filesKnowledgeChunk{
			ID:         c.ID,
			FileID:     c.FileID,
			FileName:   fileName,
			Heading:    heading,
			Content:    content,
			ChunkIndex: c.ChunkIndex,
			lower:      strings.ToLower(heading + "\n" + fileName + "\n" + content),
		})
	}

	sourceVersion := version
	if doc.Version > 0 {
		sourceVersion = fmt.Sprintf("%d", doc.Version)
	}

	source := &filesKnowledgeSource{
		URL:         rawURL,
		Version:     sourceVersion,
		FetchedAt:   time.Now(),
		Files:       doc.Files,
		Chunks:      chunkList,
		ChunksCount: len(chunkList),
	}

	p.mu.Lock()
	p.ensureCacheLocked()
	p.cache[rawURL] = source
	p.mu.Unlock()

	return source, "", nil
}

func (p *FilesKnowledgeProfile) ensureCacheLocked() {
	if p.cache == nil {
		p.cache = map[string]*filesKnowledgeSource{}
	}
}

func (p *FilesKnowledgeProfile) httpClient() *http.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client == nil {
		p.client = &http.Client{Timeout: 30 * time.Second}
	}
	return p.client
}

func scoreFilesKnowledgeChunk(chunk filesKnowledgeChunk, queryLower string, terms []string) float64 {
	score := 0.0
	if strings.Contains(chunk.lower, queryLower) {
		score += 8
	}

	headingLower := strings.ToLower(chunk.Heading)
	fileLower := strings.ToLower(chunk.FileName)
	for _, term := range terms {
		if term == "" {
			continue
		}
		occ := strings.Count(chunk.lower, term)
		if occ > 0 {
			score += float64(occ)
		}
		if strings.Contains(headingLower, term) {
			score += 2.0
		}
		if strings.Contains(fileLower, term) {
			score += 1.5
		}
	}

	if len(chunk.Content) < 1200 {
		score += 0.3
	}
	return score
}
