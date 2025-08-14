//go:build pgonline

package dcrpg

import (
	"testing"
)

func Test_retrieveSysSettingsPerformance(t *testing.T) {
	ss, err := retrieveSysSettingsPerformance(db.db)
	if err != nil {
		t.Errorf("Failed to retrieve system settings: %v", err)
	}
	t.Logf("\n%v", ss)
}

func Test_retrieveSysSettingsServer(t *testing.T) {
	ss, err := retrieveSysSettingsServer(db.db)
	if err != nil {
		t.Errorf("Failed to retrieve system server: %v", err)
	}
	t.Logf("\n%v", ss)
}

func Test_retrievePGVersion(t *testing.T) {
	ver, verNum, err := retrievePGVersion(db.db)
	if err != nil {
		t.Errorf("Failed to retrieve postgres version: %v", err)
	}
	t.Logf("\n%d: %s", verNum, ver)
}
