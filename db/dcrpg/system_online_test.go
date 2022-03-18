//go:build pgonline

package dcrpg

import (
	"database/sql"
	"testing"
)

func TestRetrieveSysSettingsConfFile(t *testing.T) {
	ss, err := RetrieveSysSettingsConfFile(db.db)
	if err != nil && err != sql.ErrNoRows {
		t.Errorf("Failed to retrieve system settings: %v", err)
	}
	t.Logf("\n%v", ss)
}

func TestRetrieveSysSettingsPerformance(t *testing.T) {
	ss, err := RetrieveSysSettingsPerformance(db.db)
	if err != nil {
		t.Errorf("Failed to retrieve system settings: %v", err)
	}
	t.Logf("\n%v", ss)
}

func TestRetrieveSysSettingsServer(t *testing.T) {
	ss, err := RetrieveSysSettingsServer(db.db)
	if err != nil {
		t.Errorf("Failed to retrieve system server: %v", err)
	}
	t.Logf("\n%v", ss)
}

func TestRetrievePGVersion(t *testing.T) {
	ver, verNum, err := RetrievePGVersion(db.db)
	if err != nil {
		t.Errorf("Failed to retrieve postgres version: %v", err)
	}
	t.Logf("\n%d: %s", verNum, ver)
}
