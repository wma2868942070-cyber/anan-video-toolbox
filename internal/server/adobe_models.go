package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type adobeModelVariant struct {
	ID                     string
	CanonicalID            string
	Family                 string
	MediaType              string
	Duration               int
	Resolution             string
	AspectRatio            string
	SupportsAudio          bool
	SupportsImageReference bool
	SupportsStartFrame     bool
	SupportsVideoReference bool
	SupportsAudioReference bool
}

var adobeCanonicalModelOrder = []string{
	"firefly-nano-banana-pro",
	"firefly-nano-banana",
	"firefly-nano-banana2",
	"firefly-gpt-image",
	"firefly-sora2",
	"firefly-sora2-pro",
	"firefly-gemini-omni",
	"firefly-veo31",
	"firefly-veo31-ref",
	"firefly-veo31-fast",
	"firefly-kling-o3",
	"firefly-kling3",
	"firefly-seedance20",
	"firefly-seedance20-fast",
}

func (s *Server) loadAdobeModelVariants(ctx context.Context) ([]adobeModelVariant, error) {
	now := time.Now()
	if cached, ok := s.cachedAdobeModelVariants(now); ok {
		return cached, nil
	}

	// Several VideoClaw/Canvas requests can arrive together. Only one of them
	// is allowed to refresh the catalog; the others reuse the result instead of
	// creating a burst of identical /v1/models calls against Adobe2API.
	s.adobeModelRefreshMu.Lock()
	defer s.adobeModelRefreshMu.Unlock()
	if cached, ok := s.cachedAdobeModelVariants(time.Now()); ok {
		return cached, nil
	}

	variants, err := s.fetchAdobeModelVariants(ctx)
	if err != nil {
		// A temporary sidecar/proxy failure should not make an otherwise valid
		// generation fail just because the catalog TTL elapsed. Serve the last
		// known-good catalog and retry a refresh after the normal TTL.
		if cached, ok := s.snapshotAdobeModelVariants(); ok {
			s.adobeModelCacheMu.Lock()
			s.adobeModelCacheAt = time.Now()
			s.adobeModelCacheMu.Unlock()
			return cached, nil
		}
		return nil, err
	}
	if len(variants) == 0 {
		if cached, ok := s.snapshotAdobeModelVariants(); ok {
			return cached, nil
		}
		return variants, nil
	}

	s.adobeModelCacheMu.Lock()
	s.adobeModelCache = append([]adobeModelVariant(nil), variants...)
	s.adobeModelCacheAt = time.Now()
	s.adobeModelCacheMu.Unlock()
	return append([]adobeModelVariant(nil), variants...), nil
}

func (s *Server) fetchAdobeModelVariants(ctx context.Context) ([]adobeModelVariant, error) {
	payload, err := s.adobe.ServiceJSON(ctx, http.MethodGet, "/v1/models", nil)
	if err != nil {
		return nil, err
	}
	rawModels, _ := payload["data"].([]any)
	variants := make([]adobeModelVariant, 0, len(rawModels))
	for _, raw := range rawModels {
		row, _ := raw.(map[string]any)
		id := strings.TrimSpace(fmt.Sprint(row["id"]))
		if id == "" {
			continue
		}
		meta := adobeModelMetadataFromRow(row)
		variants = append(variants, adobeModelVariant{
			ID:                     id,
			CanonicalID:            canonicalAdobeModelID(id),
			Family:                 strings.TrimSpace(fmt.Sprint(meta["family"])),
			MediaType:              strings.TrimSpace(fmt.Sprint(meta["type"])),
			Duration:               adobeAnyInt(meta["duration"]),
			Resolution:             strings.ToLower(strings.TrimSpace(fmt.Sprint(meta["resolution"]))),
			AspectRatio:            normalizeAdobeAspect(strings.TrimSpace(fmt.Sprint(meta["aspect_ratio"]))),
			SupportsAudio:          adobeAnyBool(meta["supports_audio"]),
			SupportsImageReference: adobeAnyBool(meta["supports_image_reference"]),
			SupportsStartFrame:     adobeAnyBool(meta["supports_start_frame"]),
			SupportsVideoReference: adobeAnyBool(meta["supports_video_reference"]),
			SupportsAudioReference: adobeAnyBool(meta["supports_audio_reference"]),
		})
	}
	return variants, nil
}

