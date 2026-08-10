package store

import (
	"database/sql"
	"os"
)

// PreferredDBPath chooses the renamed database unless it only contains an
// empty bootstrap schema and the legacy LeoStudio database still contains the
// user's accounts, models, or material history.
func PreferredDBPath(current, legacy string) string {
	currentExists := fileExists(current)
	legacyExists := fileExists(legacy)
	if currentExists {
		if hasData, err := DatabaseHasUserData(current); err == nil && hasData {
			return current
		}
	}
	if legacyExists {
		if hasData, err := DatabaseHasUserData(legacy); err == nil && hasData {
			return legacy
		}
	}
	if currentExists || !legacyExists {
		return current
	}
	return legacy
}

// DatabaseHasUserData inspects only row counts and never reads cookie values.
func DatabaseHasUserData(path string) (bool, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return false, err
	}
	defer db.Close()
	for _, table := range []string{"cookies", "generation_logs", "models"} {
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
		if err != nil {
			// Old/partial databases may not contain every table yet.
			continue
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
