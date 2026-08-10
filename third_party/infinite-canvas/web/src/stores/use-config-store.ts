import { useMemo } from "react";
import { create } from "zustand";
import { persist } from "zustand/middleware";
import { nanoid } from "nanoid";

import i18n from "@/i18n";

export type ApiCallFormat = "openai" | "gemini" | "ark";
export type ModelCapability = "image" | "video" | "text" | "audio";
export type ReasoningEffort = "auto" | "low" | "medium" | "high" | "xhigh";

export type ChannelModel = {
    name: string;
    displayName?: string;
    capability: ModelCapability;
    script?: string;
};

export type ModelChannel = {
    id: string;
    name: string;
    baseUrl: string;
    apiKey: string;
    apiFormat: ApiCallFormat;
    models: ChannelModel[];
};

export type AiConfig = {
    channelMode: "remote" | "local";
    baseUrl: string;
    apiKey: string;
    apiFormat: ApiCallFormat;
    channels: ModelChannel[];
    model: string;
    imageModel: string;
    videoModel: string;
    textModel: string;
    audioModel: string;
    audioVoice: string;
    audioFormat: string;
    audioSpeed: string;
    audioInstructions: string;
    videoSeconds: string;
    vquality: string;
    videoGenerateAudio: string;
    videoWatermark: string;
    systemPrompt: string;
    reasoningEffort: ReasoningEffort;
    models: string[];
    quality: string;
    size: string;
    background: string;
    count: string;
    canvasImageCount: string;
};

export type WebdavSyncConfig = {
    url: string;
    username: string;
    password: string;
    directory: string;
    lastSyncedAt: string;
};
export type ConfigTabKey = "channels" | "preferences" | "prompt-sources" | "webdav" | "local-storage";

