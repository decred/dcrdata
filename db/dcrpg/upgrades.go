// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package dcrpg

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrdata/db/dcrpg/v8/internal"
	"github.com/decred/dcrdata/v8/stakedb"
)

// The database schema is versioned in the meta table as follows.
const (
	// compatVersion indicates major DB changes for which there are no automated
	// upgrades. A complete DB rebuild is required if this version changes. This
	// should change very rarely, but when it does change all of the upgrades
	// defined here should be removed since they are no longer applicable.
	compatVersion = 2

	// schemaVersion pertains to a sequence of incremental upgrades to the
	// database schema that may be performed for the same compatibility version.
	// This includes changes such as creating tables, adding/deleting columns,
	// adding/deleting indexes or any other operations that create, delete, or
	// modify the definition of any database relation.
	schemaVersion = 0

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

// These are the recognized CompatActions for upgrading a database from one
// version to another.
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
	case v.compat > other.compat:
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
	// bestBlockHash   dbtypes.ChainHash
	dbVer DatabaseVersion
	// ibdComplete bool
}

func initMetaData(db *sql.DB, meta *metaData) error {
	_, err := db.Exec(internal.InitMetaRow, meta.netName, meta.currencyNet,
		meta.bestBlockHeight, // meta.bestBlockHash,
		meta.dbVer.compat, meta.dbVer.schema, meta.dbVer.maint,
		false /* meta.ibdComplete */)
	return err
}

func updateSchemaVersion(db *sql.DB, schema uint32) error { //nolint:unused
	_, err := db.Exec(internal.SetDBSchemaVersion, schema)
	return err
}

func updateMaintVersion(db *sql.DB, maint uint32) error { //nolint:unused
	_, err := db.Exec(internal.SetDBMaintenanceVersion, maint)
	return err
}

// Upgrader contains a number of elements necessary to perform a database
// upgrade.
type Upgrader struct {
	db      *sql.DB
	params  *chaincfg.Params
	bg      BlockGetter
	stakeDB *stakedb.StakeDatabase
	ctx     context.Context
}

// NewUpgrader is a contructor for an Upgrader.
func NewUpgrader(ctx context.Context, params *chaincfg.Params, db *sql.DB, bg BlockGetter, stakeDB *stakedb.StakeDatabase) *Upgrader {
	return &Upgrader{
		db:      db,
		params:  params,
		bg:      bg,
		stakeDB: stakeDB,
		ctx:     ctx,
	}
}

// UpgradeDatabase attempts to upgrade the given sql.DB with help from the
// BlockGetter. The DB version will be compared against the target version to
// decide what upgrade type to initiate.
func (u *Upgrader) UpgradeDatabase() (bool, error) {
	initVer, upgradeType, err := versionCheck(u.db)
	if err != nil {
		return false, err
	}

	switch upgradeType {
	case OK:
		return true, nil
	case Upgrade, Maintenance:
		// Automatic upgrade is supported. Attempt to upgrade from initVer ->
		// targetDatabaseVersion.
		return u.upgradeDatabase(*initVer, *targetDatabaseVersion)
	case TimeTravel:
		return false, fmt.Errorf("the current table version is newer than supported: "+
			"%v > %v", initVer, targetDatabaseVersion)
	case Unknown, Rebuild:
		fallthrough
	default:
		return false, fmt.Errorf("rebuild of entire database required")
	}
}

func (u *Upgrader) upgradeDatabase(current, target DatabaseVersion) (bool, error) {
	switch current.compat {
	case 2:
		return u.compatVersion2Upgrades(current, target)
	default:
		return false, fmt.Errorf("unsupported DB compatibility version %d", current.compat)
	}
}

func (u *Upgrader) compatVersion2Upgrades(current, target DatabaseVersion) (bool, error) {
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
	// initSchema := current.schema
	switch current.schema {
	case 0:
		return true, nil // nothing to do

	/* when there's an upgrade to define:
	case 0:
		// Remove table comments where the versions were stored.
		log.Infof("Performing database upgrade 2.0.0 -> 2.1.0")

		// removeTableComments(u.db) // do something here

		err = u.upgradeSchema7to8()
		if err != nil {
			return false, fmt.Errorf("failed to upgrade 1.7.0 to 1.8.0: %v", err)
		}
		current.schema++
		current.maint = 0
		if err = storeVers(u.db, &current); err != nil {
			return false, err
		}

		fallthrough

	case 1:
		// Perform schema v11 maintenance.

		// No further upgrades.
		return upgradeCheck()

		// Or continue to upgrades for the next schema version.
		// fallthrough
	*/

	default:
		return false, fmt.Errorf("unsupported schema version %d", current.schema)
	}
}

func storeVers(db *sql.DB, dbVer *DatabaseVersion) error { //nolint:unused
	err := updateSchemaVersion(db, dbVer.schema)
	if err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}
	err = updateMaintVersion(db, dbVer.maint)
	return fmt.Errorf("failed to update maintenance version: %w", err)
}

/* define when needed
func (u *Upgrader) upgradeSchema-to1() error {
	log.Infof("Performing database upgrade 2.0.0 -> 2.1.0")
	// describe the actions...
	return whatever(u.db)
}
*/
