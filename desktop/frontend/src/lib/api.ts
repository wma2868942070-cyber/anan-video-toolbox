// Thin typed wrapper around Wails-generated bindings. Wails injects
// window.go.<package>.<struct>.<method>. We avoid hard imports from
// wailsjs/go so the project compiles even before the first dev/build run.

export type Cookie = {
  id: number;
  account_id: string;
  email: string;
  auto_recoverable: boolean;
  is_active: boolean;
  session_status: string;
  jwt_expires_at: number;
  refresh_fail_count: number;
  refresh_fail_reason: string;
  error_until: number;
  last_refresh_at: number;
  last_balance: number;
  last_error: string;
  last_used_at: number;
  last_checked_at: number;
  disabled_reason: string;
  disabled_at: number;
  created_at: number;
  status: "READY" | "TEMPORARY" | "INVALID" | "ABNORMAL" | "DEPLETED" | "DISABLED";
};

export type AddCookieResult = {
  email: string;
  balance: number;
  updated_existing: boolean;
  merged_duplicates: number;
  warning?: string;
};

export type DeleteCookieResult = {
  workspaces_removed: number;
};

export type CookieRefreshResult = {
  checked: number;
  ok: number;
	reenabled: number;
  merged: number;
};

export type CookieHealth = {
  total: number;
  ready: number;
  temporary: number;
  depleted: number;
  disabled: number;
  total_balance: number;
  active_balance: number;
};

export type LeonardoBrowserWorkspace = {
  id: string;
  name: string;
  browser: string;
  bound: boolean;
  profileDir: string;
  debugPort: number;
  pid: number;
  lastOpenedAt: number;
};

export type LeonardoServiceStatus = {
  running: boolean;
  baseURL: string;
  adminURL: string;
  total: number;
  ready: number;
  cooling: number;
  disabled: number;
  totalBalance: number;
  activeTasks: number;
  leonardoTasks: number;
  error: string;
};

export type ImageModel = {
  id: number;
  name: string;
  modelId: string;
  sdVersion: string;
  isDefault: boolean;
  createdAt: number;
};

export type VideoModel = {
  name: string;
  family: string;
  slug: string;
  modelValue: string;
  requestProfile: string;
  defaultMode: string;
  supportedModes: string[];
  durationOptions: number[];
  defaultDuration: number;
  supportsAudio: boolean;
  audioPolicy: "none" | "optional" | "required" | string;
  supportsRefImage: boolean;
  requiresRefImage: boolean;
  supportsEndFrame: boolean;
  supportsImageReference: boolean;
  supportsVideoReference: boolean;
  supportsAudioReference: boolean;
  defaultAspect: string;
  docsURL: string;
  notes: string;
};

export type AspectRatioOption = {
  label: string;
  width: number;
  height: number;
};

export type GenerationLog = {
  id: number;
  provider: "leonardo" | "adobe" | string;
  providerGenerationID: string;
  providerAccountID: string;
  mediaType: "image" | "video" | string;
  metadataJSON: string;
  usedCookieID: number;
  modelID: string;
  aspectRatio: string;
  prompt: string;
  imageURLs: string[];
  savedFiles: string[];
  saveEnabled: boolean;
  status: string;
  errorMessage: string;
  createdAt: number;
};

export type AdobeServiceStatus = {
  running: boolean;
  baseURL: string;
  stateDir: string;
  sourceDir: string;
  pythonPath: string;
  poolSize: number;
  error: string;
  adminURL: string;
  cookiePlugin: string;
};

export type VideoClawServiceStatus = {
  running: boolean;
  backendRunning: boolean;
  frontendRunning: boolean;
  backendURL: string;
  frontendURL: string;
  stateDir: string;
  sourceDir: string;
  pythonPath: string;
  error: string;
};

export type LocalMiniDramaModelConfig = {
  name: string;
  service_type: "image" | "video" | string;
  model_count: number;
  default_model: string;
  is_active: boolean;
  updated_at: string;
};