export const CONFIG_STORE_KEY = "infinite-canvas:ai_config_store";
const CHANNEL_MODEL_SEPARATOR = "::";
const OPENAI_BASE_URL = "https://api.openai.com";
const GEMINI_BASE_URL = "https://generativelanguage.googleapis.com";
const ARK_BASE_URL = "https://ark.cn-beijing.volces.com/api/v3";
const TOOLBOX_EMBEDDED = globalThis.location?.pathname.startsWith("/infinite-canvas") ?? false;
const TOOLBOX_BASE_URL = TOOLBOX_EMBEDDED ? globalThis.location.origin : OPENAI_BASE_URL;
const TOOLBOX_API_KEY = TOOLBOX_EMBEDDED ? "local-anan-video-toolbox" : "";
const ADOBE_CHANNEL_ID = "adobe";
const ADOBE_BASE_URL = TOOLBOX_EMBEDDED ? `${TOOLBOX_BASE_URL}/adobe` : OPENAI_BASE_URL;
const ADOBE_CANONICAL_MODELS = [
    "firefly-nano-banana-pro",
    "firefly-nano-banana2",
    "firefly-nano-banana",
    "firefly-gpt-image",
    "firefly-sora2-pro",
    "firefly-sora2",
    "firefly-gemini-omni",
    "firefly-veo31-ref",
    "firefly-veo31-fast",
    "firefly-veo31",
    "firefly-kling-o3",
    "firefly-kling3",
    "firefly-seedance20-fast",
    "firefly-seedance20",
] as const;
const TOOLBOX_VIDEO_NAMES: Record<string, string> = {
    "seedance-2.0": "Seedance 2.0",
    "seedance-2.0-fast": "Seedance 2.0 Fast",
    "seedance-2.0-mini": "Seedance 2.0 Mini",
    "seedance-1.0-pro": "Seedance 1.0 Pro",
    "seedance-1.0-pro-fast": "Seedance 1.0 Pro Fast",
    "hailuo-2.3": "MiniMax Hailuo 2.3",
    "hailuo-2.3-fast": "MiniMax Hailuo 2.3 Fast",
    "kling-3.0": "Kling 3.0",
    "kling-3.0-turbo": "Kling 3.0 Turbo",
    "kling-video-o-3": "Kling O3",
    "kling-video-o-1": "Kling O1",
    "kling-2.6": "Kling 2.6",
    "kling-2.5-turbo-standard": "Kling 2.5 Turbo Standard",
    "kling-2.5-turbo": "Kling 2.5 Turbo",
    "kling-2.1-pro": "Kling 2.1 Pro",
    "veo-3.1-generate-001": "Veo 3.1",
    "veo-3.1-fast-generate-001": "Veo 3.1 Fast",
    "veo-3.1-lite": "Veo 3.1 Lite",
    "ltx-2-pro": "LTX 2 Pro",
    "ltx-2-fast": "LTX 2 Fast",
    "ltx-2.3-pro": "LTX 2.3 Pro",
    "ltx-2.3-fast": "LTX 2.3 Fast",
    "wan-2.7": "Wan 2.7",
    "wan-2.6": "Wan 2.6",
    "grok-imagine-1.5": "Grok Imagine 1.5",
    "gemini-omni-flash": "Gemini Omni Flash",
    "happy-horse-1.1": "Happy Horse 1.1",
    "motion_2.0": "Motion 2.0",
    "motion_2.0-fast": "Motion 2.0 Fast",
};
const TOOLBOX_MODELS: ChannelModel[] = [
    { name: "anan-default", displayName: "Leonardo 默认图片模型", capability: "image" },
    ...[
        "seedance-2.0",
        "seedance-2.0-fast",
        "seedance-2.0-mini",
        "seedance-1.0-pro",
        "seedance-1.0-pro-fast",
        "hailuo-2.3",
        "hailuo-2.3-fast",
        "kling-3.0",
        "kling-3.0-turbo",
        "kling-video-o-3",
        "kling-video-o-1",
        "kling-2.6",
        "kling-2.5-turbo-standard",
        "kling-2.5-turbo",
        "kling-2.1-pro",
        "veo-3.1-generate-001",
        "veo-3.1-fast-generate-001",
        "veo-3.1-lite",
        "ltx-2-pro",
        "ltx-2-fast",
        "ltx-2.3-pro",
        "ltx-2.3-fast",
        "wan-2.7",
        "wan-2.6",
        "grok-imagine-1.5",
        "gemini-omni-flash",
        "happy-horse-1.1",
        "motion_2.0",
        "motion_2.0-fast",
    ].map((name) => ({ name, displayName: TOOLBOX_VIDEO_NAMES[name] || name, capability: "video" as const })),
];
const DEFAULT_CHANNEL_MODELS: ChannelModel[] = TOOLBOX_EMBEDDED
    ? TOOLBOX_MODELS
    : [
          { name: "gpt-image-2", capability: "image" },
          { name: "grok-imagine-video", capability: "video" },
          { name: "gpt-5.5", capability: "text" },
          { name: "gpt-4o-mini-tts", capability: "audio" },
      ];
const DEFAULT_IMAGE_MODEL = TOOLBOX_EMBEDDED ? "anan-default" : "gpt-image-2";
const DEFAULT_VIDEO_MODEL = TOOLBOX_EMBEDDED ? "seedance-2.0" : "grok-imagine-video";
const DEFAULT_TEXT_MODEL = TOOLBOX_EMBEDDED ? "" : "gpt-5.5";
const DEFAULT_AUDIO_MODEL = TOOLBOX_EMBEDDED ? "" : "gpt-4o-mini-tts";

