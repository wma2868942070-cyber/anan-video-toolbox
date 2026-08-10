package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// CookieUpsertResult describes whether a validated Leonardo account created a
// new pool row or refreshed an existing account. Merged is the number of stale
// duplicate rows removed while preserving generation-log references.
type CookieUpsertResult struct {
	ID              int64
	UpdatedExisting bool
	Merged          int
}

// ListCookies returns all cookies ordered by id desc (matches Python).
func (s *Store) ListCookies() ([]Cookie, error) {
	return s.queryCookies("SELECT * FROM cookies ORDER BY id DESC")
}

// ListActiveCookies returns active cookies ordered by least-recently-used.
func (s *Store) ListActiveCookies() ([]Cookie, error) {
	return s.queryCookies(
		"SELECT * FROM cookies WHERE is_active = 1 ORDER BY last_used_at ASC, id ASC",
	)
}

// AddCookie inserts a cookie value. Idempotent on the unique constraint.
func (s *Store) AddCookie(value string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil
	}
	_, err := s.db.Exec(
		"INSERT OR IGNORE INTO cookies (value, is_active, created_at) VALUES (?, 1, ?)",
		v, nowTS(),
	)
	return err
}

// UpsertCookieAccount stores one row per stable Leonardo account id. Email is
// display metadata only: it must never be used to merge accounts because the
// pool is expected to preserve every independently logged-in account.
func (s *Store) UpsertCookieAccount(value, accountID, email string, balance int64) (CookieUpsertResult, error) {
	v := strings.TrimSpace(value)
	accountID = strings.TrimSpace(accountID)
	email = strings.TrimSpace(email)
	if v == "" {
		return CookieUpsertResult{}, errors.New("store: empty cookie value")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return CookieUpsertResult{}, fmt.Errorf("store: begin cookie upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(`
		SELECT id FROM cookies
		WHERE ? <> '' AND account_id = ?
		ORDER BY id ASC`, accountID, accountID)
	if err != nil {
		return CookieUpsertResult{}, fmt.Errorf("store: find cookie account: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return CookieUpsertResult{}, fmt.Errorf("store: scan cookie account: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return CookieUpsertResult{}, fmt.Errorf("store: close cookie account rows: %w", err)
	}

	// A Leonardo web account switch may reuse the exact same browser session
	// Cookie while changing the active remote account. Overwriting the old row
	// here would make that account disappear from a multi-account pool. Refuse
	// the ambiguous import and instruct the caller to use isolated browser
	// profiles, which produce independent long-lived sessions.
	var valueOwnerID int64
	var valueOwnerAccountID, valueOwnerEmail string
	err = tx.QueryRow(
		"SELECT id, account_id, email FROM cookies WHERE value = ? LIMIT 1", v,
	).Scan(&valueOwnerID, &valueOwnerAccountID, &valueOwnerEmail)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CookieUpsertResult{}, fmt.Errorf("store: find cookie session owner: %w", err)
	}
	if err == nil {
		sameIdentity := accountID != "" && strings.TrimSpace(valueOwnerAccountID) == accountID
		legacySameIdentity := strings.TrimSpace(valueOwnerAccountID) == "" &&
			accountID != "" && email != "" && strings.EqualFold(strings.TrimSpace(valueOwnerEmail), email)
		if !sameIdentity && !legacySameIdentity {
			return CookieUpsertResult{}, errors.New("这个 Cookie 与账号池中的另一个账号共用了同一浏览器会话；为防止旧账号被覆盖，请使用不同浏览器配置文件分别登录每个 Leonardo 账号，再复制各自的 get-session cURL")
		}
		ownerIncluded := false
		for _, id := range ids {
			if id == valueOwnerID {
				ownerIncluded = true
				break
			}
		}
		if !ownerIncluded {
			ids = append(ids, valueOwnerID)
		}
	}

	now := nowTS()
	if len(ids) == 0 {
		res, err := tx.Exec(`
			INSERT INTO cookies (
				value, account_id, email, last_balance, last_checked_at,
				is_active, last_error, disabled_reason, disabled_at, created_at
			) VALUES (?, ?, ?, ?, ?, 1, '', '', 0, ?)`,
			v, accountID, email, balance, now, now,
		)
		if err != nil {
			return CookieUpsertResult{}, fmt.Errorf("store: insert cookie account: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return CookieUpsertResult{}, fmt.Errorf("store: read inserted cookie id: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return CookieUpsertResult{}, fmt.Errorf("store: commit cookie insert: %w", err)
		}
		return CookieUpsertResult{ID: id}, nil
	}

	primaryID := ids[0]
	for _, duplicateID := range ids[1:] {
		if _, err := tx.Exec(
			"UPDATE generation_logs SET used_cookie_id = ? WHERE used_cookie_id = ?",
			primaryID, duplicateID,
		); err != nil {
			return CookieUpsertResult{}, fmt.Errorf("store: reassign generation logs: %w", err)
		}
		if _, err := tx.Exec("DELETE FROM cookies WHERE id = ?", duplicateID); err != nil {
			return CookieUpsertResult{}, fmt.Errorf("store: delete duplicate cookie: %w", err)
		}
	}

	if _, err := tx.Exec(`
		UPDATE cookies
		SET value = ?, account_id = ?, email = ?, last_balance = ?,
			last_checked_at = ?, is_active = 1, last_error = '',
			disabled_reason = '', disabled_at = 0
		WHERE id = ?`,
		v, accountID, email, balance, now, primaryID,
	); err != nil {
		return CookieUpsertResult{}, fmt.Errorf("store: refresh cookie account: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return CookieUpsertResult{}, fmt.Errorf("store: commit cookie refresh: %w", err)
	}
	return CookieUpsertResult{
		ID:              primaryID,
		UpdatedExisting: true,
		Merged:          len(ids) - 1,
	}, nil
}

// MergeDuplicateCookieAccounts repairs only rows proven to share the same
// stable Leonardo account id. It deliberately does not merge by email or raw
// Cookie value because either can represent an ambiguous multi-account web
// session.
func (s *Store) MergeDuplicateCookieAccounts() (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("store: begin duplicate merge: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	merged := 0
	mergeGroups := func(groupQuery, memberQuery string) error {
		rows, err := tx.Query(groupQuery)
		if err != nil {
			return err
		}
		var keys []string
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				_ = rows.Close()
				return err
			}
			keys = append(keys, key)
		}
		if err := rows.Close(); err != nil {
			return err
		}

		for _, key := range keys {
			members, err := tx.Query(memberQuery, key)
			if err != nil {
				return err
			}
			var ids []int64
			for members.Next() {
				var id int64
				if err := members.Scan(&id); err != nil {
					_ = members.Close()
					return err
				}
				ids = append(ids, id)
			}
			if err := members.Close(); err != nil {
				return err
			}
			if len(ids) < 2 {
				continue
			}
			primaryID := ids[0]
			for _, duplicateID := range ids[1:] {
				if _, err := tx.Exec(
					"UPDATE generation_logs SET used_cookie_id = ? WHERE used_cookie_id = ?",
					primaryID, duplicateID,
				); err != nil {
					return err
				}
				if _, err := tx.Exec("DELETE FROM cookies WHERE id = ?", duplicateID); err != nil {
					return err
				}
				merged++
			}
		}
		return nil
	}

	if err := mergeGroups(
		`SELECT account_id FROM cookies WHERE trim(account_id) <> '' GROUP BY account_id HAVING count(*) > 1`,
		`SELECT id FROM cookies WHERE account_id = ? ORDER BY last_checked_at DESC, created_at DESC, id DESC`,
	); err != nil {
		return 0, fmt.Errorf("store: merge duplicate account ids: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit duplicate merge: %w", err)
	}
	return merged, nil
}

// GetCookieByValue returns the row matching the literal cookie payload.
func (s *Store) GetCookieByValue(value string) (*Cookie, error) {
	rows, err := s.queryCookies("SELECT * FROM cookies WHERE value = ? LIMIT 1", value)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// GetCookieByID returns one cookie row without exposing it outside the local
// service layer. It is used to ensure an edit cannot repurpose an existing
// account slot into a different Leonardo account.
func (s *Store) GetCookieByID(id int64) (*Cookie, error) {
	rows, err := s.queryCookies("SELECT * FROM cookies WHERE id = ? LIMIT 1", id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// UpdateCookieValue replaces the cookie payload for an existing id.
// Returns false if no change occurred or the new value collides.
func (s *Store) UpdateCookieValue(id int64, value string) (bool, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return false, nil
	}

	var current string
	err := s.db.QueryRow("SELECT value FROM cookies WHERE id = ? LIMIT 1", id).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: load cookie %d: %w", id, err)
	}
	if strings.TrimSpace(current) == v {
		return false, nil
	}

	if _, err := s.db.Exec("UPDATE cookies SET value = ? WHERE id = ?", v, id); err != nil {
		// Silently swallow uniqueness collisions like the Python version.
		if strings.Contains(err.Error(), "UNIQUE") {
			return false, nil
		}
		return false, fmt.Errorf("store: update cookie %d: %w", id, err)
	}
	return true, nil
}

// DeleteCookie removes a cookie row.
func (s *Store) DeleteCookie(id int64) error {
	_, err := s.db.Exec("DELETE FROM cookies WHERE id = ?", id)
	return err
}

// ToggleCookie flips the is_active flag and clears disabled metadata when re-enabled.
func (s *Store) ToggleCookie(id int64, enabled bool) error {
	if enabled {
		_, err := s.db.Exec(
			"UPDATE cookies SET is_active = 1, session_status = 'active', disabled_reason = '', disabled_at = 0, error_until = 0 WHERE id = ?",
			id,
		)
		return err
	}
	_, err := s.db.Exec("UPDATE cookies SET is_active = 0 WHERE id = ?", id)
	return err
}

// UpdateCookieSessionSuccess stores the cached short-lived JWT separately from
// the long-lived browser Cookie and clears the recoverable refresh state.
func (s *Store) UpdateCookieSessionSuccess(id int64, token string, expiresAt int64) error {
	now := nowTS()
	_, err := s.db.Exec(`
		UPDATE cookies
		SET jwt_token = ?, jwt_expires_at = ?, session_status = 'active',
			refresh_fail_count = 0, refresh_fail_reason = '', error_until = 0,
			last_refresh_at = ?, last_checked_at = ?, last_error = '',
			is_active = CASE
				WHEN disabled_reason IN ('AUTH_EXPIRED', 'INVALID', 'ABNORMAL', 'TEMPORARY_UNAVAILABLE') THEN 1
				ELSE is_active
			END,
			disabled_reason = CASE
				WHEN disabled_reason IN ('AUTH_EXPIRED', 'INVALID', 'ABNORMAL', 'TEMPORARY_UNAVAILABLE') THEN ''
				ELSE disabled_reason
			END,
			disabled_at = CASE
				WHEN disabled_reason IN ('AUTH_EXPIRED', 'INVALID', 'ABNORMAL', 'TEMPORARY_UNAVAILABLE') THEN 0
				ELSE disabled_at
			END
		WHERE id = ?`,
		token, expiresAt, now, now, id,
	)
	return err
}

// ClearCookieSessionJWT forces the next request to use get-session rather than
// retrying a JWT that Leonardo has already rejected.
func (s *Store) ClearCookieSessionJWT(id int64) error {
	_, err := s.db.Exec("UPDATE cookies SET jwt_token = '', jwt_expires_at = 0 WHERE id = ?", id)
	return err
}

// TryAcquireCookieRefreshLease serializes get-session across the desktop and
// the 8001 API process, which share one SQLite database but not one Go mutex.
func (s *Store) TryAcquireCookieRefreshLease(id int64, owner string, leaseUntil int64) (bool, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return false, errors.New("refresh lease owner is required")
	}
	now := nowTS()
	res, err := s.db.Exec(`
		UPDATE cookies SET refresh_lease_until = ?, refresh_lease_owner = ?
		WHERE id = ? AND (refresh_lease_until = 0 OR refresh_lease_until < ? OR refresh_lease_owner = ?)`,
		leaseUntil, owner, id, now, owner,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) ReleaseCookieRefreshLease(id int64, owner string) error {
	_, err := s.db.Exec(
		"UPDATE cookies SET refresh_lease_until = 0, refresh_lease_owner = '' WHERE id = ? AND refresh_lease_owner = ?",
		id, strings.TrimSpace(owner),
	)
	return err
}

// RecordCookieRefreshFailure increments the counter only for consecutive
// failures with the same normalized reason and installs exponential backoff.
func (s *Store) RecordCookieRefreshFailure(id int64, reason string, errorUntil int64) (int, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unknown refresh error"
	}
	if len(reason) > 160 {
		reason = reason[:160]
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var previous string
	var count int
	if err := tx.QueryRow("SELECT refresh_fail_reason, refresh_fail_count FROM cookies WHERE id = ?", id).Scan(&previous, &count); err != nil {
		return 0, err
	}
	if strings.EqualFold(strings.TrimSpace(previous), reason) {
		count++
	} else {
		count = 1
	}
	_, err = tx.Exec(`
		UPDATE cookies
		SET refresh_fail_reason = ?, refresh_fail_count = ?, error_until = ?,
			last_error = ?, last_checked_at = ?
		WHERE id = ?`, reason, count, errorUntil, reason, nowTS(), id)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

// SetCookieSessionState changes the refresh scheduler state. disable=true is
// reserved for confirmed invalid/abnormal sessions; transient failures remain
// enabled and recover automatically after error_until.
func (s *Store) SetCookieSessionState(id int64, status, reason string, disable bool) error {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		status = "active"
	}
	if disable {
		_, err := s.db.Exec(
			"UPDATE cookies SET session_status = ?, is_active = 0, disabled_reason = ?, disabled_at = ? WHERE id = ?",
			status, strings.ToUpper(status), nowTS(), id,
		)
		return err
	}
	_, err := s.db.Exec("UPDATE cookies SET session_status = ?, last_error = ? WHERE id = ?", status, reason, id)
	return err
}

// AutoDisableCookie disables a cookie with a structured reason.
func (s *Store) AutoDisableCookie(id int64, reason string) error {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(reason), " ", "_"))
	if normalized == "" {
		normalized = "AUTO_DISABLED"
	}
	if len(normalized) > 64 {
		normalized = normalized[:64]
	}
	_, err := s.db.Exec(
		"UPDATE cookies SET is_active = 0, disabled_reason = ?, disabled_at = ? WHERE id = ?",
		normalized, nowTS(), id,
	)
	return err
}

// MarkCookieUsed records a successful usage timestamp and clears errors.
func (s *Store) MarkCookieUsed(id int64) error {
	now := nowTS()
	_, err := s.db.Exec(
		"UPDATE cookies SET last_used_at = ?, last_checked_at = ?, last_error = '' WHERE id = ?",
		now, now, id,
	)
	return err
}

// MarkCookieError stores the latest failure reason.
func (s *Store) MarkCookieError(id int64, message string) error {
	if len(message) > 300 {
		message = message[:300]
	}
	_, err := s.db.Exec(
		"UPDATE cookies SET last_error = ?, last_checked_at = ? WHERE id = ?",
		message, nowTS(), id,
	)
	return err
}

// UpdateCookieProfile updates email + balance after a profile fetch.
func (s *Store) UpdateCookieProfile(id int64, accountID, email string, balance int64) error {
	accountID = strings.TrimSpace(accountID)
	var storedAccountID string
	err := s.db.QueryRow("SELECT account_id FROM cookies WHERE id = ? LIMIT 1", id).Scan(&storedAccountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("store: load cookie account identity: %w", err)
	}
	storedAccountID = strings.TrimSpace(storedAccountID)
	if storedAccountID != "" && accountID != "" && storedAccountID != accountID {
		return errors.New("Leonardo 浏览器会话已切换到另一个账号，已拒绝覆盖原账号身份")
	}
	if len(accountID) > 200 {
		accountID = accountID[:200]
	}
	if len(email) > 200 {
		email = email[:200]
	}
	_, err = s.db.Exec(
		"UPDATE cookies SET account_id = ?, email = ?, last_balance = ?, last_checked_at = ? WHERE id = ?",
		accountID, email, balance, nowTS(), id,
	)
	return err
}

func (s *Store) queryCookies(query string, args ...any) ([]Cookie, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query cookies: %w", err)
	}
	defer rows.Close()

	var (
		out  []Cookie
		cols []string
	)
	cols, err = rows.Columns()
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}

		var c Cookie
		for i, name := range cols {
			switch name {
			case "id":
				c.ID = toInt64(raw[i])
			case "value":
				c.Value = toString(raw[i])
			case "account_id":
				c.AccountID = toString(raw[i])
			case "is_active":
				c.IsActive = int(toInt64(raw[i]))
			case "session_status":
				c.SessionStatus = toString(raw[i])
			case "jwt_token":
				c.JWTToken = toString(raw[i])
			case "jwt_expires_at":
				c.JWTExpiresAt = toInt64(raw[i])
			case "refresh_fail_count":
				c.RefreshFails = int(toInt64(raw[i]))
			case "refresh_fail_reason":
				c.RefreshReason = toString(raw[i])
			case "error_until":
				c.ErrorUntil = toInt64(raw[i])
			case "last_refresh_at":
				c.LastRefreshAt = toInt64(raw[i])
			case "refresh_lease_until":
				c.RefreshLease = toInt64(raw[i])
			case "refresh_lease_owner":
				c.RefreshOwner = toString(raw[i])
			case "last_error":
				c.LastError = toString(raw[i])
			case "last_used_at":
				c.LastUsedAt = toInt64(raw[i])
			case "email":
				c.Email = toString(raw[i])
			case "last_balance":
				c.LastBalance = toInt64(raw[i])
			case "last_checked_at":
				c.LastCheckedAt = toInt64(raw[i])
			case "disabled_reason":
				c.DisabledReason = toString(raw[i])
			case "disabled_at":
				c.DisabledAt = toInt64(raw[i])
			case "created_at":
				c.CreatedAt = toInt64(raw[i])
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func toInt64(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case []byte:
		var n int64
		fmt.Sscanf(string(t), "%d", &n)
		return n
	case string:
		var n int64
		fmt.Sscanf(t, "%d", &n)
		return n
	}
	return 0
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	}
	return ""
}
