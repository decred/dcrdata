// Copyright (c) 2018, The Decred developers
// See LICENSE for details.

package dcrpg

import (
	"bytes"
	"database/sql"
	"fmt"
	"time"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/db/dcrpg/internal"
	"github.com/decred/dcrdata/rpcutils"
	"github.com/decred/dcrdata/txhelpers"
)

// tableUpgradeType defines the types of upgrades that currently exists and
// happen automatically. This upgrade run on normal start up the first the
// style is run after updating dcrdata past version 3.0.0.
type tableUpgradeType int

const (
	vinsTableCoinSupplyUpgrade tableUpgradeType = iota
	agendasTableUpgrade
	vinsTableMainchainUpgrade
	blocksTableMainchainUpgrade
	transactionsTableMainchainUpgrade
	addressesTableMainchainUpgrade
	votesTableMainchainUpgrade
)

type TableUpgradeType struct {
	TableName   string
	upgradeType tableUpgradeType
}

// CheckForAuxDBUpgrade checks if an upgrade is required and currently supported.
// A boolean value is returned to indicate if the db upgrade was
// successfully completed.
func (pgb *ChainDB) CheckForAuxDBUpgrade(dcrdClient *rpcclient.Client) (bool, error) {
	var version, needVersion TableVersion
	upgradeInfo := TableUpgradesRequired(TableVersions(pgb.db))

	if len(upgradeInfo) > 0 {
		version = upgradeInfo[0].CurrentVer
		needVersion = upgradeInfo[0].RequiredVer
	} else {
		return false, nil
	}

	// Apply each upgrade in succession
	switch {
	case upgradeInfo[0].UpgradeType != "upgrade":
		return false, nil

	// Upgrade from 3.1.0 --> 3.2.0
	case version.major == 3 && version.minor == 1 && version.patch == 0:
		toVersion := TableVersion{3, 2, 0}
		smartClient := rpcutils.NewBlockGate(dcrdClient, 10)

		theseUpgrades := []TableUpgradeType{
			{"agendas", agendasTableUpgrade},
			{"vins", vinsTableCoinSupplyUpgrade},
		}

		for it := range theseUpgrades {
			upgradeSuccess, err := pgb.handleUpgrades(smartClient, theseUpgrades[it].upgradeType)
			if err != nil || !upgradeSuccess {
				return false, fmt.Errorf("failed to upgrade %s table to version %v. Error: %v",
					theseUpgrades[it].TableName, toVersion, err)
			}
		}

		// Bump version
		if err := versionAllTables(pgb.db, toVersion); err != nil {
			return false, fmt.Errorf("failed to bump version to %v: %v", toVersion, err)
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.2.0 --> 3.3.0
	case version.major == 3 && version.minor == 2 && version.patch == 0:
		toVersion := TableVersion{3, 3, 0}
		smartClient := rpcutils.NewBlockGate(dcrdClient, 10)

		// The order of these upgrades is critical
		theseUpgrades := []TableUpgradeType{
			{"blocks", blocksTableMainchainUpgrade}, // also patches block_chain
			{"transactions", transactionsTableMainchainUpgrade},
			{"vins", vinsTableMainchainUpgrade},
			{"addresses", addressesTableMainchainUpgrade},
			{"votes", votesTableMainchainUpgrade},
		}

		for it := range theseUpgrades {
			upgradeSuccess, err := pgb.handleUpgrades(smartClient, theseUpgrades[it].upgradeType)
			if err != nil || !upgradeSuccess {
				return false, fmt.Errorf("failed to upgrade %s table to version %v. Error: %v",
					theseUpgrades[it].TableName, toVersion, err)
			}
		}

		// Bump version
		if err := versionAllTables(pgb.db, toVersion); err != nil {
			return false, fmt.Errorf("failed to bump version to %v: %v", toVersion, err)
		}

		// Go on to next upgrade
		// fallthrough
		// or be done.
	default:
		// UpgradeType == "upgrade", but no supported case.
		return false, fmt.Errorf("no upgrade path available for version %v --> %v",
			version, needVersion)
	}

	// Ensure the required version was reached.
	upgradeInfo = TableUpgradesRequired(TableVersions(pgb.db))
	if len(upgradeInfo) > 0 && upgradeInfo[0].UpgradeType != "ok" {
		return false, fmt.Errorf("failed to upgrade tables to required version %v", needVersion)
	}

	// Unsupported upgrades caught by default case, so we've succeeded.
	log.Infof("Table upgrades completed.")
	return true, nil
}

// handleUpgrades the individual upgrade and returns a bool and an error
// indicating if the upgrade was successful or not.
func (pgb *ChainDB) handleUpgrades(client *rpcutils.BlockGate,
	tableUpgrade tableUpgradeType) (bool, error) {
	// For the agendas upgrade, i is set to a block height of 128000 since that
	// is when the first vote for an agenda was cast. For vins upgrades (coin
	// supply and mainchain), i is not set since all the blocks are considered.
	var err error
	var i uint64
	var tableReady bool
	var tableName, upgradeTypeStr string
	switch tableUpgrade {
	case vinsTableCoinSupplyUpgrade:
		tableReady, err = addVinsColumnsForCoinSupply(pgb.db)
		tableName, upgradeTypeStr = "vins", "vin validity/coin supply"
	case vinsTableMainchainUpgrade:
		tableReady, err = addVinsColumnsForMainchain(pgb.db)
		tableName, upgradeTypeStr = "vins", "sidechain/reorg"
	case blocksTableMainchainUpgrade:
		tableReady, err = addBlocksColumnsForMainchain(pgb.db)
		tableName, upgradeTypeStr = "blocks", "sidechain/reorg"
	case addressesTableMainchainUpgrade:
		tableReady, err = addAddressesColumnsForMainchain(pgb.db)
		tableName, upgradeTypeStr = "addresses", "sidechain/reorg"
	case votesTableMainchainUpgrade:
		tableReady, err = addVotesColumnsForMainchain(pgb.db)
		tableName, upgradeTypeStr = "votes", "sidechain/reorg"
	case transactionsTableMainchainUpgrade:
		tableReady, err = addTransactionsColumnsForMainchain(pgb.db)
		tableName, upgradeTypeStr = "transactions", "sidechain/reorg"
	case agendasTableUpgrade:
		tableReady, err = haveEmptyAgendasTable(pgb.db)
		tableName, upgradeTypeStr = "agendas", "new table"
		i = 128000
	default:
		return false, fmt.Errorf(`upgrade "%v" is unknown`, tableUpgrade)
	}

	// Ensure new columns were added successfully.
	if err != nil || !tableReady {
		return false, fmt.Errorf("failed to prepare table %s for %s upgrade: %v",
			tableName, upgradeTypeStr, err)
	}

	// Update table data
	var rowsUpdated int64
	timeStart := time.Now()

	switch tableUpgrade {
	case vinsTableCoinSupplyUpgrade, agendasTableUpgrade:
		// height is the best block where this table upgrade should stop at.
		height, err := pgb.HeightDB()
		if err != nil {
			return false, err
		}
		log.Infof("Found the best block at height: %v", height)

		// For each block on the main chain, perform upgrade operations
		for ; i <= height; i++ {
			block, err := client.UpdateToBlock(int64(i))
			if err != nil {
				return false, err
			}

			if i%5000 == 0 {
				var limit = i + 5000
				if height < limit {
					limit = height
				}
				log.Infof("Upgrading the %s table (%s upgrade) from height %v to %v ",
					tableName, upgradeTypeStr, i, limit-1)
			}

			var rows int64
			var msgBlock = block.MsgBlock()

			switch tableUpgrade {
			case vinsTableCoinSupplyUpgrade:
				rows, err = pgb.handlevinsTableCoinSupplyUpgrade(msgBlock)
			case agendasTableUpgrade:
				rows, err = pgb.handleAgendasTableUpgrade(msgBlock)
			}
			if err != nil {
				return false, err
			}

			rowsUpdated += rows
		}

	case blocksTableMainchainUpgrade:
		// blocks table upgrade proceeds from best block back to genesis
		blockHash, err := pgb.HashDB()
		if err != nil {
			return false, fmt.Errorf("failed to retrieve best block from DB: %v", err)
		}
		log.Infof("Starting blocks table mainchain upgrade at block %s (working back to genesis)", blockHash)
		rowsUpdated, err = pgb.handleBlocksTableMainchainUpgrade(blockHash)
		if err != nil {
			return false, fmt.Errorf(`upgrade of blocks table ended prematurely after %d blocks. `+
				`Error: %v`, rowsUpdated, err)
		}
	case transactionsTableMainchainUpgrade:
		// transactions table upgrade handled entirely by the DB backend
		log.Infof("Starting transactions table mainchain upgrade...")
		rowsUpdated, err = pgb.handleTransactionsTableMainchainUpgrade()
		if err != nil {
			return false, fmt.Errorf(`upgrade of transactions table ended prematurely after %d transactions. `+
				`Error: %v`, rowsUpdated, err)
		}
	case vinsTableMainchainUpgrade:
		// vins table upgrade handled entirely by the DB backend
		log.Infof("Starting vins table mainchain upgrade...")
		rowsUpdated, err = pgb.handleVinsTableMainchainupgrade()
		if err != nil {
			return false, fmt.Errorf(`upgrade of vins table ended prematurely after %d vins. `+
				`Error: %v`, rowsUpdated, err)
		}
	case votesTableMainchainUpgrade:
		// votes table upgrade handled entirely by the DB backend
		log.Infof("Starting votes table mainchain upgrade...")
		rowsUpdated, err = updateAllVotesMainchain(pgb.db)
		if err != nil {
			return false, fmt.Errorf(`upgrade of votes table ended prematurely after %d votes. `+
				`Error: %v`, rowsUpdated, err)
		}
	case addressesTableMainchainUpgrade:
		// addresses table upgrade handled entirely by the DB backend
		log.Infof("Starting addresses table mainchain upgrade...")
		log.Infof("This an extremely I/O intensive operation on the database machine. It can take from 30-90 minutes.")
		rowsUpdated, err = updateAllAddressesValidMainchain(pgb.db)
		if err != nil {
			return false, fmt.Errorf(`upgrade of addresses table ended prematurely after %d address rows.`+
				`Error: %v`, rowsUpdated, err)
		}
	default:
		return false, fmt.Errorf(`upgrade "%v" unknown`, tableUpgrade)
	}

	log.Infof(" - %v rows in %s table (%s upgrade) were successfully upgraded in %.1f sec",
		rowsUpdated, tableName, upgradeTypeStr, time.Since(timeStart).Seconds())

	// Indexes
	switch tableUpgrade {
	case vinsTableCoinSupplyUpgrade:
		log.Infof("Index the agendas table on Agenda ID...")
		if err = IndexAgendasTableOnAgendaID(pgb.db); err != nil {
			return false, fmt.Errorf("failed to index agendas table: %v", err)
		}

		log.Infof("Index the agendas table on Block Time...")
		if err = IndexAgendasTableOnBlockTime(pgb.db); err != nil {
			return false, fmt.Errorf("failed to index agendas table: %v", err)
		}
	}

	return true, nil
}

func (pgb *ChainDB) handleVinsTableMainchainupgrade() (int64, error) {
	// Get all of the block hashes
	log.Infof(" - Retrieving all block hashes...")
	blockHashes, err := RetrieveBlocksHashesAll(pgb.db)
	if err != nil {
		return 0, fmt.Errorf("unable to retrieve all block hashes: %v", err)
	}

	log.Infof(" - Updating vins data for each transactions in every block...")
	var rowsUpdated int64
	for i, blockHash := range blockHashes {
		vinDbIDsBlk, areValid, areMainchain, err := RetrieveTxnsVinsByBlock(pgb.db, blockHash)
		if err != nil {
			return 0, fmt.Errorf("unable to retrieve vin data for block %s: %v", blockHash, err)
		}

		numUpd, err := pgb.upgradeVinsMainchainForMany(vinDbIDsBlk, areValid, areMainchain)
		if err != nil {
			log.Warnf("Unable to set valid/mainchain for vins: %v", err)
		}
		rowsUpdated += numUpd
		if i%10000 == 0 {
			log.Debugf(" -- updated %d vins for %d of %d blocks", rowsUpdated, i+1, len(blockHashes))
		}
	}
	return rowsUpdated, nil
}

func (pgb *ChainDB) upgradeVinsMainchainForMany(vinDbIDsBlk []dbtypes.UInt64Array,
	areValid, areMainchain []bool) (int64, error) {
	var rowsUpdated int64
	// each transaction
	for it, vs := range vinDbIDsBlk {
		// each vin
		numUpd, err := pgb.upgradeVinsMainchainOneTxn(vs, areValid[it], areMainchain[it])
		if err != nil {
			continue
		}
		rowsUpdated += numUpd
	}
	return rowsUpdated, nil
}

func (pgb *ChainDB) upgradeVinsMainchainOneTxn(vinDbIDs dbtypes.UInt64Array,
	isValid, isMainchain bool) (int64, error) {
	var rowsUpdated int64

	// each vin
	for _, vinDbID := range vinDbIDs {
		result, err := pgb.db.Exec(internal.SetIsValidIsMainchainByVinID,
			vinDbID, isValid, isMainchain)
		if err != nil {
			log.Warnf("db ID not found: %d", vinDbID)
			continue
		}

		c, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}

		rowsUpdated += c
	}

	return rowsUpdated, nil
}