export const defaultConfig: AiConfig = {
    channelMode: "local",
    baseUrl: TOOLBOX_BASE_URL,
    apiKey: TOOLBOX_API_KEY,
    apiFormat: "openai",
    channels: [
        {
            id: "default",
            name: TOOLBOX_EMBEDDED ? "anan视频工具箱本地反代" : i18n.t("config.channels.defaultName"),
            baseUrl: TOOLBOX_BASE_URL,
            apiKey: TOOLBOX_API_KEY,
            apiFormat: "openai",
            models: DEFAULT_CHANNEL_MODELS,
        },
        ...(TOOLBOX_EMBEDDED
            ? [
                  {
                      id: ADOBE_CHANNEL_ID,
                      name: "Adobe Firefly 本地反代",
                      baseUrl: ADOBE_BASE_URL,
                      apiKey: TOOLBOX_API_KEY,
                      apiFormat: "openai" as const,
                      models: [] as ChannelModel[],
                  },
              ]
            : []),
    ],
    model: `default::${DEFAULT_IMAGE_MODEL}`,
    imageModel: `default::${DEFAULT_IMAGE_MODEL}`,
    videoModel: `default::${DEFAULT_VIDEO_MODEL}`,
    textModel: DEFAULT_TEXT_MODEL ? `default::${DEFAULT_TEXT_MODEL}` : "",
    audioModel: DEFAULT_AUDIO_MODEL ? `default::${DEFAULT_AUDIO_MODEL}` : "",
    audioVoice: "alloy",
    audioFormat: "mp3",
    audioSpeed: "1",
    audioInstructions: "",
    videoSeconds: "6",
    vquality: "720",
    videoGenerateAudio: "true",
    videoWatermark: "false",
    systemPrompt: "",
    reasoningEffort: "auto",
    models: DEFAULT_CHANNEL_MODELS.map((model) => `default::${model.name}`),
    quality: "auto",
    size: "1:1",
    background: "",
    count: "1",
    canvasImageCount: "3",
};

export const defaultWebdavSyncConfig: WebdavSyncConfig = {
    url: "",
    username: "",
    password: "",
    directory: "infinite-canvas",
    lastSyncedAt: "",
};

type ConfigStore = {
    config: AiConfig;
    webdav: WebdavSyncConfig;
    isConfigOpen: boolean;
    configTab: ConfigTabKey;
    shouldPromptContinue: boolean;
    updateConfig: <K extends keyof AiConfig>(key: K, value: AiConfig[K]) => void;
    updateWebdavConfig: <K extends keyof WebdavSyncConfig>(key: K, value: WebdavSyncConfig[K]) => void;
    isAiConfigReady: (config: AiConfig, model: string) => boolean;
    openConfigDialog: (shouldPromptContinue?: boolean, tab?: ConfigTabKey) => void;
    setConfigDialogOpen: (isOpen: boolean) => void;
    clearPromptContinue: () => void;
};

const VIDEO_KEYWORDS = ["seedance", "video", "sora", "veo", "kling", "wan", "hailuo", "minimax", "ltx", "motion", "grok", "happy-horse", "omni"];
const AUDIO_KEYWORDS = ["audio", "tts", "speech", "voice", "music", "sound"];
const IMAGE_KEYWORDS = ["seedream", "gpt-image", "image", "dall-e", "dalle", "imagen", "flux", "sdxl", "stable-diffusion", "midjourney"];

/** Best-effort default capability for a freshly fetched model name; user can override in the channel editor. */
export function guessCapability(name: string): ModelCapability {
    const value = name.toLowerCase();
    if (VIDEO_KEYWORDS.some((keyword) => value.includes(keyword))) return "video";
    if (AUDIO_KEYWORDS.some((keyword) => value.includes(keyword))) return "audio";
    if (IMAGE_KEYWORDS.some((keyword) => value.includes(keyword))) return "image";
    return "text";
}

function findChannelModel(config: AiConfig, value: string): { channel: ModelChannel; model: ChannelModel } | null {
    const decoded = decodeChannelModel(value);
    const name = decoded?.model || value;
    const channel = decoded ? config.channels.find((item) => item.id === decoded.channelId) : config.channels.find((item) => item.models.some((model) => model.name === name));
    const model = channel?.models.find((item) => item.name === name);
    return channel && model ? { channel, model } : null;
}

export function modelCapabilityOf(config: AiConfig, value: string): ModelCapability | undefined {
    return findChannelModel(config, value)?.model.capability;
}

export function modelMatchesCapability(config: AiConfig, value: string, capability?: ModelCapability) {
    if (!capability) return true;
    return modelCapabilityOf(config, value) === capability;
}

