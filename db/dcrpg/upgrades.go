// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package dcrpg

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/db/dcrpg/v8/internal"
	"github.com/decred/dcrdata/v8/stakedb"
	"github.com/decred/dcrdata/v8/txhelpers"
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
	schemaVersion = 11

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

func updateMaintVersion(db *sql.DB, maint uint32) error {
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
	initSchema := current.schema
	switch current.schema {
	case 0: // legacyDatabaseVersion
		// Remove table comments where the versions were stored.
		log.Infof("Performing database upgrade 1.0.0 -> 1.1.0")
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
		err = u.upgradeSchema1to2()
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
		err = u.upgradeSchema2to3()
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
		err = u.upgradeSchema3to4()
		if err != nil {
			return false, fmt.Errorf("failed to upgrade 1.3.0 to 1.4.0: %v", err)
		}
		current.schema++
		if err = updateSchemaVersion(u.db, current.schema); err != nil {
			return false, fmt.Errorf("failed to update schema version: %v", err)
		}
		fallthrough

	case 4:
		// Upgrade to schema v5.
		err = u.upgradeSchema4to5()
		if err != nil {
			return false, fmt.Errorf("failed to upgrade 1.4.0 to 1.5.0: %v", err)
		}
		current.schema++
		if err = updateSchemaVersion(u.db, current.schema); err != nil {
			return false, fmt.Errorf("failed to update schema version: %v", err)
		}
		fallthrough

	case 5:
		// Perform schema v5 maintenance.
		switch current.maint {
		case 0:
			// The maint 0 -> 1 upgrade is only needed if the user had upgraded
			// to 1.5.0 before 1.5.1 was defined.
			log.Infof("Performing database upgrade 1.5.0 -> 1.5.1")
			if initSchema == 5 {
				err = u.setTxMixData()
				if err != nil {
					return false, fmt.Errorf("failed to upgrade 1.5.0 to 1.5.1: %v", err)
				}
			}
			current.maint++
			if err = updateMaintVersion(u.db, current.maint); err != nil {
				return false, fmt.Errorf("failed to update maintenance version: %v", err)
			}
			fallthrough
		case 1:
			// all ready
		default:
			return false, fmt.Errorf("unsupported maint version %d", current.maint)
		}

		// Upgrade to schema v6.
		err = u.upgradeSchema5to6()
		if err != nil {
			return false, fmt.Errorf("failed to upgrade 1.5.1 to 1.6.0: %v", err)
		}
		current.schema++
		current.maint = 0
		if storeVers(u.db, &current); err != nil {
			return false, err
		}

		fallthrough

	case 6:
		err = u.upgradeSchema6to7()
		if err != nil {
			return false, fmt.Errorf("failed to upgrade 1.6.0 to 1.7.0: %v", err)
		}
		current.schema++
		current.maint = 0
		if storeVers(u.db, &current); err != nil {
			return false, err
		}

		fallthrough

	case 7:
		err = u.upgradeSchema7to8()
		if err != nil {
			return false, fmt.Errorf("failed to upgrade 1.7.0 to 1.8.0: %v", err)
		}
		current.schema++
		current.maint = 0
		if storeVers(u.db, &current); err != nil {
			return false, err
		}

		fallthrough

	case 8:
		err = u.upgradeSchema8to9()
		if err != nil {
			return false, fmt.Errorf("failed to upgrade 1.8.0 to 1.9.0: %v", err)
		}
		current.schema++
		current.maint = 0
		if storeVers(u.db, &current); err != nil {
			return false, err
		}

		fallthrough

	case 9:
		err = u.upgradeSchema9to10()
		if err != nil {
			return false, fmt.Errorf("failed to upgrade 1.9.0 to 1.10.0: %v", err)
		}
		current.schema++
		current.maint = 0
		if storeVers(u.db, &current); err != nil {
			return false, err
		}

		fallthrough

	case 10:
		err = u.upgradeSchema10to11()
		if err != nil {
			return false, fmt.Errorf("failed to upgrade 1.10.0 to 1.11.0: %v", err)
		}
		current.schema++
		current.maint = 0
		if storeVers(u.db, &current); err != nil {
			return false, err
		}

		fallthrough

	case 11:
		// Perform schema v11 maintenance.

		// No further upgrades.
		return upgradeCheck()

		// Or continue to upgrades for the next schema version.
		// fallthrough
	default:
		return false, fmt.Errorf("unsupported schema version %d", current.schema)
	}
}

