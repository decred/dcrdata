// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package dcrpg

import (
	"context"
	"database/sql"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrdata/db/dcrpg/v4/internal"
	"github.com/decred/dcrdata/stakedb/v3"
	"github.com/lib/pq"
)

// The database schema is versioned in the meta table as follows.
const (
	// compatVersion indicates major DB changes for which there are no automated
	// upgrades. A complete DB rebuild is required if this version changes. This
	// should change very rarely, but when it does change all of the upgrades
	// defined here should be removed since they are no longer applicable.
	compatVersion = 1

	// schemaVersion pertains to a sequence of incremental upgrades to the
	// database schema that may be performed for the same compatibility version.
	// This includes changes such as creating tables, adding/deleting columns,
	// adding/deleting indexes or any other operations that create, delete, or
	// modify the definition of any database relation.
	schemaVersion = 3

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

// Upgrader contains a number of elements necessary to perform a database
// upgrade.
type Upgrader struct {
	db      *sql.DB
	bg      BlockGetter
	stakeDB *stakedb.StakeDatabase
	ctx     context.Context
}

// NewUpgrader is a contructor for an Upgrader.
func NewUpgrader(ctx context.Context, db *sql.DB, bg BlockGetter, stakeDB *stakedb.StakeDatabase) *Upgrader {
	return &Upgrader{
		db:      db,
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
	case 1:
		return u.compatVersion1Upgrades(current, target)
	default:
		return false, fmt.Errorf("unsupported DB compatibility version %d", current.compat)
	}
}

func (u *Upgrader) compatVersion1Upgrades(current, target DatabaseVersion) (bool, error) {
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
		removeTableComments(u.db)

		// Bump schema version.
		current.schema++
		if err = updateSchemaVersion(u.db, current.schema); err != nil {
			return false, fmt.Errorf("failed to update schema version: %v", err)
		}

		// Continue to upgrades for the next schema version.
		fallthrough
	case 1:
		// Upgrade to schema v2.
		err = u.upgrade110to120()
		if err != nil {
			return false, fmt.Errorf("failed to upgrade 1.1.0 to 1.2.0: %v", err)
		}
		current.schema++
		if err = updateSchemaVersion(u.db, current.schema); err != nil {
			return false, fmt.Errorf("failed to update schema version: %v", err)
		}
		fallthrough
	case 2:
		// Upgrade to schema v3.
		err = u.upgrade120to130()
		if err != nil {
			return false, fmt.Errorf("failed to upgrade 1.2.0 to 1.3.0: %v", err)
		}
		current.schema++
		if err = updateSchemaVersion(u.db, current.schema); err != nil {
			return false, fmt.Errorf("failed to update schema version: %v", err)
		}
		fallthrough

	case 3:
		// Upgrade to schema v4.
		err = u.upgrade130to140()
		if err != nil {
			return false, fmt.Errorf("failed to upgrade 1.3.0 to 1.4.0: %v", err)
		}
		current.schema++
		if err = updateSchemaVersion(u.db, current.schema); err != nil {
			return false, fmt.Errorf("failed to update schema version: %v", err)
		}
		fallthrough

	case 4:
		// Perform schema v4 maintenance.
		// --> noop, but would switch on current.maint

		// No further upgrades.
		return upgradeCheck()
		// Or continue to upgrades for the next schema version.
		// fallthrough
	default:
		return false, fmt.Errorf("unsupported schema version %d", current.schema)
	}
}

func removeTableComments(db *sql.DB) {
	for _, pair := range createTableStatements {
		tableName := pair[0]
		_, err := db.Exec(fmt.Sprintf(`COMMENT ON table %s IS NULL;`, tableName))
		if err != nil {
			log.Errorf(`Failed to remove comment on table %s.`, tableName)
		}
	}
}

// This changes the data type of votes.version from INT2 to INT4.
func (u *Upgrader) upgrade130to140() error {
	// Create the stats table and height index.
	log.Infof("Performing database upgrade 1.3.0 -> 1.4.0")
	_, err := u.db.Exec(`ALTER TABLE votes ALTER COLUMN version TYPE INT4`)
	return err
}

// This indexes the blocks table on the "time" column.
func (u *Upgrader) upgrade120to130() error {
	// Create the stats table and height index.
	log.Infof("Performing database upgrade 1.2.0 -> 1.3.0")

	existsIdx, err := ExistsIndex(u.db, internal.IndexBlocksTableOnTime)
	if err != nil {
		return err
	}
	if existsIdx {
		log.Warnf("The index %s already exists!", internal.IndexOfBlocksTableOnTime)
		return nil
	}

	return IndexBlockTableOnTime(u.db)
}

