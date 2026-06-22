package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReranker_InterfaceTypeOllama(t *testing.T) {
	r, err := newReranker(&RerankerConfig{
		ModelName:     "qwen3:latest",
		BaseURL:       "http://localhost:11435",
		InterfaceType: "ollama",
		Provider:      "generic",
	})
	require.NoError(t, err)
	assert.IsType(t, &OllamaChatReranker{}, r)
}

func TestOllamaChatReranker_Rerank(t *testing.T) {
	t.Run("normalizes and sorts results", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/chat", r.URL.Path)
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]any{
					"content": `{"results":[{"index":0,"relevance_score":2.5},{"index":1,"relevance_score":1.5}]}`,
				},
			}))
		}))
		defer server.Close()

		reranker, err := NewOllamaChatReranker(&RerankerConfig{
			ModelName: "dengcao/Qwen3-Reranker-4B:Q4_K_M",
			ModelID:   "rr-1",
			BaseURL:   server.URL,
		})
		require.NoError(t, err)

		results, err := reranker.Rerank(context.Background(), "apple", []string{"apple", "banana"})
		require.NoError(t, err)
		require.Len(t, results, 2)
		assert.Equal(t, 0, results[0].Index)
		assert.Equal(t, "apple", results[0].Document.Text)
		assert.Equal(t, 1.0, results[0].RelevanceScore)
		assert.Equal(t, 1, results[1].Index)
		assert.Equal(t, 0.0, results[1].RelevanceScore)
	})

	t.Run("retries once after invalid JSON", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			content := "not-json"
			if attempts == 2 {
				content = `{"results":[{"index":0,"relevance_score":0.1},{"index":1,"relevance_score":0.9}]}`
			}
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]any{
					"content": content,
				},
			}))
		}))
		defer server.Close()

		reranker, err := NewOllamaChatReranker(&RerankerConfig{
			ModelName: "dengcao/Qwen3-Reranker-4B:Q4_K_M",
			BaseURL:   server.URL,
		})
		require.NoError(t, err)

		results, err := reranker.Rerank(context.Background(), "apple", []string{"a", "b"})
		require.NoError(t, err)
		require.Len(t, results, 2)
		assert.Equal(t, 2, attempts)
		assert.Equal(t, 1, results[0].Index)
		assert.Equal(t, 0, results[1].Index)
	})
}

func TestNormalizeRerankScores(t *testing.T) {
	t.Run("keeps scores already in range", func(t *testing.T) {
		assert.Equal(t, []float64{0.2, 0.8}, normalizeRerankScores([]float64{0.2, 0.8}))
	})

	t.Run("normalizes out-of-range scores", func(t *testing.T) {
		assert.Equal(t, []float64{0, 0.5, 1}, normalizeRerankScores([]float64{-2, 0, 2}))
	})

	t.Run("handles identical scores", func(t *testing.T) {
		assert.Equal(t, []float64{1, 1}, normalizeRerankScores([]float64{3, 3}))
		assert.Equal(t, []float64{0, 0}, normalizeRerankScores([]float64{-3, -3}))
	})
}