func storeVers(db *sql.DB, dbVer *DatabaseVersion) error {
	err := updateSchemaVersion(db, dbVer.schema)
	if err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}
	err = updateMaintVersion(db, dbVer.maint)
	return fmt.Errorf("failed to update maintenance version: %w", err)
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

func (u *Upgrader) upgradeSchema10to11() error {
	log.Infof("Performing database upgrade 1.10.0 -> 1.11.0")
	// The status table already had an index created automatically because of
	// the UNIQUE constraint declaration for the heights column. Remove the
	// redundant index uix_stats_height on stats(height).
	return DeindexStatsTableOnHeight(u.db)
}

func (u *Upgrader) upgradeSchema9to10() (err error) {
	log.Infof("Performing database upgrade 1.9.0 -> 1.10.0")

	exists, err := TableExists(u.db, "swaps")
	if err != nil {
		return err
	}
	if !exists {
		// NOTE: cannot create table in a DB transaction and use it.
		_, err = u.db.Exec(internal.CreateAtomicSwapTableV0)
		if err != nil {
			return fmt.Errorf("CreateStatsTable: %w", err)
		}

		_, err = u.db.Exec(internal.IndexSwapsOnHeightV0)
		if err != nil {
			return fmt.Errorf("IndexSwapsOnHeight: %v", err)
		}
	}

	dbTx, err := u.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to create db transaction: %w", err)
	}
	defer func() {
		if err == nil {
			err = dbTx.Commit()
			return
		}
		if errRoll := dbTx.Rollback(); errRoll != nil {
			log.Errorf("Rollback failed: %v", errRoll)
			// but keep err
		}
	}()

	makeErr := func(s string, args ...interface{}) error {
		err = fmt.Errorf(s, args...)
		return err
	}

	// Start with a height-ordered list of block data.
	blockRows, err := u.db.Query(`
		SELECT hash, height
		FROM blocks
		WHERE is_mainchain
		ORDER BY height ASC
	;`)
	if err != nil {
		return makeErr("block hash query error: %w", err)
	}
	defer blockRows.Close()

	var redeems, refunds int

	var checkHeight int64
	for blockRows.Next() {
		if u.ctx.Err() != nil {
			return makeErr("context cancelled. rolling back update")
		}

		var height int64
		var hashStr string
		err = blockRows.Scan(&hashStr, &height)
		if err != nil {
			return makeErr("blockRows.Scan: %w", err)
		}
		hash, err := chainhash.NewHashFromStr(hashStr)
		if err != nil {
			return makeErr("NewHashFromStr: %w", err)
		}
		// If the height is not the expected height, the database must be corrupted.
		if height != checkHeight {
			return makeErr("height mismatch %d != %d. database corrupted!", height, checkHeight)
		}
		checkHeight++
		// A periodic update message.
		if height%10000 == 0 {
			log.Infof("Processing atomic swaps in blocks [%d,%d)", height, height+10000)
		}

		msgBlock, err := u.bg.GetBlock(u.ctx, hash)
		if err != nil {
			return makeErr("GetBlock(%v): %w", hash, err)
		}

		for _, tx := range msgBlock.Transactions[1:] { // skip the coinbase
			// This will only identify the redeem and refund txns, unlike
			// the use of TxAtomicSwapsInfo in API and explorer calls.
			swapTxns, err := txhelpers.MsgTxAtomicSwapsInfo(tx, nil, u.params)
			if err != nil {
				log.Warnf("MsgTxAtomicSwapsInfo: %v", err)
				continue
			}
			if swapTxns == nil || swapTxns.Found == "" {
				continue
			}
			for _, red := range swapTxns.Redemptions {
				err = InsertSwap(u.db, height, red)
				if err != nil {
					return makeErr("InsertSwap: %w", err)
				}
				redeems++
			}
			for _, ref := range swapTxns.Refunds {
				err = InsertSwap(u.db, height, ref)
				if err != nil {
					return makeErr("InsertSwap: %w", err)
				}
				refunds++
			}
		}

	}

	if err = blockRows.Err(); err != nil {
		return makeErr("blockRows.Err: %w", err)
	}

	log.Infof("Inserted %d contract redeems, %d contract refunds.", redeems, refunds)

	return nil
}