func (s *Server) cachedAdobeModelVariants(now time.Time) ([]adobeModelVariant, bool) {
	s.adobeModelCacheMu.RLock()
	defer s.adobeModelCacheMu.RUnlock()
	if len(s.adobeModelCache) == 0 {
		return nil, false
	}
	ttl := s.adobeModelCacheTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if now.Sub(s.adobeModelCacheAt) >= ttl {
		return nil, false
	}
	return append([]adobeModelVariant(nil), s.adobeModelCache...), true
}

func (s *Server) snapshotAdobeModelVariants() ([]adobeModelVariant, bool) {
	s.adobeModelCacheMu.RLock()
	defer s.adobeModelCacheMu.RUnlock()
	if len(s.adobeModelCache) == 0 {
		return nil, false
	}
	return append([]adobeModelVariant(nil), s.adobeModelCache...), true
}

// adobeModelMetadataFromRow prefers the sidecar's explicit contract and only
// falls back to the legacy ID heuristic when an older sidecar omits fields.
func adobeModelMetadataFromRow(row map[string]any) map[string]any {
	id := strings.TrimSpace(fmt.Sprint(row["id"]))
	meta := adobeModelMetadata(id, strings.TrimSpace(fmt.Sprint(row["description"])))
	for _, key := range []string{
		"family", "display_name", "name", "type", "duration", "resolution",
		"aspect_ratio", "supports_audio", "supports_image_reference",
		"supports_start_frame", "supports_video_reference", "supports_audio_reference",
	} {
		if value, ok := row[key]; ok && value != nil && strings.TrimSpace(fmt.Sprint(value)) != "<nil>" {
			meta[key] = value
		}
	}
	// Newer Adobe2API rows expose the exact variant contract through
	// `default_*` and `*_options` fields instead of the legacy singular
	// `duration`/`resolution`/`aspect_ratio` fields.  Normalize both shapes so
	// the gateway does not silently collapse a full variant catalog into rows
	// with empty duration/ratio/resolution metadata.
	if adobeAnyInt(meta["duration"]) <= 0 {
		if value := adobeAnyInt(row["default_duration"]); value > 0 {
			meta["duration"] = value
		} else if values := adobeIntSlice(row["duration_options"]); len(values) > 0 {
			meta["duration"] = values[0]
		}
	}
	if strings.TrimSpace(fmt.Sprint(meta["resolution"])) == "" || strings.TrimSpace(fmt.Sprint(meta["resolution"])) == "<nil>" {
		if value := strings.TrimSpace(fmt.Sprint(row["default_resolution"])); value != "" && value != "<nil>" {
			meta["resolution"] = value
		} else if values := adobeStringSlice(row["resolution_options"]); len(values) > 0 {
			meta["resolution"] = values[0]
		}
	}
	if strings.TrimSpace(fmt.Sprint(meta["aspect_ratio"])) == "" || strings.TrimSpace(fmt.Sprint(meta["aspect_ratio"])) == "<nil>" {
		if value := strings.TrimSpace(fmt.Sprint(row["default_aspect_ratio"])); value != "" && value != "<nil>" {
			meta["aspect_ratio"] = value
		} else if values := adobeStringSlice(row["aspect_ratio_options"]); len(values) > 0 {
			meta["aspect_ratio"] = values[0]
		}
	}
	return meta
}

func adobeIntSlice(value any) []int {
	values := make([]int, 0)
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if parsed := adobeAnyInt(item); parsed > 0 {
				values = append(values, parsed)
			}
		}
	case []int:
		for _, item := range typed {
			if item > 0 {
				values = append(values, item)
			}
		}
	}
	return values
}

func adobeStringSlice(value any) []string {
	values := make([]string, 0)
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if parsed := strings.TrimSpace(fmt.Sprint(item)); parsed != "" && parsed != "<nil>" {
				values = append(values, parsed)
			}
		}
	case []string:
		for _, item := range typed {
			if parsed := strings.TrimSpace(item); parsed != "" {
				values = append(values, parsed)
			}
		}
	}
	return values
}

