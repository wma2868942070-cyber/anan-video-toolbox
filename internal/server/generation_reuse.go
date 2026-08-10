package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/service"
)

// tryReuseSuccessfulImage returns a previously completed result for an
// idempotent client request. This is deliberately a best-effort fast path:
// errors and old rows simply fall through to a normal provider request.
func (s *Server) tryReuseSuccessfulImage(c *gin.Context, provider, model, prompt, requestID string) bool {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return false
	}
	row, err := s.store.FindSuccessfulGenerationByClientRequest(provider, "image", requestID, model, prompt)
	if err != nil {
		if err != sql.ErrNoRows {
			// Do not turn a catalog/migration problem into a generation failure.
			c.Set("generation_reuse_error", err.Error())
		}
		return false
	}
	var urls []string
	if json.Unmarshal([]byte(row.ImageURLsJSON), &urls) != nil || len(urls) == 0 {
		return false
	}
	data := make([]gin.H, 0, len(urls))
	for _, url := range urls {
		if strings.TrimSpace(url) != "" {
			data = append(data, gin.H{"url": url})
		}
	}
	if len(data) == 0 {
		return false
	}
	c.JSON(http.StatusOK, gin.H{
		"created": row.CreatedAt,
		"data":    data,
		"provider": gin.H{
			"name":              provider,
			"generation_id":     row.ProviderGenerationID,
			"model_id":          row.ModelID,
			"reused":            true,
			"client_request_id": requestID,
		},
	})
	return true
}

// reusedVideoResponse reconstructs the OpenAI-compatible response from the
// local material log. This lets VideoClaw retry a request after its page or
// backend was restarted without submitting another paid provider job.
func (s *Server) reusedVideoResponse(provider, model, prompt, requestID string) (*service.VideoResponse, bool) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, false
	}
	row, err := s.store.FindSuccessfulGenerationByClientRequest(provider, "video", requestID, model, prompt)
	if err == sql.ErrNoRows && strings.TrimSpace(model) != "" {
		row, err = s.store.FindSuccessfulGenerationByClientRequest(provider, "video", requestID, "", prompt)
	}
	if err != nil {
		return nil, false
	}
	var urls []string
	if json.Unmarshal([]byte(row.ImageURLsJSON), &urls) != nil {
		return nil, false
	}
	items := make([]service.VideoResponseItem, 0, len(urls))
	for _, value := range urls {
		if value = strings.TrimSpace(value); value != "" {
			items = append(items, service.VideoResponseItem{URL: value, MP4URL: value})
		}
	}
	if len(items) == 0 {
		return nil, false
	}
	meta := service.VideoResponseProvider{
		GenerationID:    row.ProviderGenerationID,
		Model:           row.ModelID,
		AspectRatio:     row.AspectRatio,
		ClientRequestID: requestID,
	}
	var raw map[string]any
	if json.Unmarshal([]byte(row.MetadataJSON), &raw) == nil {
		if value, ok := raw["account_id"].(string); ok {
			_ = value // account identity remains in the library row
		}
		if value, ok := raw["duration"].(float64); ok {
			meta.Duration = int(value)
		}
		if value, ok := raw["audio"].(bool); ok {
			meta.Audio = value
		}
		if value, ok := raw["resolution"].(string); ok {
			meta.Resolution = value
		}
	}
	return &service.VideoResponse{Created: row.CreatedAt, Data: items, Provider: meta}, true
}

func (s *Server) videoTaskEnvelope(job *videoJob) gin.H {
	out := gin.H{
		"id": job.ID, "object": "video", "status": job.Status,
		"model": job.Model, "created_at": job.CreatedAt,
		"status_url":  "/v1/videos/" + job.ID,
		"content_url": "/v1/videos/" + job.ID + "/content",
	}
	if job.UpdatedAt != 0 {
		out["updated_at"] = job.UpdatedAt
	}
	if job.Error != "" {
		out["error"] = gin.H{"message": job.Error}
	}
	if job.Result != nil && len(job.Result.Data) > 0 {
		out["video_url"] = job.Result.Data[0].MP4URL
		out["result"] = job.Result
	}
	return out
}

func (s *Server) reusedAdobeVideoJob(model, prompt, requestID string) (*adobeVideoJob, bool) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, false
	}
	row, err := s.store.FindSuccessfulGenerationByClientRequest("adobe", "video", requestID, model, prompt)
	if err == sql.ErrNoRows && strings.TrimSpace(model) != "" {
		row, err = s.store.FindSuccessfulGenerationByClientRequest("adobe", "video", requestID, "", prompt)
	}
	if err != nil {
		return nil, false
	}
	var urls []string
	if json.Unmarshal([]byte(row.ImageURLsJSON), &urls) != nil || len(urls) == 0 {
		return nil, false
	}
	resultURL := ""
	for _, value := range urls {
		if strings.TrimSpace(value) != "" {
			resultURL = strings.TrimSpace(value)
			break
		}
	}
	if resultURL == "" {
		return nil, false
	}
	var provider map[string]any
	_ = json.Unmarshal([]byte(row.MetadataJSON), &provider)
	if provider == nil {
		provider = map[string]any{"name": "adobe"}
	}
	return &adobeVideoJob{
		ID: newVideoJobID(), CreatedAt: row.CreatedAt, UpdatedAt: row.CreatedAt,
		Status: "completed", Model: row.ModelID, Prompt: row.Prompt,
		AspectRatio: row.AspectRatio, ResultURL: resultURL,
		ProviderAccountID: row.ProviderAccountID, ProviderMetadata: provider,
		ClientRequestID: requestID,
	}, true
}

// generationMetadataWithProvider merges a request id into provider metadata
// while retaining account information returned by Adobe.
func generationMetadataWithProvider(provider map[string]any, requestID string) string {
	if provider == nil {
		provider = map[string]any{}
	}
	if requestID = strings.TrimSpace(requestID); requestID != "" {
		provider["client_request_id"] = requestID
	}
	payload, err := json.Marshal(provider)
	if err != nil {
		return "{}"
	}
	return string(payload)
}
