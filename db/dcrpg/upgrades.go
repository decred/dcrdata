// Copyright (c) 2018, The Decred developers
// See LICENSE for details.

package dcrpg

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/v3/db/dbtypes"
	"github.com/decred/dcrdata/v3/db/dcrpg/internal"
	"github.com/decred/dcrdata/v3/rpcutils"
	"github.com/decred/dcrdata/v3/txhelpers"
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
	ticketsTableMainchainUpgrade
	votesTableBlockHashIndex
	vinsTxHistogramUpgrade
	addressesTxHistogramUpgrade
	agendasVotingMilestonesUpgrade
	ticketsTableBlockTimeUpgrade
	addressesTableValidMainchainPatch
	addressesTableMatchingTxHashPatch
	addressesTableBlockTimeSortedIndex
	addressesBlockTimeDataTypeUpdate
	blocksBlockTimeDataTypeUpdate
	agendasBlockTimeDataTypeUpdate
	transactionsBlockTimeDataTypeUpdate
	vinsBlockTimeDataTypeUpdate
	blocksChainWorkUpdate
)

type TableUpgradeType struct {
	TableName   string
	upgradeType tableUpgradeType
}

// VinVoutTypeUpdateData defines the fetched details from the transactions table that
// are needed to undertake the histogram upgrade.
type VinVoutTypeUpdateData struct {
	VinsDbIDs  dbtypes.UInt64Array
	VoutsDbIDs dbtypes.UInt64Array
	TxType     stake.TxType
}

