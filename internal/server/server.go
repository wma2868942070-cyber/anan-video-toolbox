// Package server exposes the OpenAI-compatible HTTP surface backed by the
// Leonardo cookie pool service. Mirrors the core endpoints of app/main.py.
package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/adobe"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/config"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/service"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/store"
)

// Server holds dependencies shared across HTTP handlers.
type Server struct {
	cfg              config.Config
	store            *store.Store
	service          *service.LeonardoPool
	adobe            *adobe.Client
	engine           *gin.Engine
	videoMu          sync.RWMutex
	videoJobs        map[string]*videoJob
	videoRequestJobs map[string]string
	adobeMu          sync.RWMutex
	adobeJobs        map[string]*adobeVideoJob
	adobeRequestJobs map[string]string
	// Adobe's model catalog changes far less often than generation requests.
	// Keep the last successful catalog in memory so every image/video request
	// does not perform a round-trip through the sidecar first.
	adobeModelCacheMu   sync.RWMutex
	adobeModelRefreshMu sync.Mutex
	adobeModelCache     []adobeModelVariant
	adobeModelCacheAt   time.Time
	adobeModelCacheTTL  time.Duration
}

// New wires up the gin engine with the core API routes.
func New(cfg config.Config, st *store.Store, svc *service.LeonardoPool) *Server {
	s := &Server{
		cfg:                cfg,
		store:              st,
		service:            svc,
		adobe:              adobe.NewClient(),
		videoJobs:          make(map[string]*videoJob),
		videoRequestJobs:   make(map[string]string),
		adobeJobs:          make(map[string]*adobeVideoJob),
		adobeRequestJobs:   make(map[string]string),
		adobeModelCacheTTL: 5 * time.Minute,
	}
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(s.corsMiddleware())
	s.registerRoutes(engine)
	s.engine = engine
	return s
}

// Run starts the HTTP server.
func (s *Server) Run(addr string) error {
	return s.engine.Run(addr)
}

func (s *Server) registerRoutes(r *gin.Engine) {
	r.GET("/", func(c *gin.Context) { c.Redirect(http.StatusTemporaryRedirect, "/infinite-canvas/#/canvas") })
	// /v1 is an API base path rather than an API operation. Send browser visits
	// to the bundled canvas; account management is desktop-only.
	r.GET("/v1", func(c *gin.Context) { c.Redirect(http.StatusTemporaryRedirect, "/infinite-canvas/#/canvas") })
	r.GET("/canvas", func(c *gin.Context) { c.Redirect(http.StatusTemporaryRedirect, "/infinite-canvas/#/canvas") })
	r.GET("/health", s.handleHealth)
	r.GET("/admin", s.handleLeonardoAdmin)
	r.GET("/admin/status", s.handleLeonardoAdminStatus)
	r.GET("/adobe/health", s.handleAdobeHealth)
	r.GET("/adobe/generated/:filename", s.handleAdobeGenerated)
	if strings.TrimSpace(s.cfg.CanvasDir) != "" {
		r.StaticFS("/infinite-canvas", gin.Dir(s.cfg.CanvasDir, false))
	}
	protected := r.Group("/")
	protected.Use(s.requireAPIKey)
	protected.GET("/v1/models", s.handleModels)
	protected.POST("/v1/images/generations", s.handleGenerate)
	protected.POST("/v1/images/edits", s.handleGenerateEdit)
	protected.POST("/v1/videos/generations", s.handleGenerateVideo)
	protected.POST("/v1/video/generations", s.handleGenerateVideo)
	protected.POST("/v1/videos", s.handleCreateVideoTask)
	protected.GET("/v1/videos/:id", s.handleGetVideoTask)
	protected.GET("/v1/videos/:id/content", s.handleGetVideoContent)
	protected.GET("/adobe/v1/models", s.handleAdobeModels)
	protected.POST("/adobe/v1/images/generations", s.handleAdobeImageGeneration)
	protected.POST("/adobe/v1/images/edits", s.handleAdobeImageEdit)
	protected.POST("/adobe/v1/videos", s.handleAdobeCreateVideoTask)
	protected.GET("/adobe/v1/videos/:id", s.handleAdobeGetVideoTask)
	protected.GET("/adobe/v1/videos/:id/content", s.handleAdobeGetVideoContent)
}