export function resolveModelForCapability(config: AiConfig, currentModel: string | undefined, capability: ModelCapability) {
    const defaultModel = capability === "image" ? config.imageModel : capability === "video" ? config.videoModel : capability === "audio" ? config.audioModel : config.textModel;
    const fallbackModel = capability === "image" ? defaultConfig.imageModel : capability === "video" ? defaultConfig.videoModel : capability === "audio" ? defaultConfig.audioModel : defaultConfig.textModel;
    if (currentModel && modelMatchesCapability(config, currentModel, capability)) return currentModel;
    if (defaultModel && modelMatchesCapability(config, defaultModel, capability)) return defaultModel;
    return fallbackModel;
}

export function selectableModelsByCapability(config: AiConfig, capability?: ModelCapability) {
    if (!capability) return config.models;
    return config.channels.flatMap((channel) => channel.models.filter((model) => model.capability === capability).map((model) => encodeChannelModel(channel.id, model.name)));
}

/** The user script (if any) attached to a model; empty string means use the system default call. */
export function resolveModelScript(config: AiConfig, value: string) {
    return findChannelModel(config, value)?.model.script?.trim() || "";
}

function isAiConfigReady(config: AiConfig, model: string) {
    const channel = resolveModelChannel(config, model);
    return Boolean(model.trim() && channel.baseUrl.trim() && channel.apiKey.trim());
}

export const useConfigStore = create<ConfigStore>()(
    persist(
        (set, get) => ({
            config: defaultConfig,
            webdav: defaultWebdavSyncConfig,
            isConfigOpen: false,
            configTab: "channels",
            shouldPromptContinue: false,
            updateConfig: (key, value) =>
                set((state) => ({
                    config: {
                        ...state.config,
                        [key]: value,
                    },
                })),
            updateWebdavConfig: (key, value) =>
                set((state) => ({
                    webdav: {
                        ...state.webdav,
                        [key]: value,
                    },
                })),
            isAiConfigReady: (config, model) => isAiConfigReady(config, model),
            openConfigDialog: (shouldPromptContinue = false, configTab = "channels") => set({ isConfigOpen: true, shouldPromptContinue, configTab }),
            setConfigDialogOpen: (isConfigOpen) => set({ isConfigOpen }),
            clearPromptContinue: () => set({ shouldPromptContinue: false }),
        }),
        {
            name: CONFIG_STORE_KEY,
            partialize: (state) => ({ config: state.config, webdav: state.webdav }),
            merge: (persisted, current) => {
                const persistedState = (persisted || {}) as Partial<ConfigStore>;
                const persistedConfig = (persistedState.config || {}) as Partial<AiConfig>;
                const persistedWebdav = (persistedState.webdav || {}) as Partial<WebdavSyncConfig>;
                const untouchedOpenAIConfig = TOOLBOX_EMBEDDED && (!persistedConfig.apiKey || persistedConfig.apiKey === "") && (!persistedConfig.baseUrl || persistedConfig.baseUrl === OPENAI_BASE_URL);
                const config = untouchedOpenAIConfig ? { ...defaultConfig } : { ...defaultConfig, ...persistedConfig };
                if (!Array.isArray(persistedConfig.channels)) config.channels = [];
                let channels = normalizeChannels(config);
                if (TOOLBOX_EMBEDDED) {
                    const localIndex = channels.findIndex((channel) => channel.apiKey === TOOLBOX_API_KEY || channel.baseUrl === TOOLBOX_BASE_URL);
                    const localChannel = createModelChannel({
                        ...(localIndex >= 0 ? channels[localIndex] : {}),
                        id: "default",
                        name: "anan视频工具箱本地反代",
                        baseUrl: TOOLBOX_BASE_URL,
                        apiKey: TOOLBOX_API_KEY,
                        apiFormat: "openai",
                        models: TOOLBOX_MODELS,
                    });
                    channels = localIndex >= 0 ? channels.map((channel, index) => (index === localIndex ? localChannel : channel)) : [localChannel, ...channels];
                    const adobeIndex = channels.findIndex((channel) => channel.id === ADOBE_CHANNEL_ID || channel.baseUrl === ADOBE_BASE_URL);
                    const adobeChannel = createModelChannel({
                        ...(adobeIndex >= 0 ? channels[adobeIndex] : {}),
                        id: ADOBE_CHANNEL_ID,
                        name: "Adobe Firefly 本地反代",
                        baseUrl: ADOBE_BASE_URL,
                        apiKey: TOOLBOX_API_KEY,
                        apiFormat: "openai",
                        models: adobeIndex >= 0 ? channels[adobeIndex].models : [],
                    });
                    channels = adobeIndex >= 0 ? channels.map((channel, index) => (index === adobeIndex ? adobeChannel : channel)) : [...channels, adobeChannel];
                }
                const models = modelOptionsFromChannels(channels);
                return {
                    ...current,
                    webdav: { ...defaultWebdavSyncConfig, ...persistedWebdav },
                    config: {
                        ...config,
                        channelMode: "local",
                        apiFormat: normalizeApiFormat(config.apiFormat),
                        channels,
                        models,
                        imageModel: normalizeModelOptionValue(config.imageModel || config.model, channels),
                        videoModel: normalizeModelOptionValue(config.videoModel, channels),
                        textModel: normalizeModelOptionValue(config.textModel || config.model, channels),
                        audioModel: normalizeModelOptionValue(config.audioModel || defaultConfig.audioModel, channels),
                        audioVoice: config.audioVoice || defaultConfig.audioVoice,
                        audioFormat: config.audioFormat || defaultConfig.audioFormat,
                        audioSpeed: config.audioSpeed || defaultConfig.audioSpeed,
                        audioInstructions: config.audioInstructions || "",
                        reasoningEffort: config.reasoningEffort || "auto",
                        videoSeconds: config.videoSeconds || "6",
                        vquality: config.vquality || "720",
                        videoGenerateAudio: config.videoGenerateAudio || "true",
                        videoWatermark: config.videoWatermark || "false",
                        canvasImageCount: config.canvasImageCount || "3",
                    },
                };
            },
        },
    ),
);

