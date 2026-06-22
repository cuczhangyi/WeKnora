package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

const (
	ollamaChatRerankEndpoint = "/api/chat"
	ollamaChatRetryCount     = 2
)

type OllamaChatReranker struct {
	modelName     string
	modelID       string
	baseURL       string
	client        *http.Client
	customHeaders map[string]string
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRerankRequest struct {
	Model    string                 `json:"model"`
	Messages []ollamaChatMessage    `json:"messages"`
	Stream   bool                   `json:"stream"`
	Think    bool                   `json:"think"`
	Format   interface{}            `json:"format"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

type ollamaChatRerankResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error"`
}

type ollamaChatRerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

type ollamaChatRerankEnvelope struct {
	Results []ollamaChatRerankResult `json:"results"`
}

func NewOllamaChatReranker(config *RerankerConfig) (*OllamaChatReranker, error) {
	if config == nil {
		return nil, fmt.Errorf("reranker config is required")
	}
	if strings.TrimSpace(config.ModelName) == "" {
		return nil, fmt.Errorf("ollama rerank model name is required")
	}
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, fmt.Errorf("ollama rerank base URL is required")
	}
	if _, err := buildOllamaChatURL(config.BaseURL); err != nil {
		return nil, err
	}

	return &OllamaChatReranker{
		modelName: strings.TrimSpace(config.ModelName),
		modelID:   config.ModelID,
		baseURL:   strings.TrimSpace(config.BaseURL),
		client: &http.Client{
			Transport: &http.Transport{
				DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
				TLSHandshakeTimeout: 10 * time.Second,
				IdleConnTimeout:     90 * time.Second,
				MaxIdleConns:        10,
			},
		},
	}, nil
}

func (r *OllamaChatReranker) SetCustomHeaders(headers map[string]string) {
	r.customHeaders = headers
}

func (r *OllamaChatReranker) GetModelName() string {
	return r.modelName
}

func (r *OllamaChatReranker) GetModelID() string {
	return r.modelID
}

func (r *OllamaChatReranker) Rerank(
	ctx context.Context, query string, documents []string,
) ([]RankResult, error) {
	if len(documents) == 0 {
		return nil, nil
	}

	endpoint, err := buildOllamaChatURL(r.baseURL)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := range ollamaChatRetryCount {
		content, err := r.callOllamaChat(ctx, endpoint, query, documents, attempt == 0)
		if err != nil {
			lastErr = err
			continue
		}

		results, err := parseOllamaChatRerankResults(content, documents)
		if err == nil {
			return results, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("ollama chat rerank failed after retry: %w", lastErr)
}

func (r *OllamaChatReranker) callOllamaChat(
	ctx context.Context,
	endpoint string,
	query string,
	documents []string,
	firstAttempt bool,
) (string, error) {
	systemPrompt, userPrompt := buildOllamaChatRerankPrompt(query, documents, firstAttempt)
	payload := ollamaChatRerankRequest{
		Model: r.modelName,
		Messages: []ollamaChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Stream: false,
		Think:  false,
		Format: buildOllamaChatRerankSchema(len(documents)),
		Options: map[string]interface{}{
			"temperature": 0,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal ollama rerank request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create ollama rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	secutils.ApplyCustomHeaders(req, r.customHeaders)

	logger.Debugf(ctx, "Ollama chat rerank request: model=%s endpoint=%s docs=%d",
		r.modelName, endpoint, len(documents))

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call ollama rerank endpoint: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read ollama rerank response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama rerank http status %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var decoded ollamaChatRerankResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return "", fmt.Errorf("unmarshal ollama rerank response: %w", err)
	}
	if decoded.Error != "" {
		return "", fmt.Errorf("ollama rerank error: %s", decoded.Error)
	}
	if strings.TrimSpace(decoded.Message.Content) == "" {
		return "", fmt.Errorf("ollama rerank returned empty content")
	}

	return decoded.Message.Content, nil
}

func buildOllamaChatURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("invalid ollama rerank base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid ollama rerank base URL: %s", baseURL)
	}
	parsed.Path = ollamaChatRerankEndpoint
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func buildOllamaChatRerankPrompt(
	query string, documents []string, firstAttempt bool,
) (string, string) {
	systemPrompt := strings.TrimSpace(`
You are a reranking model.
Return only JSON that matches the provided schema.
Score every document for relevance to the query.
Rules:
- Include every document exactly once.
- The number of results must equal the number of documents.
- index must match the provided document index.
- relevance_score must be a number between 0 and 1.
- Do not output markdown, code fences, commentary, or extra keys.
`)

	var builder strings.Builder
	if firstAttempt {
		builder.WriteString(fmt.Sprintf("There are %d documents.\n", len(documents)))
		builder.WriteString(fmt.Sprintf("Return exactly %d results.\n\n", len(documents)))
		builder.WriteString("Query:\n")
	} else {
		builder.WriteString("Your previous answer was invalid.\n")
		builder.WriteString(fmt.Sprintf("There are %d documents, so results must contain exactly %d items.\n",
			len(documents), len(documents)))
		builder.WriteString("Return only JSON that matches the schema.\n")
		builder.WriteString("No markdown. No explanations. No code fences.\n\n")
		builder.WriteString("Query:\n")
	}
	builder.WriteString(query)
	builder.WriteString("\n\nDocuments:\n")
	for idx, doc := range documents {
		builder.WriteString(fmt.Sprintf("Document %d:\n%s\n\n", idx, doc))
	}
	return systemPrompt, builder.String()
}

func buildOllamaChatRerankSchema(docCount int) map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"results": map[string]interface{}{
				"type":     "array",
				"minItems": docCount,
				"maxItems": docCount,
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"index": map[string]interface{}{
							"type": "integer",
						},
						"relevance_score": map[string]interface{}{
							"type": "number",
						},
					},
					"required": []string{"index", "relevance_score"},
				},
			},
		},
		"required": []string{"results"},
	}
}