// VotingMilestones defines the various milestones phases have taken place when
// the agendas have been up for voting. Only sdiffalgorithm, lnsupport and
// lnfeatures agenda ids exist since appropriate voting mechanisms on the dcrd
// level are not yet fully implemented.
var VotingMilestones = map[string]dbtypes.MileStone{
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

// toVersion defines a table version to which the pg tables will commented to.
var toVersion TableVersion

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
	case upgradeInfo[0].UpgradeType != "upgrade" && upgradeInfo[0].UpgradeType != "reindex":
		return false, nil

	// Upgrade from 3.1.0 --> 3.2.0
	case version.major == 3 && version.minor == 1 && version.patch == 0:
		toVersion = TableVersion{3, 2, 0}
		smartClient := rpcutils.NewBlockGate(dcrdClient, 10)

		theseUpgrades := []TableUpgradeType{
			{"agendas", agendasTableUpgrade},
			{"vins", vinsTableCoinSupplyUpgrade},
		}

		isSuccess, er := pgb.initiatePgUpgrade(smartClient, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.2.0 --> 3.3.0
	case version.major == 3 && version.minor == 2 && version.patch == 0:
		toVersion = TableVersion{3, 3, 0}
		smartClient := rpcutils.NewBlockGate(dcrdClient, 10)

		// The order of these upgrades is critical
		theseUpgrades := []TableUpgradeType{
			{"blocks", blocksTableMainchainUpgrade}, // also patches block_chain
			{"transactions", transactionsTableMainchainUpgrade},
			{"vins", vinsTableMainchainUpgrade},
			{"addresses", addressesTableMainchainUpgrade},
			{"votes", votesTableMainchainUpgrade},
		}

		isSuccess, er := pgb.initiatePgUpgrade(smartClient, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.3.0 --> 3.4.0
	case version.major == 3 && version.minor == 3 && version.patch == 0:
		toVersion = TableVersion{3, 4, 0}
		smartClient := rpcutils.NewBlockGate(dcrdClient, 10)

		// The order of these upgrades is critical
		theseUpgrades := []TableUpgradeType{
			{"tickets", ticketsTableMainchainUpgrade},
		}

		isSuccess, er := pgb.initiatePgUpgrade(smartClient, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.4.0 --> 3.4.1
	case version.major == 3 && version.minor == 4 && version.patch == 0:
		// This is a "reindex" upgrade. Bump patch.
		toVersion = TableVersion{3, 4, 1}

		// The order of these upgrades is critical
		theseUpgrades := []TableUpgradeType{
			{"votes", votesTableBlockHashIndex},
		}

		isSuccess, er := pgb.initiatePgUpgrade(nil, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.4.1 --> 3.5.0
	case version.major == 3 && version.minor == 4 && version.patch == 1:
		toVersion = TableVersion{3, 5, 0}

		theseUpgrades := []TableUpgradeType{
			{"vins", vinsTxHistogramUpgrade},
			{"addresses", addressesTxHistogramUpgrade},
		}

		isSuccess, er := pgb.initiatePgUpgrade(nil, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.5.0 --> 3.5.1
	case version.major == 3 && version.minor == 5 && version.patch == 0:
		toVersion = TableVersion{3, 5, 1}

		theseUpgrades := []TableUpgradeType{
			{"agendas", agendasVotingMilestonesUpgrade},
		}

		isSuccess, er := pgb.initiatePgUpgrade(nil, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.5.1 --> 3.5.2
	case version.major == 3 && version.minor == 5 && version.patch == 1:
		toVersion = TableVersion{3, 5, 2}

		theseUpgrades := []TableUpgradeType{
			{"tickets", ticketsTableBlockTimeUpgrade},
		}

		isSuccess, er := pgb.initiatePgUpgrade(nil, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.5.2 --> 3.5.3
	case version.major == 3 && version.minor == 5 && version.patch == 2:
		toVersion = TableVersion{3, 5, 3}

		theseUpgrades := []TableUpgradeType{
			{"addresses", addressesTableValidMainchainPatch},
		}

		isSuccess, er := pgb.initiatePgUpgrade(nil, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.5.3 --> 3.5.4
	case version.major == 3 && version.minor == 5 && version.patch == 3:
		toVersion = TableVersion{3, 5, 4}

		theseUpgrades := []TableUpgradeType{
			{"addresses", addressesTableMatchingTxHashPatch},
		}

		isSuccess, er := pgb.initiatePgUpgrade(nil, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.5.4 --> 3.5.5
	case version.major == 3 && version.minor == 5 && version.patch == 4:
		toVersion = TableVersion{3, 5, 5}

		theseUpgrades := []TableUpgradeType{
			{"addresses", addressesTableBlockTimeSortedIndex},
		}

		isSuccess, er := pgb.initiatePgUpgrade(nil, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.5.5 --> 3.6.0
	case version.major == 3 && version.minor == 5 && version.patch == 5:
		toVersion = TableVersion{3, 6, 0}

		theseUpgrades := []TableUpgradeType{
			{"addresses", addressesBlockTimeDataTypeUpdate},
			{"blocks", blocksBlockTimeDataTypeUpdate},
			{"agendas", agendasBlockTimeDataTypeUpdate},
			{"transactions", transactionsBlockTimeDataTypeUpdate},
			{"vins", vinsBlockTimeDataTypeUpdate},
		}

		pgb.dropBlockTimeIndexes()

		isSuccess, er := pgb.initiatePgUpgrade(nil, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		pgb.createBlockTimeIndexes()
		// Go on to next upgrade
		fallthrough

		// Upgrade from 3.6.0 --> 3.7.0
	case version.major == 3 && version.minor == 6 && version.patch == 0:
		toVersion = TableVersion{3, 7, 0}

		theseUpgrades := []TableUpgradeType{
			{"blocks", blocksChainWorkUpdate},
		}

		smartClient := rpcutils.NewBlockGate(dcrdClient, 10)
		isSuccess, er := pgb.initiatePgUpgrade(smartClient, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

	// Go on to next upgrade
	// fallthrough
	// or be done

	default:
		// UpgradeType == "upgrade" or "reindex", but no supported case.
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

// initiatePgUpgrade starts the specific auxiliary database.
func (pgb *ChainDB) initiatePgUpgrade(smartClient *rpcutils.BlockGate, theseUpgrades []TableUpgradeType) (bool, error) {
	for it := range theseUpgrades {
		upgradeSuccess, err := pgb.handleUpgrades(smartClient, theseUpgrades[it].upgradeType)
		if err != nil || !upgradeSuccess {
			return false, fmt.Errorf("failed to upgrade %s table to version %v. Error: %v",
				theseUpgrades[it].TableName, toVersion, err)
		}
	}

	// A Table upgrade must have happened therefore Bump version
	if err := versionAllTables(pgb.db, toVersion); err != nil {
		return false, fmt.Errorf("failed to bump version to %v: %v", toVersion, err)
	}
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
	case ticketsTableMainchainUpgrade:
		tableReady, err = addTicketsColumnsForMainchain(pgb.db)
		tableName, upgradeTypeStr = "tickets", "sidechain/reorg"
	case transactionsTableMainchainUpgrade:
		tableReady, err = addTransactionsColumnsForMainchain(pgb.db)
		tableName, upgradeTypeStr = "transactions", "sidechain/reorg"
	case agendasTableUpgrade:
		tableReady, err = haveEmptyAgendasTable(pgb.db)
		tableName, upgradeTypeStr = "agendas", "new table"
		i = 128000
	case votesTableBlockHashIndex:
		tableReady = true
		tableName, upgradeTypeStr = "votes", "new index"
	case vinsTxHistogramUpgrade:
		tableReady, err = addVinsColumnsForHistogramUpgrade(pgb.db)
		tableName, upgradeTypeStr = "vins", "tx-type addition/histogram"
	case addressesTxHistogramUpgrade:
		tableReady, err = addAddressesColumnsForHistogramUpgrade(pgb.db)
		tableName, upgradeTypeStr = "addresses", "tx-type addition/histogram"
	case agendasVotingMilestonesUpgrade:
		// upgrade only important to all who have already done a fresh data
		// migration between pg version 3.2.0 and 3.5.0.
		tableReady = true
		tableName, upgradeTypeStr = "agendas", "voting milestones"
	case ticketsTableBlockTimeUpgrade:
		tableReady = true
		tableName, upgradeTypeStr = "tickets", "new index"
	case addressesTableValidMainchainPatch:
		tableReady = true
		tableName, upgradeTypeStr = "addresses", "patch valid_mainchain value"
	case addressesTableMatchingTxHashPatch:
		tableReady = true
		tableName, upgradeTypeStr = "addresses", "patch matching_tx_hash value"
	case addressesTableBlockTimeSortedIndex:
		tableReady = true
		tableName, upgradeTypeStr = "addresses", "reindex"
	case addressesBlockTimeDataTypeUpdate:
		tableReady = true
		tableName, upgradeTypeStr = "addresses", "block time data type update"
	case blocksBlockTimeDataTypeUpdate:
		tableReady = true
		tableName, upgradeTypeStr = "blocks", "block time data type update"
	case agendasBlockTimeDataTypeUpdate:
		tableReady = true
		tableName, upgradeTypeStr = "agendas", "block time data type update"
	case transactionsBlockTimeDataTypeUpdate:
		tableReady = true
		tableName, upgradeTypeStr = "transactions", "block time data type update"
	case vinsBlockTimeDataTypeUpdate:
		tableReady = true
		tableName, upgradeTypeStr = "vins", "block time data type update"
	case blocksChainWorkUpdate:
		tableReady, err = addChainWorkColumn(pgb.db)
		tableName, upgradeTypeStr = "blocks", "new chainwork column"
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
		log.Infof("found the best block at height: %v", height)

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
		var blockHash string
		// blocks table upgrade proceeds from best block back to genesis
		_, blockHash, _, err = RetrieveBestBlockHeightAny(context.Background(), pgb.db)
		if err != nil {
			return false, fmt.Errorf("failed to retrieve best block from DB: %v", err)
		}

		log.Infof("Starting blocks table mainchain upgrade at block %s (working back to genesis)", blockHash)
		rowsUpdated, err = pgb.handleBlocksTableMainchainUpgrade(blockHash)

	case transactionsTableMainchainUpgrade:
		// transactions table upgrade handled entirely by the DB backend
		log.Infof("Starting transactions table mainchain upgrade...")
		rowsUpdated, err = pgb.handleTransactionsTableMainchainUpgrade()

	case vinsTableMainchainUpgrade:
		// vins table upgrade handled entirely by the DB backend
		log.Infof("Starting vins table mainchain upgrade...")
		rowsUpdated, err = pgb.handleVinsTableMainchainupgrade()

	case votesTableMainchainUpgrade:
		// votes table upgrade handled entirely by the DB backend
		log.Infof("Starting votes table mainchain upgrade...")
		rowsUpdated, err = updateAllVotesMainchain(pgb.db)

	case ticketsTableMainchainUpgrade:
		// tickets table upgrade handled entirely by the DB backend
		log.Infof("Starting tickets table mainchain upgrade...")
		rowsUpdated, err = updateAllTicketsMainchain(pgb.db)

	case addressesTableMainchainUpgrade:
		// addresses table upgrade handled entirely by the DB backend
		log.Infof("Starting addresses table mainchain upgrade...")
		log.Infof("This an extremely I/O intensive operation on the database machine. It can take from 30-90 minutes.")
		rowsUpdated, err = updateAllAddressesValidMainchain(pgb.db)

	case votesTableBlockHashIndex, ticketsTableBlockTimeUpgrade, addressesTableBlockTimeSortedIndex:
		// no upgrade, just "reindex"
	case vinsTxHistogramUpgrade, addressesTxHistogramUpgrade:
		var height uint64
		// height is the best block where this table upgrade should stop at.
		height, err = pgb.HeightDB()
		if err != nil {
			return false, err
		}

		log.Infof("found the best block at height: %v", height)
		rowsUpdated, err = pgb.handleTxTypeHistogramUpgrade(height, tableUpgrade)

	case agendasVotingMilestonesUpgrade:
		log.Infof("Setting the agendas voting milestones...")
		rowsUpdated, err = pgb.handleAgendasVotingMilestonesUpgrade()

	case addressesTableValidMainchainPatch:
		log.Infof("Patching valid_mainchain in the addresses table...")
		rowsUpdated, err = updateAddressesValidMainchainPatch(pgb.db)

	case addressesTableMatchingTxHashPatch:
		log.Infof("Patching matching_tx_hash in the addresses table...")
		rowsUpdated, err = updateAddressesMatchingTxHashPatch(pgb.db)

	case blocksChainWorkUpdate:
		log.Infof("Syncing chainwork. This might take a while...")
		rowsUpdated, err = verifyChainWork(client, pgb.db)

	case addressesBlockTimeDataTypeUpdate, blocksBlockTimeDataTypeUpdate,
		agendasBlockTimeDataTypeUpdate, transactionsBlockTimeDataTypeUpdate,
		vinsBlockTimeDataTypeUpdate:
	// set block time data type to timestamp

	default:
		return false, fmt.Errorf(`upgrade "%v" unknown`, tableUpgrade)
	}

	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf(`%s upgrade of %s table ended prematurely after %d rows.`+
			`Error: %v`, upgradeTypeStr, tableName, rowsUpdated, err)
	}

	if rowsUpdated > 0 {
		log.Infof(" - %v rows in %s table (%s upgrade) were successfully upgraded in %.2f sec",
			rowsUpdated, tableName, upgradeTypeStr, time.Since(timeStart).Seconds())
	}

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
	case votesTableBlockHashIndex:
		log.Infof("Indexing votes table on block hash...")
		if err = IndexVotesTableOnBlockHash(pgb.db); err != nil {
			return false, fmt.Errorf("failed to index votes table on block_hash: %v", err)
		}

	case ticketsTableBlockTimeUpgrade:
		log.Infof("Index the tickets table on Pool status...")
		if err = IndexTicketsTableOnPoolStatus(pgb.db); err != nil {
			return false, fmt.Errorf("failed to index tickets table: %v", err)
		}

	case addressesTableBlockTimeSortedIndex:
		log.Infof("Reindex the addresses table on block_time (sorted)...")
		if err = pgb.ReindexAddressesBlockTime(); err != nil {
			return false, fmt.Errorf("failed to reindex addresses table: %v", err)
		}
	}

	type dataTypeUpgrade struct {
		Column   string
		DataType string
	}

	var columnsUpdate []dataTypeUpgrade
	switch tableUpgrade {
	case addressesBlockTimeDataTypeUpdate, agendasBlockTimeDataTypeUpdate,
		vinsBlockTimeDataTypeUpdate:
		columnsUpdate = []dataTypeUpgrade{{Column: "block_time", DataType: "TIMESTAMP"}}
	case blocksBlockTimeDataTypeUpdate:
		columnsUpdate = []dataTypeUpgrade{{Column: "time", DataType: "TIMESTAMP"}}
	case transactionsBlockTimeDataTypeUpdate:
		columnsUpdate = []dataTypeUpgrade{
			{Column: "time", DataType: "TIMESTAMP"},
			{Column: "block_time", DataType: "TIMESTAMP"},
		}
	}

	if len(columnsUpdate) > 0 {
		for _, c := range columnsUpdate {
			log.Infof("Setting the %s table %s column data type to %s. Please wait...", tableName, c.Column, c.DataType)
			_, err := pgb.alterColumnDataType(tableName, c.Column, c.DataType)
			if err != nil {
				return false, fmt.Errorf("failed to set the %s data type to %s column in %s table: %v", tableName, c.Column, c.DataType, err)
			}
		}
	}

	return true, nil
}

// dropBlockTimeIndexes drops all indexes that are likely to slow down the db upgrade.
func (pgb *ChainDB) dropBlockTimeIndexes() {
	log.Info("Dropping block time indexes")
	err := DeindexBlockTimeOnTableAddress(pgb.db)
	if err != nil {
		log.Warnf("DeindexBlockTimeOnTableAddress failed: error: %v", err)
	}
}

// CreateBlockTimeIndexes creates back all the dropped indexes.
func (pgb *ChainDB) createBlockTimeIndexes() {
	log.Info("Creating the dropped block time indexes")
	err := IndexBlockTimeOnTableAddress(pgb.db)
	if err != nil {
		log.Warnf("IndexBlockTimeOnTableAddress failed: error: %v", err)
	}
}

func (pgb *ChainDB) handleAgendasVotingMilestonesUpgrade() (int64, error) {
	var errorLog = func(agenda, idType string, err error) {
		if err != nil {
			log.Warnf("%s height for agenda %s wasn't set:  %v", agenda, idType, err)
		}
	}

	var rowsUpdated int64
	for id, milestones := range VotingMilestones {
		for name, val := range map[string]int64{
			"activated":   milestones.Activated,
			"locked_in":   milestones.LockedIn,
			"hard_forked": milestones.HardForked,
		} {
			if val > 0 {
				query := fmt.Sprintf("UPDATE agendas SET %v=true WHERE block_height=$1 AND agenda_id=$2", name)
				_, err := pgb.db.Exec(query, val, id)
				errorLog(name, id, err)
				rowsUpdated++
			}
		}
	}
	return rowsUpdated, nil
}

func (pgb *ChainDB) handleVinsTableMainchainupgrade() (int64, error) {
	// The queries in this function should not timeout or (probably) canceled,
	// so use a background context.
	ctx := context.Background()

	// Get all of the block hashes
	log.Infof(" - Retrieving all block hashes...")
	blockHashes, err := RetrieveBlocksHashesAll(ctx, pgb.db)
	if err != nil {
		return 0, fmt.Errorf("unable to retrieve all block hashes: %v", err)
	}

	log.Infof(" - Updating vins data for each transactions in every block...")
	var rowsUpdated int64
	for i, blockHash := range blockHashes {
		vinDbIDsBlk, areValid, areMainchain, err := RetrieveTxnsVinsByBlock(ctx, pgb.db, blockHash)
		if err != nil {
			return 0, fmt.Errorf("unable to retrieve vin data for block %s: %v", blockHash, err)
		}

		numUpd, err := pgb.upgradeVinsMainchainForMany(vinDbIDsBlk, areValid, areMainchain)
		if err != nil {
			log.Warnf("unable to set valid/mainchain for vins: %v", err)
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

func (pgb *ChainDB) alterColumnDataType(table, column, dataType string) (int64, error) {
	query := "ALTER TABLE %s ALTER COLUMN %s TYPE %s USING to_timestamp(%s);"
	results, err := pgb.db.Exec(fmt.Sprintf(query, table, column, dataType, column))
	if err != nil {
		return -1, fmt.Errorf("alterColumnDataType failed: error %v", err)
	}

	c, err := results.RowsAffected()
	if err != nil {
		return -1, err
	}

	return c, nil
}

func (pgb *ChainDB) handleTxTypeHistogramUpgrade(bestBlock uint64, upgrade tableUpgradeType) (int64, error) {
	var i, diff uint64
	// diff is the payload process at once without using over using the memory
	diff = 50000
	var rowModified int64

	// Indexing the addresses table
	log.Infof("Indexing on addresses table just to hasten the update")
	_, err := pgb.db.Exec("CREATE INDEX xxxxx_histogram ON addresses(tx_vin_vout_row_id, is_funding)")
	if err != nil {
		log.Warnf("histogram upgrade maybe slower since indexing failed: %v", err)
	} else {
		defer func() {
			_, err = pgb.db.Exec("DROP INDEX xxxxx_histogram;")
			if err != nil {
				log.Warnf("droping the histogram index failed: %v", err)
			}
		}()
	}

	for i < bestBlock {
		if (bestBlock - i) < diff {
			diff = (bestBlock - i)
		}
		val := (i + 1)
		i += diff

		log.Infof(" - Retrieving data from txs table between height %d and %d ...", val, i)

		rows, err := pgb.db.Query(internal.SelectTxsVinsAndVoutsIDs, val, i)
		if err != nil {
			return 0, err
		}

		var dbIDs = make([]VinVoutTypeUpdateData, 0)
		for rows.Next() {
			rowIDs := VinVoutTypeUpdateData{}
			err = rows.Scan(&rowIDs.TxType, &rowIDs.VinsDbIDs, &rowIDs.VoutsDbIDs)
			if err != nil {
				return 0, err
			}

			dbIDs = append(dbIDs, rowIDs)
		}

		closeRows(rows)

		switch upgrade {
		case vinsTxHistogramUpgrade:
			log.Infof("Populating vins table with data between height %d and %d ...", val, i)

		case addressesTxHistogramUpgrade:
			log.Infof("Populating addresses table with data between height %d and %d ...", val, i)

		default:
			return 0, fmt.Errorf("unsupported upgrade found: %v", upgrade)
		}

		for _, rowIDs := range dbIDs {
			var vinsCount, voutsCount int64
			switch upgrade {
			case vinsTxHistogramUpgrade:
				vinsCount, err = pgb.updateVinsTxTypeHistogramUpgrade(rowIDs.TxType,
					rowIDs.VinsDbIDs)

			case addressesTxHistogramUpgrade:
				vinsCount, err = pgb.updateAddressesTxTypeHistogramUpgrade(rowIDs.TxType,
					false, rowIDs.VinsDbIDs)
				if err != nil {
					return 0, err
				}
				voutsCount, err = pgb.updateAddressesTxTypeHistogramUpgrade(rowIDs.TxType, true,
					rowIDs.VoutsDbIDs)
			}

			if err != nil {
				return 0, err
			}

			rowModified += (vinsCount + voutsCount)
		}

	}
	return rowModified, nil
}

func (pgb *ChainDB) updateVinsTxTypeHistogramUpgrade(txType stake.TxType,
	vinIDs dbtypes.UInt64Array) (int64, error) {
	var rowsUpdated int64

	// each vin
	for _, vinDbID := range vinIDs {
		result, err := pgb.db.Exec(internal.SetTxTypeOnVinsByVinIDs, txType, vinDbID)
		if err != nil {
			return 0, err
		}

		c, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}

		rowsUpdated += c
	}

	return rowsUpdated, nil
}

func (pgb *ChainDB) updateAddressesTxTypeHistogramUpgrade(txType stake.TxType,
	isFunding bool, vinVoutRowIDs dbtypes.UInt64Array) (int64, error) {
	var rowsUpdated int64

	// each address entry with a vin db row id or with vout db row id
	for _, rowDbID := range vinVoutRowIDs {
		result, err := pgb.db.Exec(internal.SetTxTypeOnAddressesByVinAndVoutIDs,
			txType, rowDbID, isFunding)
		if err != nil {
			return 0, err
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

// updateAllTicketsMainchain sets is_mainchain for all tickets according to
// their containing block.
func updateAllTicketsMainchain(db *sql.DB) (rowsUpdated int64, err error) {
	return sqlExec(db, internal.UpdateTicketsMainchainAll,
		"failed to update tickets and mainchain status")
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

// updateAddressesValidMainchainPatch selectively sets valid_mainchain for
// addresses table rows that are set incorrectly according to their
// corresponding transaction.
func updateAddressesValidMainchainPatch(db *sql.DB) (rowsUpdated int64, err error) {
	return sqlExec(db, internal.UpdateAddressesGloballyInvalid,
		"failed to update addresses rows valid_mainchain status")
}

// updateAddressesMatchingTxHashPatch selectively sets matching_tx_hash for
// addresses table rows that are set incorrectly according to their
// corresponding transaction.
func updateAddressesMatchingTxHashPatch(db *sql.DB) (rowsUpdated int64, err error) {
	return sqlExec(db, internal.UpdateAddressesFundingMatchingHash,
		"failed to update addresses rows matching_tx_hash")
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
			var progress, ok = VotingMilestones[val.ID]
			if !ok {
				log.Debugf("The Agenda ID: '%s' is unknown", val.ID)
				continue
			}

			var index, err = dbtypes.ChoiceIndexFromStr(val.Choice.Id)
			if err != nil {
				return 0, err
			}

			err = pgb.db.QueryRow(internal.MakeAgendaInsertStatement(false),
				val.ID, index, tx.TxID, tx.BlockHeight, tx.BlockTime.T,
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

func addVinsColumnsForHistogramUpgrade(db *sql.DB) (bool, error) {
	// The new columns and their data types
	newColumns := []newColumn{
		{"tx_type", "INT4", ""},
	}

	return addNewColumnsIfNotFound(db, "vins", newColumns)
}

func addAddressesColumnsForHistogramUpgrade(db *sql.DB) (bool, error) {
	// The new columns and their data types
	newColumns := []newColumn{
		{"tx_type", "INT4", ""},
	}
	return addNewColumnsIfNotFound(db, "addresses", newColumns)
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

func addTicketsColumnsForMainchain(db *sql.DB) (bool, error) {
	// The new columns and their data types
	newColumns := []newColumn{
		{"is_mainchain", "BOOLEAN", ""},
	}
	return addNewColumnsIfNotFound(db, "tickets", newColumns)
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

func addChainWorkColumn(db *sql.DB) (bool, error) {
	newColumns := []newColumn{
		{"chainwork", "TEXT", ""},
	}
	return addNewColumnsIfNotFound(db, "blocks", newColumns)
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

// verifyChainWork fetches and inserts missing chainwork values.
// This addresses a table update done at DB version 3.7.0.
func verifyChainWork(blockgate *rpcutils.BlockGate, db *sql.DB) (int64, error) {
	// Count rows with missing chainWork.
	var count int64
	countRow := db.QueryRow(`SELECT COUNT(hash) FROM blocks WHERE chainwork IS NULL;`)
	err := countRow.Scan(&count)
	if err != nil {
		log.Error("Failed to count null chainwork columns: %v", err)
		return 0, err
	}
	if count == 0 {
		return 0, nil
	}

	// Prepare the insertion statment. Parameters: 1. chainwork; 2. blockhash.
	stmt, err := db.Prepare(`UPDATE blocks SET chainwork=$1 WHERE hash=$2;`)
	if err != nil {
		log.Error("Failed to prepare chainwork insertion statement: %v", err)
		return 0, err
	}
	defer stmt.Close()

	// Grab the blockhashes from rows that don't have chainwork.
	rows, err := db.Query(`SELECT hash FROM blocks WHERE chainwork IS NULL;`)
	if err != nil {
		log.Error("Failed to query database for missing chainwork: %v", err)
		return 0, err
	}
	defer rows.Close()

	var hashStr string
	var updated int64 = 0
	client := blockgate.Client()
	tReport := time.Now().Unix()
	for rows.Next() {
		err = rows.Scan(&hashStr)
		if err != nil {
			log.Error("Failed to Scan null chainwork results. Aborting chainwork sync: %v", err)
			return updated, err
		}

		blockHash, err := chainhash.NewHashFromStr(hashStr)
		if err != nil {
			log.Errorf("Failed to parse hash from string %s. Aborting chainwork sync.: %v", hashStr, err)
			return updated, err
		}

		chainWork, err := rpcutils.GetChainWork(client, blockHash)
		if err != nil {
			log.Errorf("GetChainWork failed (%s). Aborting chainwork sync.: %v", hashStr, err)
			return updated, err
		}

		_, err = stmt.Exec(chainWork, hashStr)
		if err != nil {
			log.Errorf("Failed to insert chainwork (%s) for block %s. Aborting chainwork sync: %v", chainWork, hashStr, err)
			return updated, err
		}
		updated += 1
		// Every two minutes, report the sync status.
		if updated%100 == 0 && time.Now().Unix()-tReport > 120 {
			tReport = time.Now().Unix()
			log.Infof("Chainwork sync is %.1f%% complete.", float64(updated)/float64(count)*100)
		}
	}

	log.Info("Chainwork sync complete.")
	return updated, nil
}