// corsMiddleware lets browser-hosted canvas/workflow tools call the local
// proxy. ANAN_VIDEO_TOOLBOX_CORS_ORIGINS accepts "*" or a comma-separated allowlist.
// API authentication still applies to every protected endpoint.
func (s *Server) corsMiddleware() gin.HandlerFunc {
	allowed := parseAllowedOrigins(s.cfg.CORSOrigins)
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin != "" {
			if allowed["*"] {
				c.Header("Access-Control-Allow-Origin", "*")
			} else if allowed[origin] {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
			} else if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key")
		c.Header("Access-Control-Max-Age", "86400")
		if strings.EqualFold(c.GetHeader("Access-Control-Request-Private-Network"), "true") {
			c.Header("Access-Control-Allow-Private-Network", "true")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func parseAllowedOrigins(raw string) map[string]bool {
	out := make(map[string]bool)
	for _, item := range strings.Split(raw, ",") {
		if origin := strings.TrimSpace(item); origin != "" {
			out[origin] = true
		}
	}
	return out
}

func requestIsLoopback(c *gin.Context) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr))
	if err != nil {
		host = strings.Trim(strings.TrimSpace(c.Request.RemoteAddr), "[]")
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) requireAPIKey(c *gin.Context) {
	if s.cfg.APIKey == "" {
		c.Next()
		return
	}
	supplied := strings.TrimSpace(c.GetHeader("X-API-Key"))
	if supplied == "" {
		authorization := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
			supplied = strings.TrimSpace(authorization[7:])
		}
	}
	// The bundled basketikun/infinite-canvas instance runs on the same local
	// origin. It uses a non-secret marker key so the actual server API key never
	// needs to be embedded into the AGPL frontend bundle.
	if (supplied == "local-anan-video-toolbox" || supplied == "local-leostudio") && s.isTrustedLocalCanvasRequest(c) {
		c.Next()
		return
	}
	if subtle.ConstantTimeCompare([]byte(supplied), []byte(s.cfg.APIKey)) != 1 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"message": "API 密钥无效"},
		})
		return
	}
	c.Next()
}

func (s *Server) isTrustedLocalCanvasRequest(c *gin.Context) bool {
	if !requestIsLoopback(c) {
		return false
	}
	referer := strings.ToLower(strings.TrimSpace(c.GetHeader("Referer")))
	if referer == "" {
		return false
	}
	prefixes := []string{
		"http://127.0.0.1:" + s.cfg.Port + "/infinite-canvas/",
		"http://localhost:" + s.cfg.Port + "/infinite-canvas/",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(referer, prefix) {
			return true
		}
	}
	return false
}

