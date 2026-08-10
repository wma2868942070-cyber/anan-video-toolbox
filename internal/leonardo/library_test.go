package leonardo

import "testing"

func TestParseLibraryGenerationsPrefersVideoMP4(t *testing.T) {
	resp := map[string]any{
		"data": map[string]any{
			"generations": []any{
				map[string]any{
					"id":          "generation-1",
					"modelId":     "minimax-hailuo-03",
					"prompt":      "test prompt",
					"status":      "COMPLETE",
					"createdAt":   "2026-08-08T12:30:00Z",
					"imageWidth":  float64(1280),
					"imageHeight": float64(720),
					"generated_images": []any{
						map[string]any{
							"url":          "https://cdn.example/thumb.jpg",
							"motionMP4URL": "https://cdn.example/video.mp4",
						},
					},
				},
			},
		},
	}
	got := parseLibraryGenerations(resp)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if len(got[0].AssetURLs) != 1 || got[0].AssetURLs[0] != "https://cdn.example/video.mp4" {
		t.Fatalf("assets = %#v", got[0].AssetURLs)
	}
	if got[0].CreatedAt != 1786192200 {
		t.Fatalf("created = %d", got[0].CreatedAt)
	}
}
