package service

import "strings"

const (
	AudioNone     = "none"
	AudioOptional = "optional"
	AudioRequired = "required"
)

// VideoModel describes a Leonardo video model and the model-specific request
// switches needed by the app's GraphQL Generate mutation. The catalog mirrors
// the public Leonardo documentation checked on 2026-08-07. Legacy models that
// still have official recipes are kept so existing workflows do not disappear.
type VideoModel struct {
	Name                   string
	Family                 string
	Slug                   string // public id accepted by anan视频工具箱 clients
	Aliases                []string
	ModelValue             string // value sent to Leonardo (defaults to Slug)
	RequestProfile         string // unified | legacy-image-to-video
	DefaultMode            string
	SupportedModes         []string
	Dimensions             map[string]map[string][2]int // mode -> aspect -> width,height
	DurationOptions        []int
	DefaultDuration        int
	AudioPolicy            string // none | optional | required
	SupportsRefImage       bool
	RequiresRefImage       bool
	SupportsEndFrame       bool
	SupportsImageReference bool
	SupportsVideoReference bool
	SupportsAudioReference bool
	IncludeMode            bool
	IncludeSeed            bool
	AudioParameter         string // defaults to motion_has_audio
	DirectStartFrame       bool   // legacy parameters.start_frame object
	DefaultAspect          string
	DocsURL                string
	Notes                  string
	// Hidden marks a website-only/diagnostic entry that must not be exposed as
	// a callable model. Keeping these entries out of /v1/models prevents the
	// canvas from showing "生成中" for a model that Leonardo never creates.
	Hidden bool
}

func (m VideoModel) LeonardoModelValue() string {
	if strings.TrimSpace(m.ModelValue) != "" {
		return m.ModelValue
	}
	return m.Slug
}

func (m VideoModel) SupportsAudio() bool { return m.AudioPolicy != AudioNone }

var dims720 = map[string]map[string][2]int{
	"RESOLUTION_720": {
		"16:9": {1280, 720}, "1:1": {960, 960}, "9:16": {720, 1280},
	},
}

var dims7201080 = map[string]map[string][2]int{
	"RESOLUTION_720": {
		"16:9": {1280, 720}, "1:1": {960, 960}, "9:16": {720, 1280},
	},
	"RESOLUTION_1080": {
		"16:9": {1920, 1080}, "1:1": {1440, 1440}, "9:16": {1080, 1920},
	},
}

var dims7201080Five = map[string]map[string][2]int{
	"RESOLUTION_720": {
		"16:9": {1280, 720}, "4:3": {960, 720}, "1:1": {960, 960},
		"3:4": {720, 960}, "9:16": {720, 1280},
	},
	"RESOLUTION_1080": {
		"16:9": {1920, 1080}, "4:3": {1440, 1080}, "1:1": {1440, 1440},
		"3:4": {1080, 1440}, "9:16": {1080, 1920},
	},
}

var seedance2Dims = map[string]map[string][2]int{
	"RESOLUTION_480": {
		"16:9": {864, 496}, "9:16": {496, 864}, "1:1": {640, 640},
		"4:3": {752, 560}, "3:4": {560, 752}, "21:9": {992, 432}, "9:21": {432, 992},
	},
	"RESOLUTION_720": {
		"16:9": {1280, 720}, "9:16": {720, 1280}, "1:1": {960, 960},
		"4:3": {1112, 834}, "3:4": {834, 1112}, "21:9": {1470, 630},
	},
	"RESOLUTION_1080": {
		"16:9": {1920, 1080}, "9:16": {1080, 1920}, "1:1": {1080, 1080},
		"4:3": {1440, 1080}, "3:4": {834, 1112}, "21:9": {2520, 1080}, "9:21": {1080, 2520},
	},
}