type ToolboxModelRow = {
    id?: string;
    name?: string;
    type?: "image" | "video" | string;
    canonical_model?: string;
    display_name?: string;
    description?: string;
};

let embeddedModelSyncPromise: Promise<void> | null = null;

/**
 * The bundled canvas uses model ids/slugs as request values, but displays the
 * names returned by anan视频工具箱的 Leonardo 模型目录. This keeps image and
 * video names aligned without breaking saved projects or API compatibility.
 */
export function syncEmbeddedToolboxModels() {
    if (!TOOLBOX_EMBEDDED) return Promise.resolve();
    if (embeddedModelSyncPromise) return embeddedModelSyncPromise;
    embeddedModelSyncPromise = (async () => {
        const fetchModels = async (url: string) => {
            const response = await fetch(url, { headers: { Authorization: `Bearer ${TOOLBOX_API_KEY}` } });
            if (!response.ok) throw new Error(`model sync failed: ${response.status}`);
            return (await response.json()) as { data?: ToolboxModelRow[] };
        };
        const [toolboxResult, adobeResult] = await Promise.allSettled([
            fetchModels(`${TOOLBOX_BASE_URL}/v1/models`),
            fetchModels(`${ADOBE_BASE_URL}/v1/models`),
        ]);
        const syncedModels = normalizeChannelModels(
            (toolboxResult.status === "fulfilled" ? toolboxResult.value.data || [] : [])
                // Alias rows remain accepted by the backend, but are hidden
                // here so the picker contains one Leonardo name per model.
                .filter((row) => !row.canonical_model)
                .filter((row) => row.id && (row.type === "image" || row.type === "video"))
                .map((row) => ({
                    name: row.id!,
                    displayName: row.name?.trim() || row.id!,
                    capability: row.type as ModelCapability,
                })),
        );
        const adobeModels = normalizeChannelModels(
            (adobeResult.status === "fulfilled" ? adobeResult.value.data || [] : [])
                .filter((row) => !row.canonical_model)
                .filter((row) => row.id && (row.type === "image" || row.type === "video"))
                .map((row) => ({
                    name: row.id!,
                    displayName: row.display_name?.trim() || row.name?.trim() || row.description?.trim() || row.id!,
                    capability: row.type as ModelCapability,
                })),
        );
        if (!syncedModels.length && !adobeModels.length) return;
        useConfigStore.setState((state) => {
            const current = state.config;
            const localIndex = current.channels.findIndex((channel) => channel.id === "default" || channel.apiKey === TOOLBOX_API_KEY || channel.baseUrl === TOOLBOX_BASE_URL);
            const localChannel = createModelChannel({
                ...(localIndex >= 0 ? current.channels[localIndex] : {}),
                id: "default",
                name: "anan视频工具箱本地反代",
                baseUrl: TOOLBOX_BASE_URL,
                apiKey: TOOLBOX_API_KEY,
                apiFormat: "openai",
                models: syncedModels,
            });
            let channels = syncedModels.length
                ? localIndex >= 0
                    ? current.channels.map((channel, index) => (index === localIndex ? localChannel : channel))
                    : [localChannel, ...current.channels]
                : current.channels;
            const adobeIndex = channels.findIndex((channel) => channel.id === ADOBE_CHANNEL_ID || channel.baseUrl === ADOBE_BASE_URL);
            if (adobeModels.length) {
                const adobeChannel = createModelChannel({
                    ...(adobeIndex >= 0 ? channels[adobeIndex] : {}),
                    id: ADOBE_CHANNEL_ID,
                    name: "Adobe Firefly 本地反代",
                    baseUrl: ADOBE_BASE_URL,
                    apiKey: TOOLBOX_API_KEY,
                    apiFormat: "openai",
                    models: adobeModels,
                });
                channels = adobeIndex >= 0 ? channels.map((channel, index) => (index === adobeIndex ? adobeChannel : channel)) : [...channels, adobeChannel];
            }
            const normalizeSyncedSelection = (value: string | undefined) =>
                normalizeModelOptionValue(migrateAdobeModelValue(value, channels), channels);
            return {
                config: {
                    ...current,
                    channels,
                    models: modelOptionsFromChannels(channels),
                    model: normalizeSyncedSelection(current.model),
                    imageModel: normalizeSyncedSelection(current.imageModel) || `default::${DEFAULT_IMAGE_MODEL}`,
                    videoModel: normalizeSyncedSelection(current.videoModel) || `default::${DEFAULT_VIDEO_MODEL}`,
                    textModel: normalizeSyncedSelection(current.textModel),
                    audioModel: normalizeSyncedSelection(current.audioModel),
                },
            };
        });
    })().catch(() => {
        // Keep the bundled fallback catalog usable when the local server is
        // briefly unavailable during startup. A page reload retries the sync.
    });
    return embeddedModelSyncPromise;
}