func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// handleModels implements the OpenAI model-list shape expected by many
// canvas clients. Image rows use the Leonardo UUID as id; anan-default is the
// branded stable alias. The previous alias remains accepted but stays hidden.
func (s *Server) handleModels(c *gin.Context) {
	rows, err := s.store.ListModels()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	created := time.Now().Unix()
	data := make([]gin.H, 0, len(rows)+len(service.VideoModels)+2)
	data = append(data, gin.H{
		"id":       "anan-default",
		"object":   "model",
		"created":  created,
		"owned_by": "anan-video-toolbox",
		"name":     "Leonardo 默认图片模型",
		"type":     "image",
	})
	data = append(data, gin.H{
		"id":              "leostudio-default",
		"object":          "model",
		"created":         created,
		"owned_by":        "anan-video-toolbox",
		"name":            "Leonardo 默认图片模型",
		"type":            "image",
		"canonical_model": "anan-default",
	})
	for _, row := range rows {
		data = append(data, gin.H{
			"id":         row.ModelID,
			"object":     "model",
			"created":    row.CreatedAt,
			"owned_by":   "leonardo",
			"name":       row.Name,
			"type":       "image",
			"is_default": row.IsDefault == 1,
		})
	}
	for _, model := range service.VideoModels {
		if model.Hidden {
			// Website-only entries are intentionally not advertised to clients.
			// Advertising them creates a local async task even though Leonardo
			// will not create a provider generation for that model value.
			continue
		}
		data = append(data, gin.H{
			"id":                   model.Slug,
			"object":               "model",
			"created":              created,
			"owned_by":             "leonardo",
			"name":                 model.Name,
			"type":                 "video",
			"family":               model.Family,
			"provider_model":       model.LeonardoModelValue(),
			"audio_policy":         model.AudioPolicy,
			"supports_audio":       model.SupportsAudio(),
			"duration_options":     model.DurationOptions,
			"default_duration":     model.DefaultDuration,
			"resolution_options":   model.SupportedModes,
			"default_resolution":   model.DefaultMode,
			"supports_start_frame": model.SupportsRefImage,
			"requires_start_frame": model.RequiresRefImage,
			"credit_cost_mode":     "dynamic",
			"credit_cost_unit":     "credits",
		})
		for _, alias := range model.Aliases {
			// Only expose the familiar MiniMax H3 label in model pickers. The
			// remaining aliases are accepted for compatibility but would create
			// duplicate rows in OpenAI-compatible clients.
			if alias != "minimaxh3" {
				continue
			}
			data = append(data, gin.H{
				"id":                   alias,
				"object":               "model",
				"created":              created,
				"owned_by":             "leonardo",
				"name":                 model.Name,
				"type":                 "video",
				"family":               model.Family,
				"provider_model":       model.LeonardoModelValue(),
				"canonical_model":      model.Slug,
				"audio_policy":         model.AudioPolicy,
				"supports_audio":       model.SupportsAudio(),
				"duration_options":     model.DurationOptions,
				"default_duration":     model.DefaultDuration,
				"resolution_options":   model.SupportedModes,
				"default_resolution":   model.DefaultMode,
				"supports_start_frame": model.SupportsRefImage,
				"requires_start_frame": model.RequiresRefImage,
				"credit_cost_mode":     "dynamic",
				"credit_cost_unit":     "credits",
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}

// openAIImageRequest mirrors OpenAIImageRequest in main.py.
type openAIImageRequest struct {
	Prompt          string   `json:"prompt"`
	Model           string   `json:"model"`
	N               int      `json:"n"`
	Size            string   `json:"size"`
	AspectRatio     string   `json:"aspect_ratio"`
	ImageURL        string   `json:"image_url"`
	ImageURLs       []string `json:"image_urls"`
	ClientRequestID string   `json:"client_request_id"`
}

func (s *Server) handleGenerate(c *gin.Context) {
	var payload openAIImageRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "JSON 格式无效："+err.Error())
		return
	}
	if strings.TrimSpace(payload.Prompt) == "" {
		writeError(c, http.StatusBadRequest, "prompt 不能为空")
		return
	}
	requestID := strings.TrimSpace(payload.ClientRequestID)
	if requestID != "" {
		s.videoMu.RLock()
		existingID := s.videoRequestJobs[requestID]
		existing := s.videoJobs[existingID]
		if existing != nil {
			copy := *existing
			s.videoMu.RUnlock()
			c.JSON(http.StatusAccepted, s.videoTaskEnvelope(&copy))
			return
		}
		s.videoMu.RUnlock()
	}
	if reused, ok := s.reusedVideoResponse("leonardo", payload.Model, payload.Prompt, requestID); ok {
		job := &videoJob{ID: newVideoJobID(), CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(), Status: "completed", Model: strings.TrimSpace(payload.Model), Result: reused, ClientRequestID: requestID}
		s.videoMu.Lock()
		s.videoJobs[job.ID] = job
		if requestID != "" {
			s.videoRequestJobs[requestID] = job.ID
		}
		s.videoMu.Unlock()
		c.JSON(http.StatusAccepted, s.videoTaskEnvelope(job))
		return
	}
	if reused, ok := s.reusedVideoResponse("leonardo", payload.Model, payload.Prompt, payload.ClientRequestID); ok {
		c.JSON(http.StatusOK, reused)
		return
	}
	if payload.N == 0 {
		payload.N = 1
	}
	if payload.N < 1 || payload.N > 4 {
		writeError(c, http.StatusBadRequest, "n 必须在 1 到 4 之间")
		return
	}

	modelID, aspect, refs, err := s.resolveGenerationRequest(payload)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if s.tryReuseSuccessfulImage(c, "leonardo", modelID, payload.Prompt, payload.ClientRequestID) {
		return
	}

	res, err := s.service.Generate(service.GenerateRequest{
		Prompt:             payload.Prompt,
		N:                  payload.N,
		ModelID:            modelID,
		AspectRatio:        aspect,
		ReferenceImageURLs: refs,
		ClientRequestID:    payload.ClientRequestID,
	})
	if err != nil {
		var pe *service.PublicError
		if errors.As(err, &pe) {
			writeError(c, pe.Status, pe.Message)
			return
		}
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, res)
}

// handleGenerateEdit accepts the OpenAI-compatible multipart image-edit
// request used by Infinite Canvas. Reference bytes stay in the generation
// request so each Leonardo account uploads its own account-scoped init image
// before submitting the generation.
func (s *Server) handleGenerateEdit(c *gin.Context) {
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		writeError(c, http.StatusBadRequest, "表单格式无效："+err.Error())
		return
	}

	n, _ := strconv.Atoi(c.PostForm("n"))
	if n == 0 {
		n = 1
	}
	if n < 1 || n > 4 {
		writeError(c, http.StatusBadRequest, "n 必须在 1 到 4 之间")
		return
	}
	payload := openAIImageRequest{
		Prompt:          c.PostForm("prompt"),
		Model:           c.PostForm("model"),
		N:               n,
		Size:            c.PostForm("size"),
		AspectRatio:     c.PostForm("aspect_ratio"),
		ClientRequestID: c.PostForm("client_request_id"),
	}
	if strings.TrimSpace(payload.Prompt) == "" {
		writeError(c, http.StatusBadRequest, "prompt 不能为空")
		return
	}

	modelID, aspect, _, err := s.resolveGenerationRequest(payload)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if s.tryReuseSuccessfulImage(c, "leonardo", modelID, payload.Prompt, payload.ClientRequestID) {
		return
	}

	var headers []*multipart.FileHeader
	if c.Request.MultipartForm != nil {
		for _, key := range []string{"image", "image[]", "input_reference[]", "input_reference"} {
			headers = append(headers, c.Request.MultipartForm.File[key]...)
		}
	}
	if len(headers) == 0 {
		writeError(c, http.StatusBadRequest, "至少需要一张参考图片")
		return
	}
	if len(headers) > 3 {
		headers = headers[:3]
	}
	references := make([]service.ReferenceImageInput, 0, len(headers))
	for _, header := range headers {
		content, ext, err := readReferenceImageFile(header)
		if err != nil {
			writeError(c, http.StatusBadRequest, err.Error())
			return
		}
		references = append(references, service.ReferenceImageInput{Content: content, Ext: ext})
	}

	res, err := s.service.Generate(service.GenerateRequest{
		Prompt:          payload.Prompt,
		N:               payload.N,
		ModelID:         modelID,
		AspectRatio:     aspect,
		ReferenceImages: references,
		ClientRequestID: payload.ClientRequestID,
	})
	if err != nil {
		var pe *service.PublicError
		if errors.As(err, &pe) {
			writeError(c, pe.Status, pe.Message)
			return
		}
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}

// resolveGenerationRequest mirrors _resolve_generation_request in main.py.
func (s *Server) resolveGenerationRequest(payload openAIImageRequest) (string, string, []string, error) {
	requestedModel := strings.TrimSpace(payload.Model)
	modelID := requestedModel
	if requestedModel == "" || isDefaultImageAlias(requestedModel) {
		def, err := s.store.DefaultModelID()
		if err != nil {
			return "", "", nil, err
		}
		modelID = def
	} else {
		// Some canvas tools submit the display name selected from /v1/models.
		// Resolve that name back to the UUID while still accepting raw UUIDs.
		models, err := s.store.ListModels()
		if err != nil {
			return "", "", nil, err
		}
		for _, model := range models {
			if strings.EqualFold(strings.TrimSpace(model.Name), requestedModel) {
				modelID = model.ModelID
				break
			}
		}
	}
	if modelID == "" {
		return "", "", nil, errors.New("尚未配置模型")
	}

	aspect, _ := s.store.GetSetting("default_aspect_ratio", "1:1")
	if a := strings.TrimSpace(payload.AspectRatio); a != "" {
		aspect = a
	}
	if size := strings.TrimSpace(payload.Size); size != "" {
		if mapped, ok := service.SizeAliasToAspect[size]; ok {
			aspect = mapped
		}
	}
	if !service.IsKnownAspect(aspect) {
		return "", "", nil, errors.New("aspect_ratio 必须是 16:9、9:16、1:1 或 4:3")
	}

	var refs []string
	if u := strings.TrimSpace(payload.ImageURL); u != "" {
		refs = append(refs, u)
	}
	for _, u := range payload.ImageURLs {
		u = strings.TrimSpace(u)
		if u != "" {
			refs = append(refs, u)
		}
	}
	return modelID, aspect, refs, nil
}

func isDefaultImageAlias(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "anan", "anan-default", "anan-video-toolbox", "leostudio", "leostudio-default", "leonardo", "default", "dall-e-3", "gpt-image-1":
		return true
	default:
		return false
	}
}

func writeError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{"message": message},
	})
}