var seedance1Dims = map[string]map[string][2]int{
	"RESOLUTION_480": {
		"16:9": {864, 480}, "4:3": {736, 544}, "1:1": {640, 640},
		"3:4": {544, 736}, "9:16": {480, 864}, "21:9": {960, 416},
	},
	"RESOLUTION_720": {
		"16:9": {1248, 704}, "4:3": {1120, 832}, "1:1": {960, 960},
		"3:4": {832, 1120}, "9:16": {704, 1248}, "21:9": {1504, 640},
	},
	"RESOLUTION_1080": {
		"16:9": {1920, 1088}, "4:3": {1664, 1248}, "1:1": {1440, 1440},
		"3:4": {1248, 1664}, "9:16": {1088, 1920}, "21:9": {2176, 928},
	},
}

var motionDims = map[string]map[string][2]int{
	"RESOLUTION_480": {
		"16:9": {864, 480}, "4:3": {736, 544}, "1:1": {640, 640},
		"3:4": {544, 736}, "9:16": {480, 864},
	},
	"RESOLUTION_720": {
		"16:9": {1280, 720}, "4:3": {960, 720}, "1:1": {960, 960},
		"3:4": {720, 960}, "9:16": {720, 1280},
	},
}

var hailuo03Dims = map[string]map[string][2]int{
	"RESOLUTION_AUTO": {"16:9": {0, 0}},
	"RESOLUTION_1440": {
		"21:9": {3360, 1440}, "16:9": {2560, 1440}, "4:3": {1920, 1440},
		"1:1": {1440, 1440}, "3:4": {1440, 1920}, "9:16": {1440, 2560},
	},
}

var veo4kDims = map[string]map[string][2]int{
	"RESOLUTION_720":  {"16:9": {1280, 720}, "9:16": {720, 1280}},
	"RESOLUTION_1080": {"16:9": {1920, 1080}, "9:16": {1080, 1920}},
	"RESOLUTION_2160": {"16:9": {3840, 2160}, "9:16": {2160, 3840}},
}

var ltxLandscapeDims = map[string]map[string][2]int{
	"RESOLUTION_1080": {"16:9": {1920, 1080}},
	"RESOLUTION_1440": {"16:9": {2560, 1440}},
	"RESOLUTION_2160": {"16:9": {3840, 2160}},
}

var ltx23Dims = map[string]map[string][2]int{
	"RESOLUTION_1080": {"16:9": {1920, 1080}, "9:16": {1080, 1920}},
	"RESOLUTION_1440": {"16:9": {2560, 1440}, "9:16": {1440, 2560}},
	"RESOLUTION_2160": {"16:9": {3840, 2160}, "9:16": {2160, 3840}},
}

var veoLiteDims = map[string]map[string][2]int{
	"RESOLUTION_720":  {"16:9": {1280, 720}, "9:16": {720, 1280}},
	"RESOLUTION_1080": {"16:9": {1920, 1080}, "9:16": {1080, 1920}},
}

var grokDims = map[string]map[string][2]int{
	"RESOLUTION_AUTO": {"16:9": {0, 0}},
	"RESOLUTION_400":  {"16:9": {736, 400}, "9:16": {400, 736}},
	"RESOLUTION_720":  {"16:9": {1280, 720}, "9:16": {720, 1280}, "1:1": {544, 544}},
	"RESOLUTION_960":  {"1:1": {960, 960}},
}

var hailuo23Dims = map[string]map[string][2]int{
	"RESOLUTION_768":  {"16:9": {1366, 768}, "9:16": {768, 1366}},
	"RESOLUTION_1080": {"16:9": {1920, 1080}, "9:16": {1080, 1920}},
}

