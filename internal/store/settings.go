package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// GetSetting returns the value for key or the default when missing.
func (s *Store) GetSetting(key, fallback string) (string, error) {
	var v string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	return v, nil
}

// SetSetting upserts a value for key.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		"INSERT INTO settings (key, value) VALUES (?, ?) "+
			"ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	return err
}

// AddGenerationLog records a generation result for auditing.
func (s *Store) AddGenerationLog(
	providerGenerationID string,
	usedCookieID int64,
	modelID string,
	aspectRatio string,
	prompt string,
	imageURLs []string,
	savedFiles []string,
	saveEnabled bool,
	status string,
	errorMessage string,
) error {
	return s.AddProviderGenerationLog(
		"leonardo", providerGenerationID, "", "image", "{}", usedCookieID,
		modelID, aspectRatio, prompt, imageURLs, savedFiles, saveEnabled, status, errorMessage,
	)
}

// AddProviderGenerationLog records a generation from Leonardo, Adobe, or a
// future provider while keeping the original AddGenerationLog API compatible.
func (s *Store) AddProviderGenerationLog(
	provider string,
	providerGenerationID string,
	providerAccountID string,
	mediaType string,
	metadataJSON string,
	usedCookieID int64,
	modelID string,
	aspectRatio string,
	prompt string,
	imageURLs []string,
	savedFiles []string,
	saveEnabled bool,
	status string,
	errorMessage string,
) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "leonardo"
	}
	mediaType = strings.TrimSpace(mediaType)
	if mediaType == "" {
		mediaType = "image"
	}
	if !json.Valid([]byte(metadataJSON)) {
		metadataJSON = "{}"
	}
	if len(errorMessage) > 400 {
		errorMessage = errorMessage[:400]
	}

	urlsJSON, err := json.Marshal(emptyOnNil(imageURLs))
	if err != nil {
		return err
	}
	savedJSON, err := json.Marshal(emptyOnNil(savedFiles))
	if err != nil {
		return err
	}

	saveFlag := 0
	if saveEnabled {
		saveFlag = 1
	}

	_, err = s.db.Exec(
		`INSERT INTO generation_logs (
			provider,
			provider_generation_id,
			provider_account_id,
			media_type,
			metadata_json,
			used_cookie_id,
			model_id,
			aspect_ratio,
			prompt,
			image_urls_json,
			saved_files_json,
			save_enabled,
			status,
			error_message,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		provider,
		providerGenerationID,
		providerAccountID,
		mediaType,
		metadataJSON,
		usedCookieID,
		modelID,
		aspectRatio,
		prompt,
		string(urlsJSON),
		string(savedJSON),
		saveFlag,
		status,
		errorMessage,
		nowTS(),
	)
	return err
}

func emptyOnNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// GenerationLog is a single audit row used by the Library UI.
type GenerationLog struct {
	ID                   int64
	Provider             string
	ProviderGenerationID string
	ProviderAccountID    string
	MediaType            string
	MetadataJSON         string
	UsedCookieID         int64
	ModelID              string
	AspectRatio          string
	Prompt               string
	ImageURLsJSON        string
	SavedFilesJSON       string
	SaveEnabled          int
	Status               string
	ErrorMessage         string
	Hidden               int
	CreatedAt            int64
}

// ListGenerationLogs returns the most recent rows, newest first.
func (s *Store) ListGenerationLogs(limit int) ([]GenerationLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, provider, provider_generation_id, provider_account_id, media_type, metadata_json,
		        used_cookie_id, model_id, aspect_ratio,
		        prompt, image_urls_json, saved_files_json, save_enabled,
		        status, error_message, hidden, created_at
		 FROM generation_logs WHERE hidden = 0 ORDER BY created_at DESC, id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GenerationLog
	for rows.Next() {
		var g GenerationLog
		if err := rows.Scan(
			&g.ID, &g.Provider, &g.ProviderGenerationID, &g.ProviderAccountID, &g.MediaType, &g.MetadataJSON,
			&g.UsedCookieID, &g.ModelID, &g.AspectRatio,
			&g.Prompt, &g.ImageURLsJSON, &g.SavedFilesJSON, &g.SaveEnabled,
			&g.Status, &g.ErrorMessage, &g.Hidden, &g.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GetGenerationLog returns one material row, including hidden rows so saved
// assets and deletions can be handled consistently.
func (s *Store) GetGenerationLog(id int64) (GenerationLog, error) {
	var g GenerationLog
	err := s.db.QueryRow(
		`SELECT id, provider, provider_generation_id, provider_account_id, media_type, metadata_json,
		        used_cookie_id, model_id, aspect_ratio,
		        prompt, image_urls_json, saved_files_json, save_enabled,
		        status, error_message, hidden, created_at
		 FROM generation_logs WHERE id = ?`, id,
	).Scan(
		&g.ID, &g.Provider, &g.ProviderGenerationID, &g.ProviderAccountID, &g.MediaType, &g.MetadataJSON,
		&g.UsedCookieID, &g.ModelID, &g.AspectRatio,
		&g.Prompt, &g.ImageURLsJSON, &g.SavedFilesJSON, &g.SaveEnabled,
		&g.Status, &g.ErrorMessage, &g.Hidden, &g.CreatedAt,
	)
	return g, err
}

// FindSuccessfulGenerationByClientRequest returns the newest completed
// result for a caller-supplied idempotency key. The key lives in metadata_json
// so existing databases do not need another migration. Prompt/model checks are
// exact when modelID is supplied; an empty modelID intentionally permits a
// provider alias to resolve to its canonical model while the request key and
// prompt still remain exact.
func (s *Store) FindSuccessfulGenerationByClientRequest(
	provider string,
	mediaType string,
	clientRequestID string,
	modelID string,
	prompt string,
) (GenerationLog, error) {
	var g GenerationLog
	clientRequestID = strings.TrimSpace(clientRequestID)
	if clientRequestID == "" {
		return g, sql.ErrNoRows
	}
	provider = strings.TrimSpace(provider)
	mediaType = strings.TrimSpace(mediaType)
	err := s.db.QueryRow(
		`SELECT id, provider, provider_generation_id, provider_account_id, media_type, metadata_json,
		        used_cookie_id, model_id, aspect_ratio,
		        prompt, image_urls_json, saved_files_json, save_enabled,
		        status, error_message, hidden, created_at
		 FROM generation_logs
		 WHERE provider = ? AND media_type = ? AND status = 'success' AND hidden = 0
		   AND json_extract(metadata_json, '$.client_request_id') = ?
		   AND (? = '' OR model_id = ?) AND prompt = ?
		 ORDER BY id DESC LIMIT 1`,
		provider, mediaType, clientRequestID, modelID, modelID, prompt,
	).Scan(
		&g.ID, &g.Provider, &g.ProviderGenerationID, &g.ProviderAccountID, &g.MediaType, &g.MetadataJSON,
		&g.UsedCookieID, &g.ModelID, &g.AspectRatio,
		&g.Prompt, &g.ImageURLsJSON, &g.SavedFilesJSON, &g.SaveEnabled,
		&g.Status, &g.ErrorMessage, &g.Hidden, &g.CreatedAt,
	)
	return g, err
}

// HideGenerationLog removes a row from the local material library without
// deleting the source generation from Leonardo. Hidden rows remain hidden
// after a later remote sync.
func (s *Store) HideGenerationLog(id int64) error {
	res, err := s.db.Exec("UPDATE generation_logs SET hidden = 1 WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetGenerationSavedFiles records files created through the Library's Save
// action. Existing entries are preserved and duplicates are removed.
func (s *Store) SetGenerationSavedFiles(id int64, files []string) error {
	g, err := s.GetGenerationLog(id)
	if err != nil {
		return err
	}
	var current []string
	_ = json.Unmarshal([]byte(g.SavedFilesJSON), &current)
	seen := make(map[string]struct{}, len(current)+len(files))
	merged := make([]string, 0, len(current)+len(files))
	for _, path := range append(current, files...) {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		merged = append(merged, path)
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		"UPDATE generation_logs SET saved_files_json = ?, save_enabled = 1 WHERE id = ?",
		string(raw), id,
	)
	return err
}

// UpsertGenerationLogFromRemote imports or refreshes one Leonardo-owned
// generation while preserving local-only saved-files and hidden state.
// It returns (added, updated, error).
func (s *Store) UpsertGenerationLogFromRemote(
	providerGenerationID string,
	usedCookieID int64,
	modelID string,
	aspectRatio string,
	prompt string,
	imageURLs []string,
	status string,
	createdAt int64,
) (bool, bool, error) {
	return s.UpsertProviderGenerationLogFromRemote(
		"leonardo", providerGenerationID, "", "image", "{}", usedCookieID,
		modelID, aspectRatio, prompt, imageURLs, status, createdAt,
	)
}

// UpsertProviderGenerationLogFromRemote imports provider-owned generations
// without colliding when two providers reuse the same generation id.
func (s *Store) UpsertProviderGenerationLogFromRemote(
	provider string,
	providerGenerationID string,
	providerAccountID string,
	mediaType string,
	metadataJSON string,
	usedCookieID int64,
	modelID string,
	aspectRatio string,
	prompt string,
	imageURLs []string,
	status string,
	createdAt int64,
) (bool, bool, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "leonardo"
	}
	mediaType = strings.TrimSpace(mediaType)
	if mediaType == "" {
		mediaType = "image"
	}
	if !json.Valid([]byte(metadataJSON)) {
		metadataJSON = "{}"
	}
	providerGenerationID = strings.TrimSpace(providerGenerationID)
	if providerGenerationID == "" {
		return false, false, errors.New("store: empty provider generation id")
	}
	if createdAt <= 0 {
		createdAt = nowTS()
	}
	urlsJSON, err := json.Marshal(emptyOnNil(imageURLs))
	if err != nil {
		return false, false, err
	}

	var existing GenerationLog
	err = s.db.QueryRow(
		`SELECT id, provider, provider_generation_id, provider_account_id, media_type, metadata_json,
		        used_cookie_id, model_id, aspect_ratio,
		        prompt, image_urls_json, saved_files_json, save_enabled,
		        status, error_message, hidden, created_at
		 FROM generation_logs
		 WHERE provider = ? AND provider_generation_id = ?
		 ORDER BY id DESC LIMIT 1`, provider, providerGenerationID,
	).Scan(
		&existing.ID, &existing.Provider, &existing.ProviderGenerationID,
		&existing.ProviderAccountID, &existing.MediaType, &existing.MetadataJSON, &existing.UsedCookieID,
		&existing.ModelID, &existing.AspectRatio, &existing.Prompt,
		&existing.ImageURLsJSON, &existing.SavedFilesJSON, &existing.SaveEnabled,
		&existing.Status, &existing.ErrorMessage, &existing.Hidden, &existing.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.db.Exec(
			`INSERT INTO generation_logs (
				provider, provider_generation_id, provider_account_id, media_type, metadata_json,
				used_cookie_id, model_id, aspect_ratio,
				prompt, image_urls_json, saved_files_json, save_enabled,
				status, error_message, hidden, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '[]', 0, ?, '', 0, ?)`,
			provider, providerGenerationID, providerAccountID, mediaType, metadataJSON,
			usedCookieID, modelID, aspectRatio, prompt,
			string(urlsJSON), status, createdAt,
		)
		return err == nil, false, err
	}
	if err != nil {
		return false, false, err
	}

	changed := existing.ProviderAccountID != providerAccountID ||
		existing.MediaType != mediaType ||
		existing.MetadataJSON != metadataJSON ||
		existing.UsedCookieID != usedCookieID ||
		existing.ModelID != modelID ||
		existing.AspectRatio != aspectRatio ||
		existing.Prompt != prompt ||
		existing.ImageURLsJSON != string(urlsJSON) ||
		existing.Status != status ||
		existing.CreatedAt != createdAt
	if !changed {
		return false, false, nil
	}
	_, err = s.db.Exec(
		`UPDATE generation_logs
		 SET provider_account_id = ?, media_type = ?, metadata_json = ?,
		     used_cookie_id = ?, model_id = ?, aspect_ratio = ?, prompt = ?,
		     image_urls_json = ?, status = ?, error_message = '', created_at = ?
		 WHERE id = ?`,
		providerAccountID, mediaType, metadataJSON, usedCookieID, modelID, aspectRatio, prompt, string(urlsJSON), status,
		createdAt, existing.ID,
	)
	return false, err == nil, err
}