// videoGenerationRequest is the public input for POST /v1/videos/generations.
// It mirrors the OpenAI image schema where it overlaps and adds video-specific
// fields (duration, audio, resolution).
type videoGenerationRequest struct {
	Prompt          string `json:"prompt"`
	Model           string `json:"model"`
	AspectRatio     string `json:"aspect_ratio"`
	Resolution      string `json:"resolution"` // 480p / 720p / 1080p
	Duration        int    `json:"duration"`   // seconds
	Audio           bool   `json:"audio"`
	ImageURL        string `json:"image_url"` // optional image-to-video start frame
	ImageID         string `json:"image_id"`
	ClientRequestID string `json:"client_request_id"`
}

func (s *Server) handleGenerateVideo(c *gin.Context) {
	var payload videoGenerationRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "JSON 格式无效："+err.Error())
		return
	}
	if strings.TrimSpace(payload.Prompt) == "" {
		writeError(c, http.StatusBadRequest, "prompt 不能为空")
		return
	}
	if reused, ok := s.reusedVideoResponse("leonardo", payload.Model, payload.Prompt, payload.ClientRequestID); ok {
		c.JSON(http.StatusOK, reused)
		return
	}

	res, err := s.service.GenerateVideo(service.VideoRequest{
		Prompt:          payload.Prompt,
		ModelSlug:       payload.Model,
		AspectRatio:     payload.AspectRatio,
		Resolution:      payload.Resolution,
		Duration:        payload.Duration,
		Audio:           payload.Audio,
		ImageURL:        payload.ImageURL,
		ImageID:         payload.ImageID,
		ClientRequestID: payload.ClientRequestID,
	})
	if err != nil {
		var pe *service.PublicError
		if errors.As(err, &pe) {
			writeError(c, pe.Status, pe.Message)
			return
		}
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, res)
}

