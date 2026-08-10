package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openCookieTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestUpsertCookieAccountRefreshesSameLeonardoAccount(t *testing.T) {
	st := openCookieTestStore(t)

	first, err := st.UpsertCookieAccount("cookie=session-one", "account-1", "old@example.com", 100)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if first.UpdatedExisting || first.ID == 0 {
		t.Fatalf("first result = %+v", first)
	}

	second, err := st.UpsertCookieAccount("cookie=session-two", "account-1", "new@example.com", 90)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if !second.UpdatedExisting || second.ID != first.ID {
		t.Fatalf("second result = %+v, first = %+v", second, first)
	}

	rows, err := st.ListCookies()
	if err != nil {
		t.Fatalf("ListCookies: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("cookie count = %d, want 1", len(rows))
	}
	if rows[0].Value != "cookie=session-two" || rows[0].AccountID != "account-1" || rows[0].Email != "new@example.com" {
		t.Fatalf("stored cookie = %+v", rows[0])
	}
}

func TestMergeDuplicateCookieAccountsPreservesGenerationReferences(t *testing.T) {
	st := openCookieTestStore(t)
	if err := st.AddCookie("cookie=older"); err != nil {
		t.Fatalf("AddCookie older: %v", err)
	}
	if err := st.AddCookie("cookie=newer"); err != nil {
		t.Fatalf("AddCookie newer: %v", err)
	}
	older, _ := st.GetCookieByValue("cookie=older")
	newer, _ := st.GetCookieByValue("cookie=newer")
	if older == nil || newer == nil {
		t.Fatal("failed to load inserted cookies")
	}
	if err := st.UpdateCookieProfile(older.ID, "account-1", "same@example.com", 10); err != nil {
		t.Fatalf("UpdateCookieProfile older: %v", err)
	}
	if err := st.UpdateCookieProfile(newer.ID, "account-1", "same@example.com", 20); err != nil {
		t.Fatalf("UpdateCookieProfile newer: %v", err)
	}
	if _, err := st.db.Exec("UPDATE cookies SET last_checked_at = 100 WHERE id = ?", older.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec("UPDATE cookies SET last_checked_at = 200 WHERE id = ?", newer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(
		"INSERT INTO generation_logs (used_cookie_id, status, created_at) VALUES (?, 'success', 1)",
		older.ID,
	); err != nil {
		t.Fatalf("insert generation log: %v", err)
	}

	merged, err := st.MergeDuplicateCookieAccounts()
	if err != nil {
		t.Fatalf("MergeDuplicateCookieAccounts: %v", err)
	}
	if merged != 1 {
		t.Fatalf("merged = %d, want 1", merged)
	}
	rows, err := st.ListCookies()
	if err != nil || len(rows) != 1 {
		t.Fatalf("cookies after merge = %+v, err=%v", rows, err)
	}
	if rows[0].ID != newer.ID {
		t.Fatalf("kept id = %d, want newest checked id %d", rows[0].ID, newer.ID)
	}
	var usedCookieID int64
	if err := st.db.QueryRow("SELECT used_cookie_id FROM generation_logs LIMIT 1").Scan(&usedCookieID); err != nil {
		t.Fatalf("read generation log: %v", err)
	}
	if usedCookieID != newer.ID {
		t.Fatalf("generation log cookie id = %d, want %d", usedCookieID, newer.ID)
	}
}

func TestMergeDuplicateCookieAccountsDoesNotMergeDifferentIDsWithSameEmail(t *testing.T) {
	st := openCookieTestStore(t)
	if _, err := st.UpsertCookieAccount("cookie=account-one", "account-1", "shared@example.com", 10); err != nil {
		t.Fatalf("upsert account 1: %v", err)
	}
	if _, err := st.UpsertCookieAccount("cookie=account-two", "account-2", "shared@example.com", 20); err != nil {
		t.Fatalf("upsert account 2: %v", err)
	}

	merged, err := st.MergeDuplicateCookieAccounts()
	if err != nil {
		t.Fatalf("MergeDuplicateCookieAccounts: %v", err)
	}
	if merged != 0 {
		t.Fatalf("merged = %d, want 0", merged)
	}
	rows, err := st.ListCookies()
	if err != nil || len(rows) != 2 {
		t.Fatalf("cookies = %+v, err=%v", rows, err)
	}
}

func TestUpsertCookieAccountRejectsSharedSessionAcrossDifferentAccounts(t *testing.T) {
	st := openCookieTestStore(t)
	first, err := st.UpsertCookieAccount("cookie=shared-browser-session", "account-1", "one@example.com", 10)
	if err != nil {
		t.Fatalf("upsert account 1: %v", err)
	}
	if _, err := st.UpsertCookieAccount("cookie=shared-browser-session", "account-2", "two@example.com", 20); err == nil {
		t.Fatal("expected shared browser session conflict")
	}

	rows, err := st.ListCookies()
	if err != nil || len(rows) != 1 {
		t.Fatalf("cookies = %+v, err=%v", rows, err)
	}
	if rows[0].ID != first.ID || rows[0].AccountID != "account-1" || rows[0].Email != "one@example.com" {
		t.Fatalf("original account was overwritten: %+v", rows[0])
	}
}

func TestListActiveCookiesRotatesLeastRecentlyUsedAccounts(t *testing.T) {
	st := openCookieTestStore(t)
	first, err := st.UpsertCookieAccount("cookie=one", "account-1", "one@example.com", 10)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.UpsertCookieAccount("cookie=two", "account-2", "two@example.com", 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec("UPDATE cookies SET last_used_at = 100 WHERE id = ?", first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec("UPDATE cookies SET last_used_at = 200 WHERE id = ?", second.ID); err != nil {
		t.Fatal(err)
	}

	rows, err := st.ListActiveCookies()
	if err != nil || len(rows) != 2 || rows[0].ID != first.ID {
		t.Fatalf("initial rotation order = %+v, err=%v", rows, err)
	}
	if err := st.MarkCookieUsed(first.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = st.ListActiveCookies()
	if err != nil || len(rows) != 2 || rows[0].ID != second.ID {
		t.Fatalf("rotation order after use = %+v, err=%v", rows, err)
	}
}

func TestUpdateCookieProfileRejectsAccountIdentityDrift(t *testing.T) {
	st := openCookieTestStore(t)
	inserted, err := st.UpsertCookieAccount("cookie=stable", "account-1", "one@example.com", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateCookieProfile(inserted.ID, "account-2", "two@example.com", 20); err == nil {
		t.Fatal("expected account identity drift error")
	}
	row, err := st.GetCookieByID(inserted.ID)
	if err != nil || row == nil {
		t.Fatalf("GetCookieByID: row=%+v err=%v", row, err)
	}
	if row.AccountID != "account-1" || row.Email != "one@example.com" || row.LastBalance != 10 {
		t.Fatalf("account identity was overwritten: %+v", row)
	}
}

func TestCookieSessionStatePersistsJWTBackoffAndRecovery(t *testing.T) {
	st := openCookieTestStore(t)
	if err := st.AddCookie("__Secure-better-auth.session_token=old"); err != nil {
		t.Fatal(err)
	}
	row, err := st.GetCookieByValue("__Secure-better-auth.session_token=old")
	if err != nil || row == nil {
		t.Fatalf("load cookie: row=%v err=%v", row, err)
	}

	if err := st.UpdateCookieSessionSuccess(row.ID, "header.payload.signature", 123456); err != nil {
		t.Fatal(err)
	}
	leaseUntil := time.Now().Add(time.Minute).Unix()
	acquired, err := st.TryAcquireCookieRefreshLease(row.ID, "worker-a", leaseUntil)
	if err != nil || !acquired {
		t.Fatalf("first refresh lease acquired=%v err=%v", acquired, err)
	}
	acquired, err = st.TryAcquireCookieRefreshLease(row.ID, "worker-b", leaseUntil)
	if err != nil || acquired {
		t.Fatalf("second refresh lease acquired=%v err=%v", acquired, err)
	}
	if err := st.ReleaseCookieRefreshLease(row.ID, "worker-b"); err != nil {
		t.Fatal(err)
	}
	acquired, err = st.TryAcquireCookieRefreshLease(row.ID, "worker-b", leaseUntil)
	if err != nil || acquired {
		t.Fatalf("wrong owner released lease: acquired=%v err=%v", acquired, err)
	}
	if err := st.ReleaseCookieRefreshLease(row.ID, "worker-a"); err != nil {
		t.Fatal(err)
	}
	acquired, err = st.TryAcquireCookieRefreshLease(row.ID, "worker-b", leaseUntil)
	if err != nil || !acquired {
		t.Fatalf("released lease was not reusable: acquired=%v err=%v", acquired, err)
	}
	_ = st.ReleaseCookieRefreshLease(row.ID, "worker-b")
	count, err := st.RecordCookieRefreshFailure(row.ID, "rate limited", 999999)
	if err != nil || count != 1 {
		t.Fatalf("first refresh failure count=%d err=%v", count, err)
	}
	count, err = st.RecordCookieRefreshFailure(row.ID, "rate limited", 999999)
	if err != nil || count != 2 {
		t.Fatalf("second refresh failure count=%d err=%v", count, err)
	}
	count, err = st.RecordCookieRefreshFailure(row.ID, "timeout", 999999)
	if err != nil || count != 1 {
		t.Fatalf("changed reason must reset count: count=%d err=%v", count, err)
	}
	if err := st.SetCookieSessionState(row.ID, "temporary_unavailable", "timeout", false); err != nil {
		t.Fatal(err)
	}
	row, _ = st.GetCookieByID(row.ID)
	if row.SessionStatus != "temporary_unavailable" || row.IsActive != 1 || row.RefreshFails != 1 {
		t.Fatalf("temporary state = %+v", row)
	}

	if err := st.SetCookieSessionState(row.ID, "invalid", "unauthorized", true); err != nil {
		t.Fatal(err)
	}
	row, _ = st.GetCookieByID(row.ID)
	if row.SessionStatus != "invalid" || row.IsActive != 0 || row.DisabledReason != "INVALID" {
		t.Fatalf("invalid state = %+v", row)
	}
	if err := st.UpdateCookieSessionSuccess(row.ID, "new.jwt.token", 654321); err != nil {
		t.Fatal(err)
	}
	row, _ = st.GetCookieByID(row.ID)
	if row.SessionStatus != "active" || row.IsActive != 1 || row.RefreshFails != 0 || row.DisabledReason != "" || row.JWTToken != "new.jwt.token" {
		t.Fatalf("recovered state = %+v", row)
	}
}