func (u *Upgrader) upgradeSchema8to9() error {
	log.Infof("Performing database upgrade 1.8.0 -> 1.9.0")

	// Create and index the treasury table.
	_, err := u.db.Exec(
		`CREATE TABLE treasury AS
			SELECT tx_hash, tx_type, spent AS value, block_hash, block_height, block_time, is_mainchain
			FROM transactions
			WHERE tx_type = ANY($1);`,
		pq.Int32Array([]int32{int32(stake.TxTypeTAdd), int32(stake.TxTypeTSpend), int32(stake.TxTypeTreasuryBase)}),
	)
	if err != nil {
		return fmt.Errorf("CreateTreasuryTable: %w", err)
	}

	// Make TSPEND value negative.
	_, err = u.db.Exec(`UPDATE treasury SET value = -value WHERE tx_type=$1;`,
		int(stake.TxTypeTSpend))
	if err != nil {
		return fmt.Errorf("updating tspend values failed: %v", err)
	}

	// Set TADD value properly from vout 0 value.
	_, err = u.db.Exec(`UPDATE treasury SET value = vouts.value FROM vouts
		WHERE tx_type=$1 AND treasury.tx_hash=vouts.tx_hash AND vouts.tx_index=0;`,
		int(stake.TxTypeTAdd))
	if err != nil {
		return fmt.Errorf("updating tadd values failed: %w", err)
	}

	_, err = u.db.Exec(internal.IndexTreasuryOnTxHash)
	if err != nil {
		return fmt.Errorf("IndexTreasuryOnTxHash: %w", err)
	}

	_, err = u.db.Exec(internal.IndexTreasuryOnBlockHeight)
	if err != nil {
		return fmt.Errorf("IndexTreasuryOnBlockHeight: %w", err)
	}

	// Import treasury txns from the transactions table.

	return nil
}

func (u *Upgrader) upgradeSchema7to8() error {
	log.Infof("Performing database upgrade 1.7.0 -> 1.8.0")
	// Index the transactions table on block height. This drastically
	// accelerates several queries including those for the following charts
	// updaters: fees, coin supply, privacy participation, and anonymity set.
	return IndexTransactionTableOnBlockHeight(u.db)
}

func (u *Upgrader) upgradeSchema6to7() error {
	log.Infof("Performing database upgrade 1.6.0 -> 1.7.0")
	// Create the missing vouts.spend_tx_row_id index.
	return IndexVoutTableOnSpendTxID(u.db)
}