func canonicalAdobeModelID(id string) string {
	lower := strings.ToLower(strings.TrimSpace(id))
	// Match the longer/specialised ids before their base family prefixes.
	// For example firefly-sora2-pro-* also begins with firefly-sora2-*.
	matchOrder := []string{
		"firefly-nano-banana-pro",
		"firefly-sora2-pro",
		"firefly-veo31-ref",
		"firefly-veo31-fast",
		"firefly-seedance20-fast",
		"firefly-nano-banana2",
		"firefly-nano-banana",
		"firefly-gpt-image",
		"firefly-sora2",
		"firefly-gemini-omni",
		"firefly-veo31",
		"firefly-kling-o3",
		"firefly-kling3",
		"firefly-seedance20",
	}
	for _, canonical := range matchOrder {
		if lower == canonical || strings.HasPrefix(lower, canonical+"-") {
			return canonical
		}
	}
	return strings.TrimSpace(id)
}

func buildAdobeCanonicalModels(variants []adobeModelVariant) []any {
	groups := make(map[string][]adobeModelVariant)
	for _, variant := range variants {
		groups[variant.CanonicalID] = append(groups[variant.CanonicalID], variant)
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		left, right := adobeCanonicalOrderIndex(ids[i]), adobeCanonicalOrderIndex(ids[j])
		if left != right {
			return left < right
		}
		return ids[i] < ids[j]
	})

	models := make([]any, 0, len(ids))
	for _, id := range ids {
		items := groups[id]
		if len(items) == 0 {
			continue
		}
		family := items[0].Family
		if family == "" {
			family = id
		}
		mediaType := items[0].MediaType
		durations := make([]int, 0)
		resolutions := make([]string, 0)
		aspects := make([]string, 0)
		supportsAudio := false
		supportsImageReference := false
		supportsStartFrame := false
		supportsVideoReference := false
		supportsAudioReference := false
		for _, item := range items {
			if item.Duration > 0 {
				durations = append(durations, item.Duration)
			}
			if item.Resolution != "" {
				resolutions = append(resolutions, item.Resolution)
			}
			if item.AspectRatio != "" {
				aspects = append(aspects, item.AspectRatio)
			}
			supportsAudio = supportsAudio || item.SupportsAudio
			supportsImageReference = supportsImageReference || item.SupportsImageReference
			supportsStartFrame = supportsStartFrame || item.SupportsStartFrame
			supportsVideoReference = supportsVideoReference || item.SupportsVideoReference
			supportsAudioReference = supportsAudioReference || item.SupportsAudioReference
		}
		durations = uniqueSortedInts(durations)
		resolutions = uniqueSortedStrings(resolutions, adobeResolutionLess)
		aspects = uniqueSortedStrings(aspects, adobeAspectLess)
		models = append(models, map[string]any{
			"id":                       id,
			"object":                   "model",
			"owned_by":                 "adobe2api",
			"provider":                 "adobe",
			"name":                     family,
			"display_name":             family,
			"description":              family,
			"family":                   family,
			"type":                     mediaType,
			"duration_options":         durations,
			"resolution_options":       resolutions,
			"aspect_ratio_options":     aspects,
			"default_duration":         defaultAdobeDuration(durations),
			"default_resolution":       defaultAdobeResolution(mediaType, resolutions),
			"default_aspect_ratio":     defaultAdobeAspect(mediaType, aspects),
			"supports_audio":           supportsAudio,
			"supports_image_reference": supportsImageReference,
			"supports_start_frame":     supportsStartFrame,
			"supports_video_reference": supportsVideoReference,
			"supports_audio_reference": supportsAudioReference,
			"variant_count":            len(items),
		})
	}
	return models
}