// videoTaskRequest is intentionally compatible with both OpenAI/Sora-style
// clients and the small /v1/video/generations contract used by canvas tools.
// The worker still delegates to the same cookie-pool service, so there is one
// account rotation and error policy for sync and async callers.
type videoTaskRequest struct {
	Prompt          string `json:"prompt"`
	Model           string `json:"model"`
	Seconds         int    `json:"seconds"`
	Duration        int    `json:"duration"`
	Size            string `json:"size"`
	AspectRatio     string `json:"aspect_ratio"`
	Resolution      string `json:"resolution"`
	Audio           *bool  `json:"audio"`
	ImageURL        string `json:"image_url"`
	ImageID         string `json:"image_id"`
	InputReference  string `json:"input_reference"`
	ClientRequestID string `json:"client_request_id"`
}

type videoJob struct {
	ID              string
	CreatedAt       int64
	UpdatedAt       int64
	Status          string
	Model           string
	Result          *service.VideoResponse
	Error           string
	ClientRequestID string
}

func newVideoJobID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "video-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return "video-" + hex.EncodeToString(buf)
}

func (s *Server) handleCreateVideoTask(c *gin.Context) {
	payload, referenceBytes, referenceExt, err := bindVideoTaskRequest(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(payload.Prompt) == "" {
		writeError(c, http.StatusBadRequest, "prompt 不能为空")
		return
	}
	requestID := strings.TrimSpace(payload.ClientRequestID)
	if requestID != "" {
		s.videoMu.RLock()
		existingID := s.videoRequestJobs[requestID]
		existing := s.videoJobs[existingID]
		if existing != nil {
			copy := *existing
			s.videoMu.RUnlock()
			c.JSON(http.StatusAccepted, s.videoTaskEnvelope(&copy))
			return
		}
		s.videoMu.RUnlock()
	}
	if reused, ok := s.reusedVideoResponse("leonardo", payload.Model, payload.Prompt, requestID); ok {
		job := &videoJob{ID: newVideoJobID(), CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(), Status: "completed", Model: strings.TrimSpace(payload.Model), Result: reused, ClientRequestID: requestID}
		s.videoMu.Lock()
		s.videoJobs[job.ID] = job
		if requestID != "" {
			s.videoRequestJobs[requestID] = job.ID
		}
		s.videoMu.Unlock()
		c.JSON(http.StatusAccepted, s.videoTaskEnvelope(job))
		return
	}
	duration := payload.Duration
	if duration == 0 {
		duration = payload.Seconds
	}
	imageURL := strings.TrimSpace(payload.ImageURL)
	if imageURL == "" {
		imageURL = strings.TrimSpace(payload.InputReference)
	}
	audio := false
	if payload.Audio != nil {
		audio = *payload.Audio
	}
	job := &videoJob{ID: newVideoJobID(), CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(), Status: "queued", Model: strings.TrimSpace(payload.Model), ClientRequestID: requestID}
	s.videoMu.Lock()
	s.videoJobs[job.ID] = job
	if requestID != "" {
		s.videoRequestJobs[requestID] = job.ID
	}
	// Keep the local task table bounded; generated assets remain in generation logs.
	if len(s.videoJobs) > 200 {
		for id, old := range s.videoJobs {
			if old.Status == "completed" || old.Status == "failed" {
				delete(s.videoJobs, id)
				break
			}
		}
	}
	s.videoMu.Unlock()

	go func() {
		s.videoMu.Lock()
		job.Status = "in_progress"
		job.UpdatedAt = time.Now().Unix()
		s.videoMu.Unlock()
		aspect := strings.TrimSpace(payload.AspectRatio)
		if aspect == "" {
			if mapped, ok := service.SizeAliasToAspect[strings.TrimSpace(payload.Size)]; ok {
				aspect = mapped
			}
		}
		res, err := s.service.GenerateVideo(service.VideoRequest{
			Prompt: payload.Prompt, ModelSlug: payload.Model, AspectRatio: aspect,
			Resolution: payload.Resolution, Duration: duration, Audio: audio,
			ImageURL: imageURL, ImageID: strings.TrimSpace(payload.ImageID),
			ImageBytes: referenceBytes, ImageExt: referenceExt,
			ClientRequestID: requestID,
		})
		s.videoMu.Lock()
		job.UpdatedAt = time.Now().Unix()
		if err != nil {
			job.Status = "failed"
			var pe *service.PublicError
			if errors.As(err, &pe) {
				job.Error = pe.Message
			} else {
				job.Error = err.Error()
			}
			if requestID != "" {
				delete(s.videoRequestJobs, requestID)
			}
		} else {
			job.Status = "completed"
			job.Result = res
		}
		s.videoMu.Unlock()
	}()

	c.JSON(http.StatusAccepted, s.videoTaskEnvelope(job))
}

func bindVideoTaskRequest(c *gin.Context) (videoTaskRequest, []byte, string, error) {
	var payload videoTaskRequest
	contentType := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	if !strings.HasPrefix(contentType, "multipart/form-data") && !strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		if err := c.ShouldBindJSON(&payload); err != nil {
			return payload, nil, "", errors.New("JSON 格式无效：" + err.Error())
		}
		return payload, nil, "", nil
	}

	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
			return payload, nil, "", errors.New("表单格式无效：" + err.Error())
		}
	} else if err := c.Request.ParseForm(); err != nil {
		return payload, nil, "", errors.New("表单格式无效：" + err.Error())
	}
	payload.Prompt = c.PostForm("prompt")
	payload.Model = c.PostForm("model")
	payload.Seconds, _ = strconv.Atoi(c.PostForm("seconds"))
	payload.Duration, _ = strconv.Atoi(c.PostForm("duration"))
	payload.Size = c.PostForm("size")
	payload.AspectRatio = c.PostForm("aspect_ratio")
	payload.Resolution = c.PostForm("resolution")
	if payload.Resolution == "" {
		payload.Resolution = c.PostForm("resolution_name")
	}
	payload.ImageURL = c.PostForm("image_url")
	payload.ImageID = c.PostForm("image_id")
	payload.InputReference = c.PostForm("input_reference")
	payload.ClientRequestID = c.PostForm("client_request_id")
	if raw := firstNonEmpty(c.PostForm("audio"), c.PostForm("generate_audio")); raw != "" {
		value := strings.EqualFold(raw, "true") || raw == "1"
		payload.Audio = &value
	}

	if c.Request.MultipartForm == nil {
		return payload, nil, "", nil
	}
	for _, key := range []string{"input_reference[]", "input_reference", "image"} {
		files := c.Request.MultipartForm.File[key]
		if len(files) == 0 {
			continue
		}
		file, err := files[0].Open()
		if err != nil {
			return payload, nil, "", errors.New("读取参考图片失败：" + err.Error())
		}
		bytes, readErr := io.ReadAll(io.LimitReader(file, (30<<20)+1))
		_ = file.Close()
		if readErr != nil {
			return payload, nil, "", errors.New("读取参考图片失败：" + readErr.Error())
		}
		if len(bytes) > 30<<20 {
			return payload, nil, "", errors.New("参考图片不能超过 30MB")
		}
		ext, err := referenceImageExtension(files[0].Filename, files[0].Header.Get("Content-Type"), bytes)
		if err != nil {
			return payload, nil, "", err
		}
		return payload, bytes, ext, nil
	}
	return payload, nil, "", nil
}