// updateAllVotesMainchain sets is_mainchain for all votes according to their
// containing block.
func updateAllVotesMainchain(db *sql.DB) (rowsUpdated int64, err error) {
	return sqlExec(db, internal.UpdateVotesMainchainAll,
		"failed to update votes and mainchain status")
}

// updateAllTxnsValidMainchain sets is_mainchain and is_valid for all
// transactions according to their containing block.
func updateAllTxnsValidMainchain(db *sql.DB) (rowsUpdated int64, err error) {
	rowsUpdated, err = sqlExec(db, internal.UpdateRegularTxnsValidAll,
		"failed to update regular transactions' validity status")
	if err != nil {
		return
	}
	return sqlExec(db, internal.UpdateTxnsMainchainAll,
		"failed to update all transactions' mainchain status")
}

// updateAllAddressesValidMainchain sets valid_mainchain for all addresses table
// rows according to their corresponding transaction.
func updateAllAddressesValidMainchain(db *sql.DB) (rowsUpdated int64, err error) {
	return sqlExec(db, internal.UpdateValidMainchainFromTransactions,
		"failed to update addresses rows valid_mainchain status")
}

// handleBlocksTableMainchainUpgrade sets is_mainchain=true for all blocks in
// the main chain, starting with the best block and working back to genesis.
func (pgb *ChainDB) handleBlocksTableMainchainUpgrade(bestBlock string) (int64, error) {
	// Start at best block, upgrade, and move to previous block. Stop after
	// genesis, which previous block hash is the zero hash.
	var blocksUpdated int64
	previousHash, thisBlockHash := bestBlock, bestBlock
	for !bytes.Equal(zeroHashStringBytes, []byte(previousHash)) {
		// set is_mainchain=1 and get previous_hash
		var err error
		previousHash, err = SetMainchainByBlockHash(pgb.db, thisBlockHash, true)
		if err != nil {
			return blocksUpdated, fmt.Errorf("SetMainchainByBlockHash for block %s failed: %v",
				thisBlockHash, err)
		}
		blocksUpdated++

		// genesis is not in the block_chain table.  All done!
		if bytes.Equal(zeroHashStringBytes, []byte(previousHash)) {
			break
		}

		// patch block_chain table
		err = UpdateBlockNextByHash(pgb.db, previousHash, thisBlockHash)
		if err != nil {
			log.Errorf("Failed to update next_hash in block_chain for block %s", previousHash)
		}

		thisBlockHash = previousHash
	}
	return blocksUpdated, nil
}

