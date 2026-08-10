package service

import "testing"

func TestLookupVideoModelUsesLeonardoHailuoAPIValues(t *testing.T) {
	model := LookupVideoModel("hailuo-2.3")
	if model == nil {
		t.Fatal("hailuo-2.3 should be a callable model")
	}
	if got := model.LeonardoModelValue(); got != "hailuo-2_3" {
		t.Fatalf("hailuo model value = %q, want hailuo-2_3", got)
	}
	if got := LookupVideoModel("hailuo-2_3"); got == nil || got.Slug != model.Slug {
		t.Fatalf("underscore model value did not resolve to %q: %#v", model.Slug, got)
	}

	fast := LookupVideoModel("hailuo-2.3-fast")
	if fast == nil || fast.LeonardoModelValue() != "hailuo-2_3-fast" {
		t.Fatalf("fast hailuo mapping = %#v", fast)
	}
}

func TestLookupVideoModelDoesNotAdvertiseWebsiteOnlyHailuo03(t *testing.T) {
	for _, value := range []string{"hailuo-03", "minimaxh3", "MiniMax Hailuo 03（网页专用）"} {
		if model := LookupVideoModel(value); model != nil {
			t.Fatalf("website-only model %q resolved to %#v", value, model)
		}
	}
}

func TestVideoCatalogHasUniqueCallableSlugs(t *testing.T) {
	seen := make(map[string]bool)
	for _, model := range VideoModels {
		if model.Hidden {
			continue
		}
		if seen[model.Slug] {
			t.Fatalf("duplicate callable video slug %q", model.Slug)
		}
		seen[model.Slug] = true
	}
}

func TestVideoCatalogHasThirtyTwoCallableModels(t *testing.T) {
	count := 0
	for _, model := range VideoModels {
		if !model.Hidden {
			count++
		}
	}
	if count != 32 {
		t.Fatalf("callable video model count = %d, want 32", count)
	}
}

func TestSeedance480pAliasesResolve(t *testing.T) {
	for _, value := range []string{"video-2.0-480p", "video-2.0-fast-480p", "video-2.0-mini-480p"} {
		model := LookupVideoModel(value)
		if model == nil {
			t.Fatalf("%s did not resolve", value)
		}
		if got := model.ResolveResolution(""); got != "RESOLUTION_480" {
			t.Fatalf("%s default resolution = %s, want RESOLUTION_480", value, got)
		}
	}
}