export type LocalMiniDramaServiceStatus = {
  running: boolean;
  url: string;
  stateDir: string;
  sourceDir: string;
  nodePath: string;
  error: string;
  modelConfigs: LocalMiniDramaModelConfig[];
};

export type AdobeProfile = {
  id: string;
  name: string;
  enabled: boolean;
  imported_at?: number;
  account?: { display_name?: string; email?: string; user_id?: string; updated_at?: number };
  state?: {
    last_error?: string;
    last_http_status?: number;
    last_success_at?: number;
    last_attempt_at?: number;
    next_refresh_at_text?: string;
    last_success_at_text?: string;
    last_attempt_at_text?: string;
  };
};

export type AdobeToken = {
  id: string;
  status: string;
  account_id?: string;
  account_name?: string;
  account_email?: string;
  auto_refresh?: boolean;
  refresh_profile_id?: string;
  credits?: { remaining?: number; total?: number; used?: number } | number;
  credits_error?: string;
};

export type AdobeBrowserWorkspace = {
  id: string;
  name: string;
  browser?: string;
  profileDir: string;
  debugPort: number;
  pid: number;
  lastOpenedAt: number;
};

export type LibrarySyncResult = {
  accounts: number;
  synced_accounts: number;
  remote_items: number;
  added: number;
  updated: number;
  skipped: number;
  failed: number;
  errors: string[];
};

export type SaveGenerationResult = {
  files: string[];
  failed: number;
};

export type ProviderConfig = {
  provider: "adobe" | "leo" | string;
  name: string;
  baseURL: string;
  enabled: boolean;
  apiKeyConfigured: boolean;
  apiKeyMasked: string;
  capabilities: string[];
};

export type ProviderConfigInput = {
  provider: string;
  baseURL: string;
  apiKey?: string;
  enabled: boolean;
  clearApiKey?: boolean;
};

export type ProviderConnectionResult = {
  provider: string;
  reachable: boolean;
  httpStatus: number;
  message: string;
  modelCount: number;
  checkedAt: number;
};

export type ImageGenerateRequest = {
  prompt: string;
  modelId?: string;
  n?: number;
  aspectRatio?: string;
  referenceImageURLs?: string[];
  referenceImageIds?: string[];
};

export type ImageGenerateResponse = {
  created: number;
  data: Array<{ url: string }>;
  provider: {
    provider?: string;
    generation_id: string;
    used_cookie_id: number;
    aspect_ratio: string;
    model_id: string;
    saved_files: string[];
    auto_save_enabled: boolean;
    save_error?: string;
  };
};

export type VideoGenerateRequest = {
  prompt: string;
  modelSlug?: string;
  aspectRatio?: string;
  resolution?: string;
  duration?: number;
  audio?: boolean;
  imageURL?: string;
  imageId?: string;
};

export type VideoGenerateResponse = {
  created: number;
  data: Array<{
    url: string;
    mp4_url: string;
    gif_url?: string;
    thumbnail_url?: string;
    width?: number;
    height?: number;
  }>;
  provider: {
    generation_id: string;
    credit_cost: number;
    credit_cost_source: string;
    used_cookie_id: number;
    model: string;
    resolution: string;
    duration: number;
    aspect_ratio: string;
    audio: boolean;
    saved_files: string[];
    auto_save_enabled: boolean;
    save_error?: string;
  };
};

export type AppInfo = {
  name: string;
  version: string;
  author: string;
  repository: string;
  license: string;
};

export type ModelSyncResult = {
  Total: number;
  Added: number;
  Updated: number;
  Sample: string[];
};

function bindings() {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const w = window as any;
  if (!w.go || !w.go.desktop || !w.go.desktop.App) {
    throw new Error("Wails bindings not available yet (window.go.desktop.App)");
  }
  return w.go.desktop.App;
}