// buildAdobeVariantModels is used by integrations that need the exact
// duration/ratio/resolution model IDs rather than the compact family list
// shown in the canvas picker. The family endpoint remains canonical so saved
// canvas projects do not suddenly gain hundreds of duplicate rows.
func buildAdobeVariantModels(variants []adobeModelVariant) []any {
	models := make([]any, 0, len(variants))
	for _, variant := range variants {
		if strings.TrimSpace(variant.ID) == "" {
			continue
		}
		family := strings.TrimSpace(variant.Family)
		if family == "" {
			family = variant.CanonicalID
		}
		mediaType := strings.TrimSpace(variant.MediaType)
		if mediaType == "" {
			mediaType = "video"
		}
		label := adobeVariantDisplayName(family, mediaType, variant.Duration, variant.Resolution, variant.AspectRatio, variant.SupportsAudio)
		row := map[string]any{
			"id":                       variant.ID,
			"object":                   "model",
			"owned_by":                 "adobe2api",
			"provider":                 "adobe",
			"name":                     label,
			"display_name":             label,
			"description":              label,
			"family":                   family,
			"canonical_model":          variant.CanonicalID,
			"type":                     mediaType,
			"duration_options":         []int{},
			"resolution_options":       []string{},
			"aspect_ratio_options":     []string{},
			"default_duration":         variant.Duration,
			"default_resolution":       variant.Resolution,
			"default_aspect_ratio":     variant.AspectRatio,
			"supports_audio":           variant.SupportsAudio,
			"supports_image_reference": variant.SupportsImageReference,
			"supports_start_frame":     variant.SupportsStartFrame,
			"supports_video_reference": variant.SupportsVideoReference,
			"supports_audio_reference": variant.SupportsAudioReference,
			"variant_count":            1,
		}
		if variant.Duration > 0 {
			row["duration_options"] = []int{variant.Duration}
		}
		if variant.Resolution != "" {
			row["resolution_options"] = []string{variant.Resolution}
		}
		if variant.AspectRatio != "" {
			row["aspect_ratio_options"] = []string{variant.AspectRatio}
		}
		models = append(models, row)
	}
	return models
}

func adobeVariantDisplayName(family, mediaType string, duration int, resolution, aspect string, supportsAudio bool) string {
	parts := []string{strings.TrimSpace(family)}
	if strings.EqualFold(mediaType, "image") {
		parts = append(parts, "图片")
	} else {
		parts = append(parts, "视频")
	}
	if duration > 0 {
		parts = append(parts, fmt.Sprintf("%d秒", duration))
	}
	if normalized := strings.ToLower(strings.TrimSpace(resolution)); normalized != "" {
		if strings.HasSuffix(normalized, "k") {
			normalized = strings.ToUpper(normalized)
		}
		parts = append(parts, normalized)
	}
	if normalized := normalizeAdobeAspect(aspect); normalized != "" {
		parts = append(parts, normalized)
	}
	if !strings.EqualFold(mediaType, "image") && supportsAudio {
		parts = append(parts, "有声")
	}
	return strings.Join(parts, " · ")
}

func selectAdobeModelVariant(variants []adobeModelVariant, requested, mediaType string, request map[string]any) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "", fmt.Errorf("model 不能为空")
	}
	for _, variant := range variants {
		if variant.ID == requested {
			if mediaType != "" && variant.MediaType != mediaType {
				return "", fmt.Errorf("Adobe 模型 %s 不是%s模型", requested, adobeMediaTypeName(mediaType))
			}
			return variant.ID, nil
		}
	}

	canonical := canonicalAdobeModelID(requested)
	candidates := make([]adobeModelVariant, 0)
	for _, variant := range variants {
		if variant.CanonicalID == canonical && (mediaType == "" || variant.MediaType == mediaType) {
			candidates = append(candidates, variant)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("Adobe 模型 %s 不存在或不支持%s生成", requested, adobeMediaTypeName(mediaType))
	}

	desiredDuration := requestedAdobeDuration(request, mediaType)
	desiredResolution := requestedAdobeResolution(request, mediaType)
	desiredAspect := requestedAdobeAspect(request, mediaType)
	best := candidates[0]
	bestScore := math.Inf(1)
	for _, candidate := range candidates {
		score := adobeVariantScore(candidate, desiredDuration, desiredResolution, desiredAspect)
		if score < bestScore || (score == bestScore && candidate.ID < best.ID) {
			best = candidate
			bestScore = score
		}
	}
	return best.ID, nil
}

