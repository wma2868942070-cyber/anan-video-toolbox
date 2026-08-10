package server

import (
	"reflect"
	"strings"
	"testing"
)

func TestAdobeModelMetadataClassifiesCapabilities(t *testing.T) {
	image := adobeModelMetadata("firefly-nano-banana2-4k-16x9", "Nano Banana 2")
	if image["type"] != "image" || image["family"] != "Nano Banana 2" {
		t.Fatalf("unexpected image metadata: %#v", image)
	}
	if image["resolution"] != "4k" || image["name"] != "Nano Banana 2 · 图片 · 4K · 16:9" {
		t.Fatalf("unexpected Chinese image name: %#v", image)
	}

	video := adobeModelMetadata("firefly-seedance20-10s-1080p-16x9", "Seedance 2.0")
	if video["type"] != "video" || video["duration"] != 10 || video["resolution"] != "1080p" || video["aspect_ratio"] != "16:9" {
		t.Fatalf("unexpected video metadata: %#v", video)
	}
	if video["supports_audio"] != true || video["supports_video_reference"] != true || video["supports_audio_reference"] != true {
		t.Fatalf("missing Seedance capabilities: %#v", video)
	}
	if video["name"] != "Seedance 2.0 · 视频 · 10秒 · 1080p · 16:9 · 有声" {
		t.Fatalf("unexpected Chinese video name: %#v", video)
	}
}

func TestBuildAdobeCanonicalModelsDeduplicatesAndUsesShortNames(t *testing.T) {
	variants := []adobeModelVariant{
		{ID: "firefly-seedance20-4s-16x9-720p", CanonicalID: "firefly-seedance20", Family: "Seedance 2.0", MediaType: "video", Duration: 4, Resolution: "720p", AspectRatio: "16:9", SupportsAudio: true},
		{ID: "firefly-seedance20-8s-9x16-1080p", CanonicalID: "firefly-seedance20", Family: "Seedance 2.0", MediaType: "video", Duration: 8, Resolution: "1080p", AspectRatio: "9:16", SupportsAudio: true, SupportsVideoReference: true, SupportsAudioReference: true},
		{ID: "firefly-gpt-image-1k-1x1", CanonicalID: "firefly-gpt-image", Family: "GPT Image", MediaType: "image", Resolution: "1k", AspectRatio: "1:1"},
		{ID: "firefly-gpt-image-4k-16x9", CanonicalID: "firefly-gpt-image", Family: "GPT Image", MediaType: "image", Resolution: "4k", AspectRatio: "16:9"},
	}
	models := buildAdobeCanonicalModels(variants)
	if len(models) != 2 {
		t.Fatalf("canonical model count = %d, want 2", len(models))
	}
	byID := make(map[string]map[string]any)
	for _, raw := range models {
		row := raw.(map[string]any)
		byID[row["id"].(string)] = row
		if strings.Contains(row["name"].(string), "·") {
			t.Fatalf("model name is still verbose: %q", row["name"])
		}
	}
	seedance := byID["firefly-seedance20"]
	if seedance["name"] != "Seedance 2.0" || seedance["variant_count"] != 2 {
		t.Fatalf("unexpected Seedance row: %#v", seedance)
	}
	if !reflect.DeepEqual(seedance["duration_options"], []int{4, 8}) || !reflect.DeepEqual(seedance["resolution_options"], []string{"720p", "1080p"}) {
		t.Fatalf("options were not aggregated: %#v", seedance)
	}
	if seedance["supports_video_reference"] != true || seedance["supports_audio_reference"] != true {
		t.Fatalf("capabilities were not aggregated: %#v", seedance)
	}
}

func TestBuildAdobeVariantModelsKeepsExactProviderIDs(t *testing.T) {
	variants := []adobeModelVariant{
		{ID: "firefly-seedance20-8s-16x9-1080p", CanonicalID: "firefly-seedance20", Family: "Seedance 2.0", MediaType: "video", Duration: 8, Resolution: "1080p", AspectRatio: "16:9", SupportsAudio: true},
		{ID: "firefly-gpt-image-4k-16x9", CanonicalID: "firefly-gpt-image", Family: "GPT Image", MediaType: "image", Resolution: "4k", AspectRatio: "16:9"},
	}
	rows := buildAdobeVariantModels(variants)
	if len(rows) != 2 {
		t.Fatalf("variant model count = %d, want 2", len(rows))
	}
	row := rows[0].(map[string]any)
	if row["id"] != "firefly-seedance20-8s-16x9-1080p" || row["canonical_model"] != "firefly-seedance20" {
		t.Fatalf("exact provider id was not retained: %#v", row)
	}
	if row["default_duration"] != 8 || row["default_resolution"] != "1080p" {
		t.Fatalf("variant defaults were not retained: %#v", row)
	}
	if row["name"] != "Seedance 2.0 · 视频 · 8秒 · 1080p · 16:9 · 有声" {
		t.Fatalf("variant display label is not descriptive: %#v", row["name"])
	}
}