if (TOOLBOX_EMBEDDED && typeof window !== "undefined") {
    queueMicrotask(() => void syncEmbeddedToolboxModels());
}

export function useEffectiveConfig() {
    const config = useConfigStore((state) => state.config);
    return useMemo(() => ({ ...config, channelMode: "local" as const }), [config]);
}

/** Normalize a mixed list of raw model names or model objects into deduped ChannelModel entries. */
export function normalizeChannelModels(models: Array<string | ChannelModel> | undefined): ChannelModel[] {
    const seen = new Set<string>();
    const result: ChannelModel[] = [];
    for (const item of models || []) {
        const name = (typeof item === "string" ? item : item?.name || "").trim();
        if (!name || seen.has(name)) continue;
        seen.add(name);
        const capability = typeof item === "string" ? guessCapability(name) : item.capability || guessCapability(name);
        const displayName = typeof item === "string" ? undefined : item.displayName?.trim() || undefined;
        const script = typeof item === "string" ? undefined : item.script?.trim() || undefined;
        result.push({ name, displayName, capability, script });
    }
    return result;
}

export function createModelChannel(channel?: Partial<ModelChannel>): ModelChannel {
    const apiFormat = normalizeApiFormat(channel?.apiFormat);
    return {
        id: channel?.id?.trim() || nanoid(),
        name: channel?.name?.trim() || i18n.t("config.channels.newName"),
        baseUrl: channel?.baseUrl?.trim() || defaultBaseUrlForApiFormat(apiFormat),
        apiKey: channel?.apiKey || "",
        apiFormat,
        models: normalizeChannelModels(channel?.models),
    };
}