func adobeVariantScore(candidate adobeModelVariant, desiredDuration int, desiredResolution, desiredAspect string) float64 {
	score := 0.0
	if desiredDuration > 0 && candidate.Duration > 0 {
		score += math.Abs(float64(candidate.Duration-desiredDuration)) * 100
		// When two variants are equally close, prefer enough duration instead
		// of silently shortening the requested clip.
		if candidate.Duration < desiredDuration {
			score += 1
		}
	}
	if desiredResolution != "" {
		if candidate.Resolution == "" {
			score += 5
		} else {
			left, right := adobeResolutionValue(candidate.Resolution), adobeResolutionValue(desiredResolution)
			if left > 0 && right > 0 {
				score += math.Abs(math.Log(left/right)) * 20
			} else if candidate.Resolution != desiredResolution {
				score += 20
			}
		}
	}
	if desiredAspect != "" && candidate.AspectRatio != "" {
		left, right := adobeAspectValue(candidate.AspectRatio), adobeAspectValue(desiredAspect)
		if left > 0 && right > 0 {
			score += math.Abs(math.Log(left/right)) * 40
		} else if candidate.AspectRatio != desiredAspect {
			score += 40
		}
	}
	return score
}

func requestedAdobeDuration(request map[string]any, mediaType string) int {
	if mediaType != "video" {
		return 0
	}
	for _, key := range []string{"duration", "seconds", "video_seconds"} {
		if value := adobeAnyInt(request[key]); value > 0 {
			return value
		}
	}
	return 6
}

func requestedAdobeResolution(request map[string]any, mediaType string) string {
	for _, key := range []string{"resolution", "resolution_name", "quality"} {
		if value := normalizeAdobeResolution(fmt.Sprint(request[key]), mediaType); value != "" {
			return value
		}
	}
	if width, height, ok := adobeDimensions(fmt.Sprint(request["size"])); ok {
		longSide := width
		if height > longSide {
			longSide = height
		}
		if mediaType == "image" {
			switch {
			case longSide > 3072:
				return "4k"
			case longSide > 1536:
				return "2k"
			default:
				return "1k"
			}
		}
		if longSide >= 1000 {
			return "1080p"
		}
		if longSide > 0 {
			return "720p"
		}
	}
	if mediaType == "image" {
		return "1k"
	}
	return "720p"
}

func requestedAdobeAspect(request map[string]any, mediaType string) string {
	for _, key := range []string{"aspect_ratio", "ratio", "size"} {
		if value := normalizeAdobeAspect(fmt.Sprint(request[key])); value != "" && value != "auto" {
			return value
		}
	}
	if mediaType == "image" {
		return "1:1"
	}
	return "16:9"
}

func normalizeAdobeResolution(value, mediaType string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "<nil>", "auto":
		return ""
	case "low":
		if mediaType == "image" {
			return "1k"
		}
		return "480p"
	case "medium", "hd":
		if mediaType == "image" {
			return "2k"
		}
		return "720p"
	case "standard":
		if mediaType == "image" {
			return "1k"
		}
		return "720p"
	case "high":
		if mediaType == "image" {
			return "4k"
		}
		return "1080p"
	}
	if strings.HasSuffix(value, "p") || strings.HasSuffix(value, "k") {
		return value
	}
	if _, err := strconv.Atoi(value); err == nil {
		if mediaType == "image" && (value == "1" || value == "2" || value == "4") {
			return value + "k"
		}
		return value + "p"
	}
	return ""
}

func normalizeAdobeAspect(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "<nil>" || value == "auto" {
		return ""
	}
	separator := ":"
	if strings.Contains(value, "x") {
		separator = "x"
	}
	parts := strings.Split(value, separator)
	if len(parts) != 2 {
		return ""
	}
	left, errLeft := strconv.Atoi(strings.TrimSpace(parts[0]))
	right, errRight := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errLeft != nil || errRight != nil || left <= 0 || right <= 0 {
		return ""
	}
	// Keep provider-native ratio labels such as 21:9 and 8:1 intact. Only
	// reduce large pixel dimensions (for example 1920x1080 -> 16:9).
	if left <= 100 && right <= 100 {
		return fmt.Sprintf("%d:%d", left, right)
	}
	divisor := adobeGCD(left, right)
	return fmt.Sprintf("%d:%d", left/divisor, right/divisor)
}