func parseOllamaChatRerankResults(content string, documents []string) ([]RankResult, error) {
	var envelope ollamaChatRerankEnvelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &envelope); err != nil {
		return nil, fmt.Errorf("parse ollama rerank JSON: %w", err)
	}
	if len(envelope.Results) != len(documents) {
		return nil, fmt.Errorf("ollama rerank returned %d results for %d documents",
			len(envelope.Results), len(documents))
	}

	scoresByIndex := make(map[int]float64, len(documents))
	for _, item := range envelope.Results {
		if item.Index < 0 || item.Index >= len(documents) {
			return nil, fmt.Errorf("ollama rerank returned out-of-range index %d", item.Index)
		}
		if _, exists := scoresByIndex[item.Index]; exists {
			return nil, fmt.Errorf("ollama rerank returned duplicate index %d", item.Index)
		}
		if math.IsNaN(item.RelevanceScore) || math.IsInf(item.RelevanceScore, 0) {
			return nil, fmt.Errorf("ollama rerank returned invalid score for index %d", item.Index)
		}
		scoresByIndex[item.Index] = item.RelevanceScore
	}
	rawScores := make([]float64, len(documents))
	for idx := range documents {
		score, ok := scoresByIndex[idx]
		if !ok {
			return nil, fmt.Errorf("ollama rerank missing score for index %d", idx)
		}
		rawScores[idx] = score
	}

	normalizedScores := normalizeRerankScores(rawScores)
	results := make([]RankResult, 0, len(documents))
	for idx, score := range normalizedScores {
		results = append(results, RankResult{
			Index:          idx,
			Document:       DocumentInfo{Text: documents[idx]},
			RelevanceScore: score,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].RelevanceScore == results[j].RelevanceScore {
			return results[i].Index < results[j].Index
		}
		return results[i].RelevanceScore > results[j].RelevanceScore
	})

	return results, nil
}

func normalizeRerankScores(scores []float64) []float64 {
	if len(scores) == 0 {
		return nil
	}

	normalized := make([]float64, len(scores))
	copy(normalized, scores)

	allWithinRange := true
	minScore, maxScore := normalized[0], normalized[0]
	for _, score := range normalized[1:] {
		if score < minScore {
			minScore = score
		}
		if score > maxScore {
			maxScore = score
		}
	}
	for _, score := range normalized {
		if score < 0 || score > 1 {
			allWithinRange = false
			break
		}
	}
	if allWithinRange {
		return normalized
	}

	if maxScore == minScore {
		fill := clamp01(maxScore)
		for idx := range normalized {
			normalized[idx] = fill
		}
		return normalized
	}

	scale := maxScore - minScore
	for idx, score := range normalized {
		normalized[idx] = clamp01((score - minScore) / scale)
	}
	return normalized
}

func clamp01(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}