func readReferenceImageFile(header *multipart.FileHeader) ([]byte, string, error) {
	file, err := header.Open()
	if err != nil {
		return nil, "", errors.New("读取参考图片失败：" + err.Error())
	}
	content, readErr := io.ReadAll(io.LimitReader(file, (30<<20)+1))
	_ = file.Close()
	if readErr != nil {
		return nil, "", errors.New("读取参考图片失败：" + readErr.Error())
	}
	if len(content) == 0 {
		return nil, "", errors.New("参考图片内容为空")
	}
	if len(content) > 30<<20 {
		return nil, "", errors.New("参考图片不能超过 30MB")
	}
	ext, err := referenceImageExtension(header.Filename, header.Header.Get("Content-Type"), content)
	if err != nil {
		return nil, "", err
	}
	return content, ext, nil
}

// referenceImageExtension trusts the actual bytes before the browser-supplied
// filename. Infinite Canvas stores generated JPEG/WebP nodes with friendly
// names ending in .png, and using that suffix as the S3 Content-Type can make
// Leonardo reject or indefinitely moderate an otherwise valid image.
func referenceImageExtension(filename, declaredType string, content []byte) (string, error) {
	fromMIME := func(value string) string {
		switch strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0])) {
		case "image/jpeg", "image/jpg":
			return "jpg"
		case "image/png":
			return "png"
		case "image/webp":
			return "webp"
		default:
			return ""
		}
	}
	if ext := fromMIME(http.DetectContentType(content)); ext != "" {
		return ext, nil
	}
	if ext := fromMIME(declaredType); ext != "" {
		return ext, nil
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if ext == "jpeg" {
		ext = "jpg"
	}
	if ext == "jpg" || ext == "png" || ext == "webp" {
		return ext, nil
	}
	return "", errors.New("参考图片格式不支持，请使用 JPG、PNG 或 WebP")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Server) handleGetVideoTask(c *gin.Context) {
	id := c.Param("id")
	s.videoMu.RLock()
	job, ok := s.videoJobs[id]
	if ok {
		copy := *job
		job = &copy
	}
	s.videoMu.RUnlock()
	if !ok {
		writeError(c, http.StatusNotFound, "视频任务不存在")
		return
	}
	out := gin.H{"id": job.ID, "object": "video", "status": job.Status, "model": job.Model, "created_at": job.CreatedAt, "updated_at": job.UpdatedAt}
	if job.Status == "failed" {
		out["error"] = gin.H{"message": job.Error}
	}
	if job.Result != nil && len(job.Result.Data) > 0 {
		out["video_url"] = job.Result.Data[0].MP4URL
		out["content_url"] = "/v1/videos/" + job.ID + "/content"
		out["result"] = job.Result
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) handleGetVideoContent(c *gin.Context) {
	id := c.Param("id")
	s.videoMu.RLock()
	job, ok := s.videoJobs[id]
	var url string
	if ok && job.Result != nil && len(job.Result.Data) > 0 {
		url = job.Result.Data[0].MP4URL
	}
	s.videoMu.RUnlock()
	if !ok {
		writeError(c, http.StatusNotFound, "视频任务不存在")
		return
	}
	if url == "" {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"message": "视频尚未完成"}})
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, url)
}