func (u *Upgrader) upgradeSchema5to6() error {
	// Add the mixed column to vouts table.
	log.Infof("Performing database upgrade 1.5.1 -> 1.6.0")
	_, err := u.db.Exec(`ALTER TABLE vouts
		ADD COLUMN mixed BOOLEAN DEFAULT FALSE,
		ADD COLUMN spend_tx_row_id INT8;`)
	if err != nil {
		return fmt.Errorf("ALTER TABLE vouts error: %v", err)
	}

	// Set the vouts.mixed column based on transactions.mix_denom and
	// transactions.vout_db_ids and vouts.value.
	log.Infof("Setting vouts.mixed (BOOLEAN) column for mixing transaction outputs with mix_denom value...")
	_, err = u.db.Exec(`UPDATE vouts SET mixed=true
		FROM transactions
		WHERE vouts.id=ANY(transactions.vout_db_ids)
			AND vouts.value=transactions.mix_denom
			AND transactions.mix_denom>0;`)
	if err != nil {
		return fmt.Errorf("UPDATE vouts.mixed error: %v", err)
	}

	// Set vouts.spend_tx_row_id using vouts.tx_hash, vins.prev_tx_hash, and
	// transactions.tx_hash.
	log.Infof("Setting vouts.spend_tx_row_id (INT8) column. This will take a while...")
	var N int64
	N, err = updateSpendTxInfoInAllVouts(u.db)
	if err != nil {
		return fmt.Errorf("UPDATE vouts.spend_tx_row_id error: %v", err)
	}
	log.Debugf("Updated %d rows of vouts table.", N)

	// var rows *sql.Rows
	// rows, err = u.db.Query(`SELECT vouts.id AS vout_id, transactions.block_height AS spend_height
	// 	FROM vouts
	// 	JOIN vins ON vouts.tx_hash=vins.prev_tx_hash AND mixed=TRUE AND vins.is_mainchain=TRUE
	// 	JOIN transactions ON transactions.tx_hash=vins.tx_hash;`)
	// if err != nil {
	// 	return fmt.Errorf("SELECT error: %v", err)
	// }
	// defer rows.Close()

	// var voutIDs, spendHeights []int64
	// for rows.Next() {
	// 	var voutID, spendHeight int64
	// 	err = rows.Scan(&voutID, &spendHeight)
	// 	if err != nil {
	// 		return fmt.Errorf("Scan error: %v", err)
	// 	}
	// 	voutIDs = append(voutIDs, voutID)
	// 	spendHeights = append(spendHeights, spendHeight)
	// }

	// for i := range voutIDs {
	// 	var N int64
	// 	N, err = sqlExec(u.db, `UPDATE vouts SET spend_height = $1 WHERE id=$2`, "UPDATE vouts error",
	// 		spendHeights[i], voutIDs[i])
	// 	if err != nil {
	// 		return err
	// 	}
	// 	if N != 1 {
	// 		return fmt.Errorf("failed to update 1 row, updated %d", N)
	// 	}
	// }

	// For all mixed vouts where spending tx is type stake.TxTypeSStx (a
	// ticket), set the ticket's vouts as mixed.
	log.Infof("Setting vouts.mixed (BOOLEAN) column for tickets funded by mixing split txns...")
	_, err = u.db.Exec(`UPDATE vouts SET mixed=TRUE
		FROM (SELECT DISTINCT ON(transactions.id) transactions.vout_db_ids
			FROM vouts
			JOIN transactions
				ON vouts.spend_tx_row_id=transactions.id
					AND vouts.mixed=true
					AND transactions.tx_type=1) AS mix_funded_tickets
		WHERE vouts.id=ANY(mix_funded_tickets.vout_db_ids)
			AND vouts.value > 0;`)
	if err != nil {
		return fmt.Errorf("UPDATE ticket vouts error: %v", err)
	}

	// For all mixed vouts where spending tx is type stake.TxTypeGen (a vote),
	// set the vote's vouts as mixed.
	log.Infof("Setting vouts.mixed (BOOLEAN) column for votes and revokes funded by tickets funded by mixing split txns...")
	_, err = u.db.Exec(`UPDATE vouts SET mixed=TRUE
		FROM (SELECT DISTINCT ON(transactions.id) transactions.vout_db_ids
			FROM vouts
			JOIN transactions
				ON vouts.spend_tx_row_id=transactions.id
					AND vouts.mixed=true
					AND (transactions.tx_type=2 OR transactions.tx_type=3)) AS mix_funded_votes
		WHERE vouts.id=ANY(mix_funded_votes.vout_db_ids)
			AND vouts.value > 0;`)
	if err != nil {
		return fmt.Errorf("UPDATE vote vouts error: %v", err)
	}

	// NOTE: fund and spend heights of mix transaction outputs
	// `SELECT vouts.value, fund_tx.block_height, spend_tx.block_height
	// 	FROM vouts
	// 	JOIN transactions AS fund_tx ON vouts.tx_hash=fund_tx.tx_hash
	// 	JOIN transactions AS spend_tx ON spend_tx_row_id=spend_tx.id
	// 	WHERE mixed=true;`

	return nil
}