export function encodeChannelModel(channelId: string, model: string) {
    return `${channelId}${CHANNEL_MODEL_SEPARATOR}${model.trim()}`;
}

export function isChannelModelValue(value: string) {
    return value.includes(CHANNEL_MODEL_SEPARATOR);
}

export function decodeChannelModel(value: string) {
    const index = value.indexOf(CHANNEL_MODEL_SEPARATOR);
    if (index < 0) return null;
    return { channelId: value.slice(0, index), model: value.slice(index + CHANNEL_MODEL_SEPARATOR.length) };
}

export function modelOptionName(value: string) {
    return decodeChannelModel(value)?.model || value;
}

export function modelOptionLabel(config: AiConfig, value: string) {
    const decoded = decodeChannelModel(value);
    const matched = findChannelModel(config, value);
    const modelName = decoded?.model || value;
    const displayName = matched?.model.displayName || modelName;
    if (!decoded || !matched) return displayName;
    if (TOOLBOX_EMBEDDED && (matched.channel.id === "default" || matched.channel.id === ADOBE_CHANNEL_ID)) {
        const normalizedLabel = displayName.trim().toLocaleLowerCase();
        const hasCrossChannelConflict = config.channels.some(
            (channel) => channel.id !== matched.channel.id && channel.models.some((model) => (model.displayName || model.name).trim().toLocaleLowerCase() === normalizedLabel),
        );
        if (!hasCrossChannelConflict) return displayName;
        const provider = matched.channel.id === ADOBE_CHANNEL_ID ? "Adobe" : "Leonardo";
        return `${displayName} · ${provider}`;
    }
    return `${displayName}（${matched.channel.name}）`;
}

function canonicalAdobeModelName(model: string) {
    const normalized = model.trim().toLowerCase();
    return ADOBE_CANONICAL_MODELS.find((canonical) => normalized === canonical || normalized.startsWith(`${canonical}-`)) || model.trim();
}

function migrateAdobeModelValue(value: string | undefined, channels: ModelChannel[]) {
    const raw = (value || "").trim();
    if (!raw) return "";
    const decoded = decodeChannelModel(raw);
    const model = decoded?.model || raw;
    const canonical = canonicalAdobeModelName(model);
    if (canonical === model) return raw;
    const adobeChannel = channels.find((channel) => channel.id === ADOBE_CHANNEL_ID || channel.baseUrl === ADOBE_BASE_URL);
    if (!adobeChannel || !adobeChannel.models.some((entry) => entry.name === canonical)) return raw;
    return encodeChannelModel(adobeChannel.id, canonical);
}

export function modelOptionsFromChannels(channels: ModelChannel[]) {
    return uniqueModelOptions(channels.flatMap((channel) => channel.models.map((model) => encodeChannelModel(channel.id, model.name))));
}

export function normalizeModelOptionValue(value: string | undefined, channels: ModelChannel[]) {
    const model = (value || "").trim();
    if (!model) return "";
    const decoded = decodeChannelModel(model);
    if (decoded) {
        const channel = channels.find((item) => item.id === decoded.channelId);
        return channel && channel.models.some((item) => item.name === decoded.model) ? model : "";
    }
    const channel = channels.find((item) => item.models.some((entry) => entry.name === model)) || channels[0];
    return channel && channel.models.some((item) => item.name === model) ? encodeChannelModel(channel.id, model) : model;
}