// handleTransactionsTableMainchainUpgrade sets is_mainchain and is_valid for
// all transactions according to their containing block. The number of
// transactions updates is returned, along with an error value.
func (pgb *ChainDB) handleTransactionsTableMainchainUpgrade() (int64, error) {
	return updateAllTxnsValidMainchain(pgb.db)
}

// handlevinsTableCoinSupplyUpgrade implements the upgrade to the new newly added columns
// in the vins table. The new columns are mainly used for the coin supply chart.
// If all the new columns are not added, quit the db upgrade.
func (pgb *ChainDB) handlevinsTableCoinSupplyUpgrade(msgBlock *wire.MsgBlock) (int64, error) {
	var isValid bool
	var rowsUpdated int64

	var err = pgb.db.QueryRow(`SELECT is_valid, is_mainchain FROM blocks WHERE hash = $1 ;`,
		msgBlock.BlockHash().String()).Scan(&isValid)
	if err != nil {
		return 0, err
	}

	// isMainchain does not mater since it is not used in this upgrade.
	_, _, stakedDbTxVins := dbtypes.ExtractBlockTransactions(
		msgBlock, wire.TxTreeStake, pgb.chainParams, isValid, false)
	_, _, regularDbTxVins := dbtypes.ExtractBlockTransactions(
		msgBlock, wire.TxTreeRegular, pgb.chainParams, isValid, false)
	dbTxVins := append(stakedDbTxVins, regularDbTxVins...)

	for _, v := range dbTxVins {
		for _, s := range v {
			// does not set is_mainchain
			result, err := pgb.db.Exec(internal.SetVinsTableCoinSupplyUpgrade,
				s.IsValid, s.Time, s.ValueIn, s.TxID, s.TxIndex, s.TxTree)
			if err != nil {
				return 0, err
			}

			c, err := result.RowsAffected()
			if err != nil {
				return 0, err
			}

			rowsUpdated += c
		}
	}
	return rowsUpdated, nil
}

