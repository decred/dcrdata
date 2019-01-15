package dcrpg

import (
	"testing"
)

func TestMakeDeleteColumnsStmt(t *testing.T) {
	table := "blocks"
	cols := []string{"extra_data", "final_state"}
	expected := "ALTER TABLE blocks DROP COLUMN IF EXISTS extra_data, DROP COLUMN IF EXISTS final_state;"

	dropStmt := makeDeleteColumnsStmt(table, cols)
	if dropStmt != expected {
		t.Errorf("expected %s, got %s", expected, dropStmt)
	}
}