func adobeDimensions(value string) (int, int, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	parts := strings.Split(value, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, errWidth := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, errHeight := strconv.Atoi(strings.TrimSpace(parts[1]))
	return width, height, errWidth == nil && errHeight == nil && width > 0 && height > 0
}

func adobeAnyInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		result, _ := strconv.Atoi(string(typed))
		return result
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			return 0
		}
		if parsed, err := strconv.ParseFloat(text, 64); err == nil {
			return int(parsed)
		}
		return 0
	}
}

func adobeAnyBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "1" || strings.EqualFold(typed, "true")
	default:
		return false
	}
}

func adobeResolutionValue(value string) float64 {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasSuffix(value, "k") {
		number, _ := strconv.ParseFloat(strings.TrimSuffix(value, "k"), 64)
		return number * 1024
	}
	number, _ := strconv.ParseFloat(strings.TrimSuffix(value, "p"), 64)
	return number
}

func adobeAspectValue(value string) float64 {
	parts := strings.Split(normalizeAdobeAspect(value), ":")
	if len(parts) != 2 {
		return 0
	}
	left, _ := strconv.ParseFloat(parts[0], 64)
	right, _ := strconv.ParseFloat(parts[1], 64)
	if right == 0 {
		return 0
	}
	return left / right
}

func adobeGCD(left, right int) int {
	for right != 0 {
		left, right = right, left%right
	}
	if left <= 0 {
		return 1
	}
	return left
}

func adobeCanonicalOrderIndex(id string) int {
	for index, candidate := range adobeCanonicalModelOrder {
		if candidate == id {
			return index
		}
	}
	return len(adobeCanonicalModelOrder) + 1
}

func uniqueSortedInts(values []int) []int {
	seen := make(map[int]bool)
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func uniqueSortedStrings(values []string, less func(string, string) bool) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return less(result[i], result[j]) })
	return result
}

func adobeResolutionLess(left, right string) bool {
	return adobeResolutionValue(left) < adobeResolutionValue(right)
}

func adobeAspectLess(left, right string) bool {
	return adobeAspectValue(left) < adobeAspectValue(right)
}

func defaultAdobeDuration(options []int) int {
	if len(options) == 0 {
		return 0
	}
	best := options[0]
	bestDistance := math.Abs(float64(best - 6))
	for _, option := range options[1:] {
		distance := math.Abs(float64(option - 6))
		if distance < bestDistance || (distance == bestDistance && option > best) {
			best, bestDistance = option, distance
		}
	}
	return best
}

func defaultAdobeResolution(mediaType string, options []string) string {
	target := "720p"
	if mediaType == "image" {
		target = "1k"
	}
	return closestAdobeStringOption(target, options, adobeResolutionValue)
}

func defaultAdobeAspect(mediaType string, options []string) string {
	target := "16:9"
	if mediaType == "image" {
		target = "1:1"
	}
	return closestAdobeStringOption(target, options, adobeAspectValue)
}

func closestAdobeStringOption(target string, options []string, value func(string) float64) string {
	if len(options) == 0 {
		return ""
	}
	best := options[0]
	bestDistance := math.Inf(1)
	targetValue := value(target)
	for _, option := range options {
		if option == target {
			return option
		}
		optionValue := value(option)
		if optionValue <= 0 || targetValue <= 0 {
			continue
		}
		distance := math.Abs(math.Log(optionValue / targetValue))
		if distance < bestDistance {
			best, bestDistance = option, distance
		}
	}
	return best
}

func adobeMediaTypeName(mediaType string) string {
	if mediaType == "image" {
		return "图片"
	}
	if mediaType == "video" {
		return "视频"
	}
	return "所选类型"
}
