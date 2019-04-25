// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package dcrpg

import (
	"database/sql"
	"fmt"

	"github.com/decred/dcrdata/db/dcrpg/internal"
)

// The database schema is versioned in the meta table as follows.
const (
	// compatVersion indicates major DB changes for which there are no automated
	// upgrades. A complete DB rebuild is required if this version changes. This
	// should change very rarely, but when it does change all of the upgrades
	// defined here should be removed since they are no longer applicable.
	compatVersion = 1

	// schemaVersion pertains to a sequence of incremental upgrades to the
	// database schema thay may be performed for the same compatibility version.
	// This includes changes such as creating tables, adding/deleting columns,
	// adding/deleting indexes or any other operations that create, delete, or
	// modify the definition of any database relation.
	schemaVersion = 1

	// maintVersion indicates when certain maintenance operations should be
	// performed for the same compatVersion and schemaVersion. Such operations
	// include duplicate row check and removal, forced table analysis, patching
	// or recomputation of data values, reindexing, or any other operations that
	// do not create, delete or modify the definition of any database relation.
	maintVersion = 0
)

var (
	targetDatabaseVersion = &DatabaseVersion{
		compat: compatVersion,
		schema: schemaVersion,
		maint:  maintVersion,
	}

	legacyDatabaseVersion = &DatabaseVersion{compatVersion, 0, 0}
)

// DatabaseVersion models a database version.
type DatabaseVersion struct {
	compat, schema, maint uint32
}

// String implements Stringer for DatabaseVersion.
func (v DatabaseVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.compat, v.schema, v.maint)
}

// NewDatabaseVersion returns a new DatabaseVersion with the version major.minor.patch
func NewDatabaseVersion(major, minor, patch uint32) DatabaseVersion {
	return DatabaseVersion{major, minor, patch}
}

// DBVersion retrieves the database version from the meta table. See
// (*DatabaseVersion).NeededToReach for version comparison.
func DBVersion(db *sql.DB) (ver DatabaseVersion, err error) {
	err = db.QueryRow(internal.SelectMetaDBVersions).Scan(&ver.compat, &ver.schema, &ver.maint)
	return
}

// CompatAction defines the action to be taken once the current and the required
// pg table versions are compared.
type CompatAction int8

const (
	Rebuild CompatAction = iota
	Upgrade
	Maintenance
	OK
	TimeTravel
	Unknown
)

// NeededToReach describes what action is required for the DatabaseVersion to
// reach another version provided in the input argument.
func (v *DatabaseVersion) NeededToReach(other *DatabaseVersion) CompatAction {
	switch {
	case v.compat < other.compat:
		return Rebuild
	case v.compat < other.compat:
		return TimeTravel
	case v.schema < other.schema:
		return Upgrade
	case v.schema > other.schema:
		return TimeTravel
	case v.maint < other.maint:
		return Maintenance
	case v.maint > other.maint:
		return TimeTravel
	default:
		return OK
	}
}

// String implements Stringer for CompatAction.
func (v CompatAction) String() string {
	actions := map[CompatAction]string{
		Rebuild:     "rebuild",
		Upgrade:     "upgrade",
		Maintenance: "maintenance",
		TimeTravel:  "time travel",
		OK:          "ok",
	}
	if actionStr, ok := actions[v]; ok {
		return actionStr
	}
	return "unknown"
}

// DatabaseUpgrade is used to define a required DB upgrade.
type DatabaseUpgrade struct {
	TableName               string
	UpgradeType             CompatAction
	CurrentVer, RequiredVer DatabaseVersion
}

// String implements Stringer for DatabaseUpgrade.
func (s DatabaseUpgrade) String() string {
	return fmt.Sprintf("Table %s requires %s (%s -> %s).", s.TableName,
		s.UpgradeType, s.CurrentVer, s.RequiredVer)
}

type metaData struct {
	netName         string
	currencyNet     uint32
	bestBlockHeight int64
	bestBlockHash   string
	dbVer           DatabaseVersion
	ibdComplete     bool
}

func insertMetaData(db *sql.DB, meta *metaData) error {
	_, err := db.Exec(internal.InsertMetaRow, meta.netName, meta.currencyNet,
		meta.bestBlockHeight, meta.bestBlockHash,
		meta.dbVer.compat, meta.dbVer.schema, meta.dbVer.maint,
		meta.ibdComplete)
	return err
}

func updateSchemaVersion(db *sql.DB, schema uint32) error {
	_, err := db.Exec(internal.SetDBSchemaVersion, schema)
	return err
}

func UpgradeDatabase(db *sql.DB, bg BlockGetter) (bool, error) {
	initVer, upgradeType, err := versionCheck(db)
	if err != nil {
		return false, err
	}

	switch upgradeType {
	case OK:
		return true, nil
	case Upgrade, Maintenance:
		// Automatic upgrade is supported. Attempt to upgrade from initVer ->
		// targetDatabaseVersion.
		return upgradeDatabase(db, *initVer, *targetDatabaseVersion, bg)
	case TimeTravel:
		return false, fmt.Errorf("the current table version is newer than supported: "+
			"%v > %v", initVer, targetDatabaseVersion)
	case Unknown, Rebuild:
		fallthrough
	default:
		return false, fmt.Errorf("rebuild of entire database required")
	}
}

func upgradeDatabase(db *sql.DB, current, target DatabaseVersion, bg BlockGetter) (bool, error) {
	switch current.compat {
	case 1:
		return compatVersion1Upgrades(db, current, target, bg)
	default:
		return false, fmt.Errorf("unsupported DB compatibility version %d", current.compat)
	}
}

func compatVersion1Upgrades(db *sql.DB, current, target DatabaseVersion, _ BlockGetter) (bool, error) {
	upgradeCheck := func() (done bool, err error) {
		switch current.NeededToReach(&target) {
		case OK:
			// No upgrade needed.
			return true, nil
		case Upgrade, Maintenance:
			// Automatic upgrade is supported.
			return false, nil
		case TimeTravel:
			return false, fmt.Errorf("the current table version is newer than supported: "+
				"%v > %v", current, target)
		case Unknown, Rebuild:
			fallthrough
		default:
			return false, fmt.Errorf("rebuild of entire database required")
		}
	}

	// Initial upgrade status check.
	done, err := upgradeCheck()
	if done || err != nil {
		return done, err
	}

	// Process schema upgrades and table maintenance.
	switch current.schema {
	case 0: // legacyDatabaseVersion
		// Remove table comments where the versions were stored.
		removeTableComments(db)

		// Bump schema version.
		current.schema++
		if err = updateSchemaVersion(db, current.schema); err != nil {
			return false, fmt.Errorf("failed to update schema version: %v", err)
		}

		// Continue to upgrades for the next schema version.
		fallthrough
	case 1:
		// Perform schema v1 maintenance.
		// --> noop, but would switch on current.maint

		// Perform upgrade to schema v2.
		// --> schema v2 not defined yet.

		// No further upgrades.
		return upgradeCheck()
		// Or continue to upgrades for the next schema version.
		// fallthrough
	default:
		return false, fmt.Errorf("unsupported schema version %d", current.schema)
	}
}

func removeTableComments(db *sql.DB) {
	for tableName := range createTableStatements {
		_, err := db.Exec(fmt.Sprintf(`COMMENT ON table %s IS NULL;`, tableName))
		if err != nil {
			log.Errorf(`Failed to remove comment on table %s.`, tableName)
		}
	}
}