// handleAgendasTableUpgrade implements the upgrade to the newly added agenda table.
func (pgb *ChainDB) handleAgendasTableUpgrade(msgBlock *wire.MsgBlock) (int64, error) {
	milestones := map[string]dbtypes.MileStone{
		"sdiffalgorithm": {
			Activated:  149248,
			HardForked: 149328,
			LockedIn:   141184,
		},
		"lnsupport": {
			Activated: 149248,
			LockedIn:  141184,
		},
		"lnfeatures": {
			Activated: 189568,
			LockedIn:  181504,
		},
	}

	// neither isValid or isMainchain are important
	dbTxns, _, _ := dbtypes.ExtractBlockTransactions(msgBlock,
		wire.TxTreeStake, pgb.chainParams, true, false)

	var rowsUpdated int64
	for i, tx := range dbTxns {
		if tx.TxType != int16(stake.TxTypeSSGen) {
			continue
		}
		_, _, _, choices, err := txhelpers.SSGenVoteChoices(msgBlock.STransactions[i],
			pgb.chainParams)
		if err != nil {
			return 0, err
		}

		var rowID uint64
		for _, val := range choices {
			// check if agenda id exists, if not it skips to the next agenda id
			var progress, ok = milestones[val.ID]
			if !ok {
				log.Debugf("The Agenda ID: '%s' is unknown", val.ID)
				continue
			}

			var index, err = dbtypes.ChoiceIndexFromStr(val.Choice.Id)
			if err != nil {
				return 0, err
			}

			err = pgb.db.QueryRow(internal.MakeAgendaInsertStatement(false),
				val.ID, index, tx.TxID, tx.BlockHeight, tx.BlockTime,
				progress.LockedIn == tx.BlockHeight,
				progress.Activated == tx.BlockHeight,
				progress.HardForked == tx.BlockHeight).Scan(&rowID)
			if err != nil {
				return 0, err
			}

			rowsUpdated++
		}
	}
	return rowsUpdated, nil
}