// This upgrade creates a stats table and adds a winners row to the blocks table
// necessary to replace information from the sqlite database, which is being
// dropped. As part of the upgrade, the entire blockchain must be requested and
// the ticket pool evolved appropriately.
func (u *Upgrader) upgrade110to120() error {
	// Create the stats table and height index.
	log.Infof("Performing database upgrade 1.1.0 -> 1.2.0")
	exists, err := TableExists(u.db, "stats")
	if err != nil {
		return err
	}
	if !exists {
		_, err = u.db.Exec(internal.CreateStatsTable)
		if err != nil {
			return fmt.Errorf("CreateStatsTable: %v", err)
		}
		_, err = u.db.Exec(internal.IndexStatsOnHeight)
		if err != nil {
			return fmt.Errorf("IndexStatsOnHeight: %v", err)
		}
		_, err = u.db.Exec(`ALTER TABLE blocks ADD COLUMN IF NOT EXISTS winners TEXT[];`)
		if err != nil {
			return fmt.Errorf("Add winners column error: %v", err)
		}
	}
	// Do everything else under a transaction.
	dbTx, err := u.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to create db transaction: %v", err)
	}
	defer func() {
		if err == nil {
			dbTx.Commit()
		} else {
			dbTx.Rollback()
		}
	}()
	makeErr := func(s string, args ...interface{}) error {
		err = fmt.Errorf(s, args...)
		return err
	}
	// Start with a height-ordered list of block data.
	blockRows, err := u.db.Query(`
		SELECT id, hash, height
		FROM blocks
		WHERE is_mainchain
		ORDER BY height
	;`)
	if err != nil {
		return makeErr("block hash query error: %v", err)
	}
	defer blockRows.Close()
	// Set the stake database to the genesis block.
	dir, err := ioutil.TempDir("", "tempstake")
	if err != nil {
		return makeErr("unable to create temp directory")
	}
	defer os.RemoveAll(dir)
	sDB, _, err := u.stakeDB.EmptyCopy(dir)
	if err != nil {
		return makeErr("stake db init error: %v", err)
	}
	// Two prepared statements.
	statsStmt, err := dbTx.Prepare(internal.UpsertStats)
	if err != nil {
		return makeErr("failed to prepare stats insert statement: %v", err)
	}
	// sql does not deal with PostgreSQL array syntax, it must be Sprintf'd.
	winnersStmt, err := dbTx.Prepare("UPDATE blocks SET winners = $1 where hash = $2;")
	if err != nil {
		return makeErr("failed to prepare winners insert statement: %v", err)
	}

	checkHeight := 0
	var hashStr string
	var id, height int
	for blockRows.Next() {
		if u.ctx.Err() != nil {
			return makeErr("context cancelled. rolling back update")
		}
		blockRows.Scan(&id, &hashStr, &height)
		hash, err := chainhash.NewHashFromStr(hashStr)
		if err != nil {
			return makeErr("NewHashFromStr: %v", err)
		}
		// If the height is not the expected height, the database must be corrupted.
		if height != checkHeight {
			return makeErr("height mismatch %d != %d. database corrupted!", height, checkHeight)
		}
		checkHeight += 1
		// A periodic update messaage.
		if height%10000 == 0 {
			log.Infof("Processing blocks %d - %d", height, height+9999)
		}
		// Connecting the block updates the live ticket cache and ticket info cache.
		// The StakeDatabase is pre-populated with the genesis block, so skip it.
		if height > 0 {
			_, err = sDB.ConnectBlockHash(hash)
			if err != nil {
				return makeErr("ConnectBlockHash: %v", err)
			}
		}

		// The "best" pool info is for the chain at the tip just added.
		poolInfo := sDB.PoolInfoBest()
		if poolInfo == nil {
			return makeErr("PoolInfoBest error encountered")
		}
		// Insert rows.
		_, err = statsStmt.Exec(id, height, poolInfo.Size, int64(poolInfo.Value*dcrToAtoms))
		if err != nil {
			return makeErr("insert Exec: %v", err)
		}
		_, err = winnersStmt.Exec(pq.Array(poolInfo.Winners), hashStr)
		if err != nil {
			return makeErr("update Exec: %v", err)
		}
	}
	return nil
}