func ints(from, to int) []int {
	out := make([]int, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

func docs(slug string) string { return "https://docs.leonardo.ai/docs/" + slug }

// VideoModels is the complete current documentation catalog plus the still
// documented Hailuo 2.3 recipe variants. Order is significant: the first row
// is the default used when a caller omits model.
var VideoModels = []VideoModel{
	{Name: "Seedance 2.0", Family: "Seedance", Slug: "seedance-2.0", RequestProfile: "unified", DefaultMode: "RESOLUTION_720", SupportedModes: []string{"RESOLUTION_480", "RESOLUTION_720", "RESOLUTION_1080"}, Dimensions: seedance2Dims, DurationOptions: ints(4, 15), DefaultDuration: 8, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsEndFrame: true, SupportsImageReference: true, SupportsVideoReference: true, SupportsAudioReference: true, IncludeMode: true, IncludeSeed: true, DefaultAspect: "16:9", DocsURL: docs("seedance-20")},
	{Name: "Seedance 2.0 Fast", Family: "Seedance", Slug: "seedance-2.0-fast", RequestProfile: "unified", DefaultMode: "RESOLUTION_720", SupportedModes: []string{"RESOLUTION_480", "RESOLUTION_720"}, Dimensions: seedance2Dims, DurationOptions: ints(4, 15), DefaultDuration: 8, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsEndFrame: true, SupportsImageReference: true, SupportsVideoReference: true, SupportsAudioReference: true, IncludeMode: true, IncludeSeed: true, DefaultAspect: "16:9", DocsURL: docs("seedance-20-fast")},
	{Name: "Seedance 2.0 Mini", Family: "Seedance", Slug: "seedance-2.0-mini", RequestProfile: "unified", DefaultMode: "RESOLUTION_720", SupportedModes: []string{"RESOLUTION_720"}, Dimensions: dims720, DurationOptions: ints(4, 15), DefaultDuration: 8, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsEndFrame: true, SupportsImageReference: true, SupportsVideoReference: true, SupportsAudioReference: true, IncludeSeed: true, DefaultAspect: "16:9", DocsURL: docs("seedance-20-mini")},
	{Name: "Seedance 2.0 480p", Family: "Seedance", Slug: "seedance-2.0-480p", Aliases: []string{"video-2.0-480p"}, RequestProfile: "unified", DefaultMode: "RESOLUTION_480", SupportedModes: []string{"RESOLUTION_480"}, Dimensions: onlyMode(seedance2Dims, "RESOLUTION_480"), DurationOptions: ints(4, 15), DefaultDuration: 8, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsEndFrame: true, SupportsImageReference: true, SupportsVideoReference: true, SupportsAudioReference: true, IncludeMode: true, IncludeSeed: true, DefaultAspect: "16:9", DocsURL: docs("seedance-20")},
	{Name: "Seedance 2.0 Fast 480p", Family: "Seedance", Slug: "seedance-2.0-fast-480p", Aliases: []string{"video-2.0-fast-480p"}, RequestProfile: "unified", DefaultMode: "RESOLUTION_480", SupportedModes: []string{"RESOLUTION_480"}, Dimensions: onlyMode(seedance2Dims, "RESOLUTION_480"), DurationOptions: ints(4, 15), DefaultDuration: 8, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsEndFrame: true, SupportsImageReference: true, SupportsVideoReference: true, SupportsAudioReference: true, IncludeMode: true, IncludeSeed: true, DefaultAspect: "16:9", DocsURL: docs("seedance-20-fast")},
	{Name: "Seedance 2.0 Mini 480p", Family: "Seedance", Slug: "seedance-2.0-mini-480p", Aliases: []string{"video-2.0-mini-480p"}, RequestProfile: "unified", DefaultMode: "RESOLUTION_480", SupportedModes: []string{"RESOLUTION_480"}, Dimensions: onlyMode(seedance2Dims, "RESOLUTION_480"), DurationOptions: ints(4, 15), DefaultDuration: 8, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsEndFrame: true, SupportsImageReference: true, SupportsVideoReference: true, SupportsAudioReference: true, IncludeMode: true, IncludeSeed: true, DefaultAspect: "16:9", DocsURL: docs("seedance-20-mini")},
	{Name: "Seedance 1.0 Pro", Family: "Seedance", Slug: "seedance-1.0-pro", RequestProfile: "unified", DefaultMode: "RESOLUTION_720", SupportedModes: []string{"RESOLUTION_480", "RESOLUTION_720", "RESOLUTION_1080"}, Dimensions: seedance1Dims, DurationOptions: []int{4, 6, 8, 10}, DefaultDuration: 8, AudioPolicy: AudioNone, SupportsRefImage: true, SupportsEndFrame: true, DefaultAspect: "16:9", DocsURL: docs("seedance-1-0-pro")},
	{Name: "Seedance 1.0 Pro Fast", Family: "Seedance", Slug: "seedance-1.0-pro-fast", RequestProfile: "unified", DefaultMode: "RESOLUTION_720", SupportedModes: []string{"RESOLUTION_480", "RESOLUTION_720", "RESOLUTION_1080"}, Dimensions: seedance1Dims, DurationOptions: []int{4, 6, 8, 10}, DefaultDuration: 8, AudioPolicy: AudioNone, SupportsRefImage: true, SupportsEndFrame: true, DefaultAspect: "16:9", DocsURL: docs("seedance-1-0-pro")},

	{Name: "MiniMax Hailuo 03（网页专用）", Family: "MiniMax", Slug: "hailuo-03", Aliases: []string{"minimaxh3", "minimax-h3", "minimax-hailuo-03", "hailuo-h3"}, RequestProfile: "unified", DefaultMode: "RESOLUTION_1440", SupportedModes: []string{"RESOLUTION_AUTO", "RESOLUTION_1440"}, Dimensions: hailuo03Dims, DurationOptions: ints(5, 15), DefaultDuration: 5, AudioPolicy: AudioRequired, SupportsRefImage: true, SupportsEndFrame: true, SupportsImageReference: true, SupportsAudioReference: true, DefaultAspect: "16:9", DocsURL: docs("minimax-hailuo-03"), Notes: "Leonardo 网页模型；当前公开视频接口没有可验证的模型值，因此不加入可调用目录", Hidden: true},
	{Name: "MiniMax Hailuo 2.3", Family: "MiniMax", Slug: "hailuo-2.3", Aliases: []string{"hailuo-2_3"}, ModelValue: "hailuo-2_3", RequestProfile: "unified-legacy", DefaultMode: "RESOLUTION_768", SupportedModes: []string{"RESOLUTION_768", "RESOLUTION_1080"}, Dimensions: hailuo23Dims, DurationOptions: []int{6, 10}, DefaultDuration: 6, AudioPolicy: AudioNone, SupportsRefImage: true, DefaultAspect: "16:9", DocsURL: "https://docs.leonardo.ai/recipes/generate-with-hailuo-23-using-text-prompts", Notes: "Leonardo API 模型值使用下划线：hailuo-2_3"},
	{Name: "MiniMax Hailuo 2.3 Fast", Family: "MiniMax", Slug: "hailuo-2.3-fast", Aliases: []string{"hailuo-2_3-fast"}, ModelValue: "hailuo-2_3-fast", RequestProfile: "unified-legacy", DefaultMode: "RESOLUTION_768", SupportedModes: []string{"RESOLUTION_768", "RESOLUTION_1080"}, Dimensions: hailuo23Dims, DurationOptions: []int{6, 10}, DefaultDuration: 6, AudioPolicy: AudioNone, SupportsRefImage: true, DefaultAspect: "16:9", DocsURL: "https://docs.leonardo.ai/recipes/generate-with-hailuo-23-fast-768p-6s-using-start-frame", Notes: "Leonardo API 模型值使用下划线：hailuo-2_3-fast"},

	{Name: "Kling 3.0", Family: "Kling", Slug: "kling-3.0", RequestProfile: "unified", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_720", "RESOLUTION_1080"}, Dimensions: dims7201080, DurationOptions: ints(3, 15), DefaultDuration: 5, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsEndFrame: true, SupportsImageReference: true, IncludeMode: true, DefaultAspect: "16:9", DocsURL: docs("kling-30")},
	{Name: "Kling 3.0 Turbo", Family: "Kling", Slug: "kling-3.0-turbo", RequestProfile: "unified", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_720", "RESOLUTION_1080"}, Dimensions: dims7201080, DurationOptions: ints(3, 15), DefaultDuration: 5, AudioPolicy: AudioOptional, SupportsRefImage: true, IncludeMode: true, DefaultAspect: "16:9", DocsURL: docs("kling-30-turbo")},
	{Name: "Kling O3", Family: "Kling", Slug: "kling-video-o-3", RequestProfile: "unified", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_720", "RESOLUTION_1080"}, Dimensions: dims7201080, DurationOptions: ints(3, 15), DefaultDuration: 5, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsEndFrame: true, SupportsImageReference: true, SupportsVideoReference: true, IncludeMode: true, DefaultAspect: "16:9", DocsURL: docs("kling-o3")},
	{Name: "Kling O1", Family: "Kling", Slug: "kling-video-o-1", RequestProfile: "unified", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_1080"}, Dimensions: map[string]map[string][2]int{"RESOLUTION_1080": {"16:9": {1920, 1080}, "1:1": {1440, 1440}, "9:16": {1080, 1920}}}, DurationOptions: []int{5, 10}, DefaultDuration: 5, AudioPolicy: AudioNone, SupportsRefImage: true, SupportsEndFrame: true, SupportsImageReference: true, SupportsVideoReference: true, DefaultAspect: "16:9", DocsURL: docs("kling-o1")},
	{Name: "Kling 2.6", Family: "Kling", Slug: "kling-2.6", RequestProfile: "unified", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_1080"}, Dimensions: onlyMode(dims7201080, "RESOLUTION_1080"), DurationOptions: []int{5, 10}, DefaultDuration: 5, AudioPolicy: AudioNone, SupportsRefImage: true, DefaultAspect: "16:9", DocsURL: docs("kling-26")},
	{Name: "Kling 2.5 Turbo Standard", Family: "Kling", Slug: "kling-2.5-turbo-standard", RequestProfile: "unified", DefaultMode: "RESOLUTION_720", SupportedModes: []string{"RESOLUTION_720"}, Dimensions: dims720, DurationOptions: []int{5, 10}, DefaultDuration: 5, AudioPolicy: AudioNone, SupportsRefImage: true, RequiresRefImage: true, IncludeMode: true, DefaultAspect: "16:9", DocsURL: "https://docs.leonardo.ai/recipes/generate-with-kling-25-turbo-standard-using-start-frame", Notes: "仅支持图生视频，必须提供首帧"},
	{Name: "Kling 2.5 Turbo", Family: "Kling", Slug: "kling-2.5-turbo", ModelValue: "Kling2_5", RequestProfile: "legacy-image-to-video", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_1080"}, Dimensions: onlyMode(dims7201080, "RESOLUTION_1080"), DurationOptions: []int{5, 10}, DefaultDuration: 5, AudioPolicy: AudioNone, SupportsRefImage: true, SupportsEndFrame: true, DefaultAspect: "16:9", DocsURL: docs("kling-2-5-turbo"), Notes: "旧版官方图生视频接口"},
	{Name: "Kling 2.1 Pro", Family: "Kling", Slug: "kling-2.1-pro", ModelValue: "KLING2_1", RequestProfile: "legacy-image-to-video", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_1080"}, Dimensions: map[string]map[string][2]int{"RESOLUTION_1080": {"16:9": {1920, 1080}, "9:16": {1080, 1920}}}, DurationOptions: []int{5, 10}, DefaultDuration: 5, AudioPolicy: AudioNone, SupportsRefImage: true, RequiresRefImage: true, DefaultAspect: "16:9", DocsURL: docs("kling-2-1-pro"), Notes: "旧版官方图生视频接口，必须提供首帧"},

	{Name: "Veo 3.1", Family: "Veo", Slug: "veo-3.1-generate-001", RequestProfile: "unified", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_720", "RESOLUTION_1080", "RESOLUTION_2160"}, Dimensions: veo4kDims, DurationOptions: []int{4, 6, 8}, DefaultDuration: 8, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsEndFrame: true, SupportsImageReference: true, IncludeSeed: true, DefaultAspect: "16:9", DocsURL: docs("veo-31")},
	{Name: "Veo 3.1 Fast", Family: "Veo", Slug: "veo-3.1-fast-generate-001", RequestProfile: "unified", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_720", "RESOLUTION_1080", "RESOLUTION_2160"}, Dimensions: veo4kDims, DurationOptions: []int{4, 6, 8}, DefaultDuration: 8, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsEndFrame: true, IncludeSeed: true, DefaultAspect: "16:9", DocsURL: docs("veo-31")},
	{Name: "Veo 3.1 Lite", Family: "Veo", Slug: "veo-3.1-lite", RequestProfile: "unified", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_720", "RESOLUTION_1080"}, Dimensions: veoLiteDims, DurationOptions: []int{4, 6, 8}, DefaultDuration: 8, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsEndFrame: true, IncludeSeed: true, DefaultAspect: "16:9", DocsURL: docs("veo-31")},

	{Name: "LTX 2 Pro", Family: "LTX", Slug: "ltx-2-pro", ModelValue: "ltxv-2.0-pro", RequestProfile: "unified-legacy", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_1080", "RESOLUTION_1440", "RESOLUTION_2160"}, Dimensions: ltxLandscapeDims, DurationOptions: []int{6, 8}, DefaultDuration: 8, AudioPolicy: AudioOptional, SupportsRefImage: true, IncludeMode: true, IncludeSeed: true, AudioParameter: "audio", DirectStartFrame: true, DefaultAspect: "16:9", DocsURL: "https://docs.leonardo.ai/v1.0/docs/ltx-20", Notes: "官方模型值 ltxv-2.0-pro"},
	{Name: "LTX 2 Fast", Family: "LTX", Slug: "ltx-2-fast", ModelValue: "ltxv-2.0-fast", RequestProfile: "unified-legacy", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_1080", "RESOLUTION_1440", "RESOLUTION_2160"}, Dimensions: ltxLandscapeDims, DurationOptions: []int{6, 8}, DefaultDuration: 8, AudioPolicy: AudioOptional, SupportsRefImage: true, IncludeMode: true, IncludeSeed: true, AudioParameter: "audio", DirectStartFrame: true, DefaultAspect: "16:9", DocsURL: "https://docs.leonardo.ai/v1.0/docs/ltx-20", Notes: "官方模型值 ltxv-2.0-fast"},
	{Name: "LTX 2.3 Pro", Family: "LTX", Slug: "ltx-2.3-pro", ModelValue: "ltxv-2.3-pro", RequestProfile: "unified", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_1080", "RESOLUTION_1440", "RESOLUTION_2160"}, Dimensions: ltx23Dims, DurationOptions: []int{6, 8, 10}, DefaultDuration: 8, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsEndFrame: true, IncludeMode: true, IncludeSeed: true, DefaultAspect: "16:9", DocsURL: "https://docs.leonardo.ai/v1.0/docs/ltx-23", Notes: "官方模型值 ltxv-2.3-pro"},
	{Name: "LTX 2.3 Fast", Family: "LTX", Slug: "ltx-2.3-fast", ModelValue: "ltxv-2.3-fast", RequestProfile: "unified", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_1080", "RESOLUTION_1440", "RESOLUTION_2160"}, Dimensions: ltx23Dims, DurationOptions: []int{6, 8, 10, 12, 14, 16, 18, 20}, DefaultDuration: 8, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsEndFrame: true, IncludeMode: true, IncludeSeed: true, DefaultAspect: "16:9", DocsURL: "https://docs.leonardo.ai/v1.0/docs/ltx-23", Notes: "官方模型值 ltxv-2.3-fast"},

	{Name: "Wan 2.7", Family: "Wan", Slug: "wan-2.7", RequestProfile: "unified", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_720", "RESOLUTION_1080"}, Dimensions: dims7201080, DurationOptions: ints(2, 10), DefaultDuration: 5, AudioPolicy: AudioNone, SupportsRefImage: true, SupportsEndFrame: true, SupportsImageReference: true, SupportsVideoReference: true, DefaultAspect: "16:9", DocsURL: docs("wan-27")},
	{Name: "Wan 2.6", Family: "Wan", Slug: "wan-2.6", RequestProfile: "unified", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_720", "RESOLUTION_1080"}, Dimensions: dims7201080Five, DurationOptions: []int{5, 10, 15}, DefaultDuration: 5, AudioPolicy: AudioNone, SupportsRefImage: true, SupportsVideoReference: true, SupportsAudioReference: true, IncludeSeed: true, DefaultAspect: "16:9", DocsURL: docs("wan-26")},

	{Name: "Grok Imagine 1.5", Family: "Grok", Slug: "grok-imagine-1.5", RequestProfile: "unified", DefaultMode: "RESOLUTION_720", SupportedModes: []string{"RESOLUTION_AUTO", "RESOLUTION_400", "RESOLUTION_720", "RESOLUTION_960"}, Dimensions: grokDims, DurationOptions: ints(3, 15), DefaultDuration: 6, AudioPolicy: AudioOptional, SupportsRefImage: true, RequiresRefImage: true, DefaultAspect: "16:9", DocsURL: docs("grok-imagine-15")},
	{Name: "Gemini Omni Flash", Family: "Gemini", Slug: "gemini-omni-flash", RequestProfile: "unified", DefaultMode: "RESOLUTION_720", SupportedModes: []string{"RESOLUTION_720"}, Dimensions: map[string]map[string][2]int{"RESOLUTION_720": {"16:9": {1280, 720}, "9:16": {720, 1280}}}, DurationOptions: ints(3, 10), DefaultDuration: 5, AudioPolicy: AudioNone, SupportsImageReference: true, DefaultAspect: "16:9", DocsURL: docs("gemini-omni-flash")},
	{Name: "Happy Horse 1.1", Family: "Happy Horse", Slug: "happy-horse-1.1", RequestProfile: "unified", DefaultMode: "RESOLUTION_1080", SupportedModes: []string{"RESOLUTION_720", "RESOLUTION_1080"}, Dimensions: dims7201080Five, DurationOptions: ints(3, 15), DefaultDuration: 5, AudioPolicy: AudioOptional, SupportsRefImage: true, SupportsImageReference: true, IncludeSeed: true, DefaultAspect: "16:9", DocsURL: docs("happy-horse-11")},

	{Name: "Motion 2.0", Family: "Motion", Slug: "motion_2.0", RequestProfile: "unified", DefaultMode: "RESOLUTION_480", SupportedModes: []string{"RESOLUTION_480", "RESOLUTION_720"}, Dimensions: motionDims, DurationOptions: []int{5}, DefaultDuration: 5, AudioPolicy: AudioNone, SupportsRefImage: true, IncludeMode: true, DefaultAspect: "16:9", DocsURL: docs("motion-20")},
	{Name: "Motion 2.0 Fast", Family: "Motion", Slug: "motion_2.0-fast", RequestProfile: "unified", DefaultMode: "RESOLUTION_480", SupportedModes: []string{"RESOLUTION_480", "RESOLUTION_720"}, Dimensions: motionDims, DurationOptions: []int{5}, DefaultDuration: 5, AudioPolicy: AudioNone, SupportsRefImage: true, IncludeMode: true, DefaultAspect: "16:9", DocsURL: docs("motion-20-fast")},
}

// onlyMode creates a one-mode view while retaining the immutable shared table.
func onlyMode(source map[string]map[string][2]int, mode string) map[string]map[string][2]int {
	return map[string]map[string][2]int{mode: source[mode]}
}

// LookupVideoModel accepts public slugs, Leonardo model values and display
// names so canvas clients can submit exactly what they received from /v1/models.
func LookupVideoModel(value string) *VideoModel {
	q := strings.ToLower(strings.TrimSpace(value))
	for i := range VideoModels {
		m := &VideoModels[i]
		if m.Hidden {
			continue
		}
		if strings.ToLower(m.Slug) == q || strings.ToLower(m.LeonardoModelValue()) == q || strings.ToLower(m.Name) == q {
			return m
		}
		for _, alias := range m.Aliases {
			if strings.ToLower(strings.TrimSpace(alias)) == q {
				return m
			}
		}
	}
	return nil
}

func DefaultVideoModel() *VideoModel {
	if len(VideoModels) == 0 {
		return nil
	}
	return &VideoModels[0]
}

func (m *VideoModel) ResolveResolution(input string) string {
	if m == nil {
		return ""
	}
	aliases := map[string]string{
		"480p": "RESOLUTION_480", "480": "RESOLUTION_480", "resolution_480": "RESOLUTION_480", "sd": "RESOLUTION_480",
		"720p": "RESOLUTION_720", "720": "RESOLUTION_720", "resolution_720": "RESOLUTION_720", "hd": "RESOLUTION_720",
		"768p": "RESOLUTION_768", "768": "RESOLUTION_768", "resolution_768": "RESOLUTION_768",
		"960p": "RESOLUTION_960", "960": "RESOLUTION_960", "resolution_960": "RESOLUTION_960",
		"1080p": "RESOLUTION_1080", "1080": "RESOLUTION_1080", "resolution_1080": "RESOLUTION_1080", "fhd": "RESOLUTION_1080",
		"1440p": "RESOLUTION_1440", "1440": "RESOLUTION_1440", "resolution_1440": "RESOLUTION_1440", "2k": "RESOLUTION_1440",
		"2160p": "RESOLUTION_2160", "2160": "RESOLUTION_2160", "resolution_2160": "RESOLUTION_2160", "4k": "RESOLUTION_2160",
		"400p": "RESOLUTION_400", "400": "RESOLUTION_400", "resolution_400": "RESOLUTION_400",
		"auto": "RESOLUTION_AUTO", "resolution_auto": "RESOLUTION_AUTO",
	}
	q := strings.ToLower(strings.TrimSpace(input))
	if q == "" {
		return m.DefaultMode
	}
	if mode, ok := aliases[q]; ok {
		return choose(m, mode)
	}
	return choose(m, strings.ToUpper(strings.TrimSpace(input)))
}

func choose(m *VideoModel, candidate string) string {
	for _, supported := range m.SupportedModes {
		if supported == candidate {
			return supported
		}
	}
	return m.DefaultMode
}

func (m *VideoModel) ResolveDimensions(mode, aspect string) (int, int) {
	if m == nil {
		return 0, 0
	}
	table, ok := m.Dimensions[mode]
	if !ok {
		return 0, 0
	}
	if aspect == "" {
		aspect = m.DefaultAspect
	}
	if dims, ok := table[aspect]; ok {
		return dims[0], dims[1]
	}
	if dims, ok := table[m.DefaultAspect]; ok {
		return dims[0], dims[1]
	}
	return 0, 0
}

func (m *VideoModel) ClampDuration(req int) int {
	if m == nil || len(m.DurationOptions) == 0 {
		return 0
	}
	if req <= 0 {
		return m.DefaultDuration
	}
	best := m.DurationOptions[0]
	for _, allowed := range m.DurationOptions {
		if allowed == req {
			return req
		}
		if allowed <= req && allowed > best {
			best = allowed
		}
	}
	return best
}