export function resolveModelChannel(config: AiConfig, value: string) {
    const decoded = decodeChannelModel(value);
    const model = decoded?.model || value;
    const matched = decoded ? config.channels.find((channel) => channel.id === decoded.channelId) : config.channels.find((channel) => channel.models.some((item) => item.name === model));
    return matched || config.channels[0] || createModelChannel({ id: "default", name: i18n.t("config.channels.defaultName"), baseUrl: config.baseUrl, apiKey: config.apiKey, apiFormat: config.apiFormat, models: config.models.map(modelOptionName).map((name) => ({ name, capability: guessCapability(name) })) });
}

export function resolveModelRequestConfig(config: AiConfig, value: string) {
    const channel = resolveModelChannel(config, value);
    return {
        ...config,
        model: modelOptionName(value || config.model),
        baseUrl: channel.baseUrl,
        apiKey: channel.apiKey,
        apiFormat: channel.apiFormat,
    };
}

function normalizeChannels(config: AiConfig) {
    const persistedChannels = Array.isArray(config.channels) ? config.channels : [];
    const channels = persistedChannels.map((channel, index) =>
        createModelChannel({
            ...channel,
            id: channel.id || (index === 0 ? "default" : `channel-${index + 1}`),
            name: channel.name || (index === 0 ? i18n.t("config.channels.defaultName") : i18n.t("config.channels.indexedName", { index: index + 1 })),
            models: normalizeChannelModels(channel.models),
        }),
    );
    if (!channels.length) {
        channels.push(
            createModelChannel({
                id: "default",
                name: i18n.t("config.channels.defaultName"),
                baseUrl: config.baseUrl || defaultConfig.baseUrl,
                apiKey: config.apiKey || "",
                apiFormat: config.apiFormat || defaultConfig.apiFormat,
                models: normalizeChannelModels([config.model, config.imageModel, config.videoModel, config.textModel, config.audioModel].map(modelOptionName)),
            }),
        );
    }
    return channels;
}

export function defaultBaseUrlForApiFormat(apiFormat: ApiCallFormat) {
    if (apiFormat === "gemini") return GEMINI_BASE_URL;
    if (apiFormat === "ark") return ARK_BASE_URL;
    return OPENAI_BASE_URL;
}

function normalizeApiFormat(apiFormat: unknown): ApiCallFormat {
    return apiFormat === "gemini" || apiFormat === "ark" ? apiFormat : "openai";
}

function uniqueModelOptions(models: string[]) {
    return Array.from(new Set((models || []).map((model) => model.trim()).filter(Boolean)));
}

export function buildApiUrl(baseUrl: string, path: string) {
    let normalizedBaseUrl = baseUrl.trim().replace(/\/+$/, "");
    normalizedBaseUrl = normalizeArkPlanBaseUrl(normalizedBaseUrl);
    const lowerBaseUrl = normalizedBaseUrl.toLowerCase();
    const apiBaseUrl = lowerBaseUrl.endsWith("/v1") || lowerBaseUrl.endsWith("/api/v3") || lowerBaseUrl.endsWith("/api/plan/v3") ? normalizedBaseUrl : `${normalizedBaseUrl}/v1`;
    return `${apiBaseUrl}${path}`;
}

function normalizeArkPlanBaseUrl(baseUrl: string) {
    try {
        const url = new URL(baseUrl);
        const path = url.pathname.replace(/\/+$/, "");
        const lowerPath = path.toLowerCase();
        const arkPlanIndex = lowerPath.indexOf("/api/plan/v3");
        if (arkPlanIndex < 0) return baseUrl;
        const end = arkPlanIndex + "/api/plan/v3".length;
        if (lowerPath.length !== end && lowerPath[end] !== "/") return baseUrl;
        url.pathname = path.slice(0, end);
        url.search = "";
        url.hash = "";
        return url.toString().replace(/\/+$/, "");
    } catch {
        return baseUrl;
    }
}