// haveEmptyAgendasTable checks if the agendas table is empty. If the agenda
// table exists bool false is returned otherwise bool true is returned.
// If the table is not empty then this upgrade doesn't proceed.
func haveEmptyAgendasTable(db *sql.DB) (bool, error) {
	var isExists int
	var err = db.QueryRow(`SELECT COUNT(*) FROM agendas;`).Scan(&isExists)
	if err != nil {
		return false, err
	}

	if isExists != 0 {
		return false, nil
	}

	return true, nil
}

type newColumn struct {
	Name    string
	Type    string
	Default string
}

// addNewColumnsIfNotFound checks if the new columns already exist and adds them
// if they are missing. If any of the expected new columns exist the upgrade is
// ready to go (and any existing columns will be patched).
func addNewColumnsIfNotFound(db *sql.DB, table string, newColumns []newColumn) (bool, error) {
	for ic := range newColumns {
		var isRowFound bool
		err := db.QueryRow(`SELECT EXISTS( SELECT column_name FROM INFORMATION_SCHEMA.COLUMNS 
			WHERE table_name = $1 AND column_name = $2 );`, table, newColumns[ic].Name).Scan(&isRowFound)
		if err != nil {
			return false, err
		}

		if isRowFound {
			return true, nil
		}

		log.Infof("Adding column %s to table %s...", newColumns[ic].Name, table)
		var result sql.Result
		if newColumns[ic].Default != "" {
			result, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s DEFAULT %s;",
				table, newColumns[ic].Name, newColumns[ic].Type, newColumns[ic].Default))
		} else {
			result, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;",
				table, newColumns[ic].Name, newColumns[ic].Type))
		}
		if err != nil {
			return false, err
		}

		_, err = result.RowsAffected()
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