func TestAdobeModelMetadataFromRowNormalizesVariantOptions(t *testing.T) {
	meta := adobeModelMetadataFromRow(map[string]any{
		"id":                   "firefly-veo31-8s-16x9-1080p",
		"type":                 "video",
		"family":               "Veo 3.1",
		"default_duration":     8,
		"default_resolution":   "1080p",
		"default_aspect_ratio": "16:9",
		"duration_options":     []any{4, 8},
		"resolution_options":   []any{"720p", "1080p"},
		"aspect_ratio_options": []any{"16:9", "9:16"},
		"supports_audio":       true,
		"supports_start_frame": true,
	})
	if meta["duration"] != 8 || meta["resolution"] != "1080p" || meta["aspect_ratio"] != "16:9" {
		t.Fatalf("variant defaults were not normalized: %#v", meta)
	}
	if meta["supports_audio"] != true || meta["supports_start_frame"] != true {
		t.Fatalf("variant capabilities were not preserved: %#v", meta)
	}
}

func TestSelectAdobeModelVariantMapsCanonicalRequestToClosestVariant(t *testing.T) {
	variants := []adobeModelVariant{
		{ID: "firefly-seedance20-4s-16x9-720p", CanonicalID: "firefly-seedance20", Family: "Seedance 2.0", MediaType: "video", Duration: 4, Resolution: "720p", AspectRatio: "16:9"},
		{ID: "firefly-seedance20-10s-9x16-1080p", CanonicalID: "firefly-seedance20", Family: "Seedance 2.0", MediaType: "video", Duration: 10, Resolution: "1080p", AspectRatio: "9:16"},
		{ID: "firefly-sora2-4s-16x9", CanonicalID: "firefly-sora2", Family: "Sora 2", MediaType: "video", Duration: 4, AspectRatio: "16:9"},
		{ID: "firefly-sora2-8s-16x9", CanonicalID: "firefly-sora2", Family: "Sora 2", MediaType: "video", Duration: 8, AspectRatio: "16:9"},
		{ID: "firefly-gpt-image-1k-1x1", CanonicalID: "firefly-gpt-image", Family: "GPT Image", MediaType: "image", Resolution: "1k", AspectRatio: "1:1"},
		{ID: "firefly-gpt-image-4k-16x9", CanonicalID: "firefly-gpt-image", Family: "GPT Image", MediaType: "image", Resolution: "4k", AspectRatio: "16:9"},
	}

	got, err := selectAdobeModelVariant(variants, "firefly-seedance20", "video", map[string]any{
		"duration": 10, "resolution": "1080p", "aspect_ratio": "9:16",
	})
	if err != nil || got != "firefly-seedance20-10s-9x16-1080p" {
		t.Fatalf("Seedance selection = %q, %v", got, err)
	}

	got, err = selectAdobeModelVariant(variants, "firefly-sora2", "video", map[string]any{
		"duration": 6, "aspect_ratio": "16:9",
	})
	if err != nil || got != "firefly-sora2-8s-16x9" {
		t.Fatalf("Sora nearest-duration selection = %q, %v", got, err)
	}

	got, err = selectAdobeModelVariant(variants, "firefly-gpt-image", "image", map[string]any{
		"quality": "high", "size": "1792x1024",
	})
	if err != nil || got != "firefly-gpt-image-4k-16x9" {
		t.Fatalf("GPT Image selection = %q, %v", got, err)
	}
}

func TestCanonicalAdobeModelIDMatchesSpecificFamiliesFirst(t *testing.T) {
	tests := map[string]string{
		"firefly-sora2-pro-8s-16x9":             "firefly-sora2-pro",
		"firefly-veo31-fast-6s-9x16-720p":       "firefly-veo31-fast",
		"firefly-veo31-ref-8s-16x9-1080p":       "firefly-veo31-ref",
		"firefly-seedance20-fast-10s-16x9-720p": "firefly-seedance20-fast",
	}
	for id, want := range tests {
		if got := canonicalAdobeModelID(id); got != want {
			t.Fatalf("canonicalAdobeModelID(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestRewriteAdobeURLsUsesGatewayProxy(t *testing.T) {
	payload := map[string]any{
		"data": []any{map[string]any{"url": "http://127.0.0.1:6001/generated/image.png"}},
		"html": `<video src="http://localhost:6001/generated/video.mp4"></video>`,
	}
	rewriteAdobeURLs(payload, "127.0.0.1:8001")
	data := payload["data"].([]any)[0].(map[string]any)
	if got := data["url"]; got != "http://127.0.0.1:8001/adobe/generated/image.png" {
		t.Fatalf("image URL = %v", got)
	}
	if got := payload["html"]; got != `<video src="http://127.0.0.1:8001/adobe/generated/video.mp4"></video>` {
		t.Fatalf("video HTML = %v", got)
	}
}

func TestAdobeGatewayErrorIsChineseAndActionable(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"Adobe2API HTTP 503: Upstream is temporarily unavailable", "Adobe 上游暂时不可用"},
		{"credits request failed: 451", "HTTP 451"},
		{"No active tokens available in the pool", "没有可用的 Adobe 账号"},
	}
	for _, test := range tests {
		if got := adobeGatewayError(assertError(test.raw)); !strings.Contains(got, test.want) {
			t.Fatalf("adobeGatewayError(%q) = %q, want substring %q", test.raw, got, test.want)
		}
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