func (u *Upgrader) upgradeSchema4to5() error {
	// Add the mix_count and mix_denom columns to the transactions table.
	log.Infof("Performing database upgrade 1.4.0 -> 1.5.0")
	_, err := u.db.Exec(`ALTER TABLE transactions
		ADD COLUMN IF NOT EXISTS mix_count INT4 DEFAULT 0,
		ADD COLUMN IF NOT EXISTS mix_denom INT8 DEFAULT 0;`)
	if err != nil {
		return fmt.Errorf("ALTER TABLE transactions error: %v", err)
	}

	return u.setTxMixData()
}

func (u *Upgrader) setTxMixData() error {
	log.Infof("Retrieving possible mix transactions...")
	txnRows, err := u.db.Query(`SELECT transactions.id, transactions.tx_hash, array_agg(value), min(blocks.sbits)
		FROM transactions
		JOIN vouts ON vouts.id=ANY(vout_db_ids)
		JOIN blocks ON blocks.hash = transactions.block_hash
		WHERE tree = 0 AND num_vout>=3
		GROUP BY transactions.id;`)
	if err != nil {
		return fmt.Errorf("transaction query error: %v", err)
	}

	var mixIDs []int64
	var mixDenoms []int64
	var mixCounts []uint32

	msgTx := new(wire.MsgTx)
	for txnRows.Next() {
		var vals []int64
		var hash string
		var id, ticketPrice int64
		err = txnRows.Scan(&id, &hash, pq.Array(&vals), &ticketPrice)
		if err != nil {
			txnRows.Close()
			return fmt.Errorf("Scan failed: %v", err)
		}

		txouts := make([]*wire.TxOut, 0, len(vals))
		txins := make([]*wire.TxIn, 0, len(vals))
		for _, v := range vals {
			txouts = append(txouts, &wire.TxOut{
				Value: v,
			})
			txins = append(txins, &wire.TxIn{ /*dummy*/ })
		}
		msgTx.TxOut = txouts
		msgTx.TxIn = txins

		_, mixDenom, mixCount := txhelpers.IsMixTx(msgTx)
		if mixCount == 0 {
			_, mixDenom, mixCount = txhelpers.IsMixedSplitTx(msgTx, int64(txhelpers.DefaultRelayFeePerKb), ticketPrice)
			if mixCount == 0 {
				continue
			}
		}

		mixIDs = append(mixIDs, id)
		mixDenoms = append(mixDenoms, mixDenom)
		mixCounts = append(mixCounts, mixCount)
	}

	txnRows.Close()

	stmt, err := u.db.Prepare(`UPDATE transactions SET mix_count = $2, mix_denom = $3 WHERE id = $1;`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	log.Infof("Updating transaction data for %d mix transactions...", len(mixIDs))
	for i := range mixIDs {
		N, err := sqlExecStmt(stmt, "failed to update transaction: ", mixIDs[i], mixCounts[i], mixDenoms[i])
		if err != nil {
			return err
		}
		if N != 1 {
			log.Warnf("Updated %d transactions rows instead of 1", N)
		}
	}

	return err
}

// This changes the data type of votes.version from INT2 to INT4.
func (u *Upgrader) upgradeSchema3to4() error {
	// Change the data type of votes.version.
	log.Infof("Performing database upgrade 1.3.0 -> 1.4.0")
	_, err := u.db.Exec(`ALTER TABLE votes ALTER COLUMN version TYPE INT4`)
	return err
}

// This indexes the blocks table on the "time" column.
func (u *Upgrader) upgradeSchema2to3() error {
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
func (u *Upgrader) upgradeSchema1to2() error {
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
	dir, err := os.MkdirTemp("", "tempstake")
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
		// A periodic update message.
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