func addVinsColumnsForCoinSupply(db *sql.DB) (bool, error) {
	// The new columns and their data types
	newColumns := []newColumn{
		{"is_valid", "BOOLEAN", "False"},
		{"block_time", "INT8", "0"},
		{"value_in", "INT8", "0"},
	}
	return addNewColumnsIfNotFound(db, "vins", newColumns)
}

func addVinsColumnsForMainchain(db *sql.DB) (bool, error) {
	// The new columns and their data types
	newColumns := []newColumn{
		{"is_mainchain", "BOOLEAN", ""}, // no default because this takes forever and we'll check for "true"
	}
	return addNewColumnsIfNotFound(db, "vins", newColumns)
}

func addBlocksColumnsForMainchain(db *sql.DB) (bool, error) {
	// The new columns and their data types
	newColumns := []newColumn{
		{"is_mainchain", "BOOLEAN", "False"},
	}
	return addNewColumnsIfNotFound(db, "blocks", newColumns)
}

func addVotesColumnsForMainchain(db *sql.DB) (bool, error) {
	// The new columns and their data types
	newColumns := []newColumn{
		{"is_mainchain", "BOOLEAN", ""},
	}
	return addNewColumnsIfNotFound(db, "votes", newColumns)
}

func addTransactionsColumnsForMainchain(db *sql.DB) (bool, error) {
	// The new columns and their data types
	newColumns := []newColumn{
		{"is_valid", "BOOLEAN", "False"},
		{"is_mainchain", "BOOLEAN", "False"},
	}
	return addNewColumnsIfNotFound(db, "transactions", newColumns)
}

func addAddressesColumnsForMainchain(db *sql.DB) (bool, error) {
	// The new columns and their data types
	newColumns := []newColumn{
		{"valid_mainchain", "BOOLEAN", ""}, // no default because this takes forever and we'll check for "true"
	}
	return addNewColumnsIfNotFound(db, "addresses", newColumns)
}

// versionAllTables comments the tables with the upgraded table version.
func versionAllTables(db *sql.DB, version TableVersion) error {
	for tableName := range createTableStatements {
		_, err := db.Exec(fmt.Sprintf(`COMMENT ON TABLE %s IS 'v%s';`,
			tableName, version))
		if err != nil {
			return err
		}

		log.Infof("Modified the %v table version to %v", tableName, version)
	}
	return nil
}