export const api = {
  ping: (): Promise<string> => bindings().Ping(),
  appInfo: (): Promise<AppInfo> => bindings().AppInfo(),
  openURL: (url: string): Promise<void> => bindings().OpenURL(url),

  // VideoClaw AI director
  videoClawServiceStatus: (): Promise<VideoClawServiceStatus> =>
    bindings().VideoClawServiceStatus(),
  startVideoClawService: (): Promise<VideoClawServiceStatus> =>
    bindings().StartVideoClawService(),
  restartVideoClawService: (): Promise<VideoClawServiceStatus> =>
    bindings().RestartVideoClawService(),

  // LocalMiniDrama
  localMiniDramaServiceStatus: (): Promise<LocalMiniDramaServiceStatus> =>
    bindings().LocalMiniDramaServiceStatus(),
  startLocalMiniDramaService: (): Promise<LocalMiniDramaServiceStatus> =>
    bindings().StartLocalMiniDramaService(),
  restartLocalMiniDramaService: (): Promise<LocalMiniDramaServiceStatus> =>
    bindings().RestartLocalMiniDramaService(),
  syncLocalMiniDramaModels: (): Promise<Record<string, unknown>> =>
    bindings().SyncLocalMiniDramaModels(),

  // Cookies
  leonardoServiceStatus: (): Promise<LeonardoServiceStatus> =>
    bindings().LeonardoServiceStatus(),
  restartLeonardoService: (): Promise<LeonardoServiceStatus> =>
    bindings().RestartLeonardoService(),
  listCookies: (): Promise<Cookie[]> => bindings().ListCookies(),
  addCookie: (raw: string): Promise<AddCookieResult> =>
    bindings().AddCookie(raw),
  updateCookie: (id: number, raw: string): Promise<AddCookieResult> =>
    bindings().UpdateCookie(id, raw),
  deleteCookie: (id: number): Promise<DeleteCookieResult> => bindings().DeleteCookie(id),
  toggleCookie: (id: number, enabled: boolean): Promise<void> =>
    bindings().ToggleCookie(id, enabled),
  refreshCookieProfiles: (): Promise<CookieRefreshResult> =>
    bindings().RefreshCookieProfiles(),
  refreshCookieSessions: (): Promise<CookieRefreshResult> =>
    bindings().RefreshCookieSessions(),
  cookieHealth: (): Promise<CookieHealth> => bindings().CookieHealth(),
  listLeonardoBrowserWorkspaces: (): Promise<LeonardoBrowserWorkspace[]> =>
    bindings().ListLeonardoBrowserWorkspaces(),
  launchLeonardoBrowserWorkspace: (name: string): Promise<LeonardoBrowserWorkspace> =>
    bindings().LaunchLeonardoBrowserWorkspace(name),
  reopenLeonardoBrowserWorkspace: (id: string): Promise<LeonardoBrowserWorkspace> =>
    bindings().ReopenLeonardoBrowserWorkspace(id),
  importLeonardoBrowserWorkspace: (id: string): Promise<AddCookieResult> =>
    bindings().ImportLeonardoBrowserWorkspace(id),

  // Settings
  getSetting: (key: string, fallback: string): Promise<string> =>
    bindings().GetSetting(key, fallback),
  setSetting: (key: string, value: string): Promise<void> =>
    bindings().SetSetting(key, value),
  listProviderConfigs: (): Promise<ProviderConfig[]> =>
    bindings().ListProviderConfigs(),
  saveProviderConfig: (input: ProviderConfigInput): Promise<ProviderConfig> =>
    bindings().SaveProviderConfig(input),
  testProviderConnection: (provider: string): Promise<ProviderConnectionResult> =>
    bindings().TestProviderConnection(provider),

  // Adobe Firefly
  adobeServiceStatus: (): Promise<AdobeServiceStatus> => bindings().AdobeServiceStatus(),
  startAdobeService: (): Promise<AdobeServiceStatus> => bindings().StartAdobeService(),
  restartAdobeService: (): Promise<AdobeServiceStatus> => bindings().RestartAdobeService(),
  listAdobeProfiles: (): Promise<{ profiles?: AdobeProfile[] }> => bindings().ListAdobeProfiles(),
  listAdobeTokens: (): Promise<{ tokens?: AdobeToken[]; data?: AdobeToken[] }> => bindings().ListAdobeTokens(),
  getAdobeConfig: (): Promise<Record<string, unknown>> => bindings().GetAdobeConfig(),
  updateAdobeConfig: (values: Record<string, unknown>): Promise<Record<string, unknown>> => bindings().UpdateAdobeConfig(values),
  importAdobeCookie: (name: string, raw: string): Promise<Record<string, unknown>> => bindings().ImportAdobeCookie(name, raw),
  importAdobeCookieFiles: (): Promise<Record<string, unknown>> => bindings().ImportAdobeCookieFiles(),
  refreshAdobeProfile: (id: string): Promise<Record<string, unknown>> => bindings().RefreshAdobeProfile(id),
  refreshAllAdobeProfiles: (): Promise<Record<string, unknown>> => bindings().RefreshAllAdobeProfiles(),
  toggleAdobeProfile: (id: string, enabled: boolean): Promise<Record<string, unknown>> => bindings().ToggleAdobeProfile(id, enabled),
  deleteAdobeProfile: (id: string): Promise<Record<string, unknown>> => bindings().DeleteAdobeProfile(id),
  listAdobeBrowserWorkspaces: (): Promise<AdobeBrowserWorkspace[]> => bindings().ListAdobeBrowserWorkspaces(),
  launchAdobeBrowserWorkspace: (name: string): Promise<AdobeBrowserWorkspace> => bindings().LaunchAdobeBrowserWorkspace(name),
  importAdobeBrowserWorkspace: (id: string): Promise<Record<string, unknown>> => bindings().ImportAdobeBrowserWorkspace(id),
  adobeAdminPassword: (): Promise<string> => bindings().AdobeAdminPassword(),
  resetAdobeAdminPassword: (): Promise<string> => bindings().ResetAdobeAdminPassword(),
  syncAdobeModels: (): Promise<{ data?: Array<Record<string, unknown>> }> => bindings().SyncAdobeModels(),

  // Image
  generateImage: (req: ImageGenerateRequest): Promise<ImageGenerateResponse> =>
    bindings().GenerateImage(req),
  listImageModels: (): Promise<ImageModel[]> => bindings().ListImageModels(),
  syncImageModels: (): Promise<ModelSyncResult> =>
    bindings().SyncImageModels(),
  addImageModel: (name: string, modelId: string): Promise<void> =>
    bindings().AddImageModel(name, modelId),
  deleteImageModel: (id: number): Promise<void> =>
    bindings().DeleteImageModel(id),
  setDefaultImageModel: (id: number): Promise<void> =>
    bindings().SetDefaultImageModel(id),
  listImageAspects: (): Promise<AspectRatioOption[]> =>
    bindings().ListImageAspects(),

  // Video
  generateVideo: (req: VideoGenerateRequest): Promise<VideoGenerateResponse> =>
    bindings().GenerateVideo(req),
  listVideoModels: (): Promise<VideoModel[]> => bindings().ListVideoModels(),

  // Library
  listGenerationLogs: (limit: number): Promise<GenerationLog[]> =>
    bindings().ListGenerationLogs(limit),
  syncLeonardoLibrary: (limit: number): Promise<LibrarySyncResult> =>
    bindings().SyncLeonardoLibrary(limit),
  deleteGenerationLog: (id: number): Promise<void> =>
    bindings().DeleteGenerationLog(id),
  saveGenerationLog: (id: number): Promise<SaveGenerationResult> =>
    bindings().SaveGenerationLog(id),

  // Filesystem dialogs
  openDirectoryDialog: (currentPath: string): Promise<string> =>
    bindings().OpenDirectoryDialog(currentPath),
  openInFileManager: (path: string): Promise<void> =>
    bindings().OpenInFileManager(path),
  downloadAsset: (url: string, suggestedName: string): Promise<string> =>
    bindings().DownloadAsset(url, suggestedName),

  // Local file upload (drag-drop / file picker)
  uploadLocalImage: (base64: string, extension: string): Promise<string> =>
    bindings().UploadLocalImage(base64, extension),
};
