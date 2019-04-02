// Copyright (c) 2018-2019, The Decred developers
// See LICENSE for details.

package dcrpg

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/rpcclient/v2"
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
	blocksRemoveRootColumns
	timestamptzUpgrade
	agendasTablePruningUpdate
	agendaVotesTableCreationUpdate
	votesTableBlockTimeUpdate
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

// votingMilestones defines the various milestones phases have taken place when
// the agendas have been up for voting. Only sdiffalgorithm, lnsupport and
// lnfeatures agenda ids exist since appropriate voting mechanisms on the dcrd
// level are not yet fully implemented.
var votingMilestones = map[string]dbtypes.MileStone{}

// toVersion defines a table version to which the pg tables will commented to.
var toVersion TableVersion

// UpgradeTables upgrades all the tables with the pending updates from the
// current table versions to the most recent table version supported. A boolean
// is returned to indicate if the db upgrade was successfully completed.
func (pgb *ChainDB) UpgradeTables(dcrdClient *rpcclient.Client,
	version, needVersion TableVersion) (bool, error) {
	// If the previous DB is between the 3.1/3.2 and 4.0 releases (dcrpg table
	// versions >3.5.5 and <3.9.0), an upgrade is likely not possible IF PostgreSQL
	// was running in a TimeZone other than UTC. Deny upgrade.
	if version.major == 3 && ((version.minor > 5 && version.minor < 9) ||
		(version.minor == 5 && version.patch > 5)) {
		dataType, err := CheckColumnDataType(pgb.db, "blocks", "time")
		if err != nil {
			return false, fmt.Errorf("failed to retrieve data_type for blocks.time: %v", err)
		}
		if dataType == "timestamp without time zone" {
			// Timestamp columns are timestamp without time zone.
			defaultTZ, _, err := CheckDefaultTimeZone(pgb.db)
			if err != nil {
				return false, fmt.Errorf("failed in CheckDefaultTimeZone: %v", err)
			}
			if defaultTZ != "UTC" {
				// Postgresql time zone is not UTC, which is bad when the data
				// type throws away time zone offset info, as is the case with
				// TIMESTAMP. If the default time zone were UTC, there is likely
				// no time stamp issue.
				return false, fmt.Errorf(
					"your dcrpg schema (v%v) has time columns without time zone, "+
						"AND the PostgreSQL default/local time zone is %s. "+
						"You must rebuild the databases from scratch!!!",
					version, defaultTZ)
			}
		}
	}

	// Apply each upgrade in succession
	switch {
	// Upgrade from 3.1.0 --> 3.2.0
	case version.major == 3 && version.minor == 1 && version.patch == 0:
		toVersion = TableVersion{3, 2, 0}

		theseUpgrades := []TableUpgradeType{
			{"agendas", agendasTableUpgrade},
			{"vins", vinsTableCoinSupplyUpgrade},
		}

		isSuccess, er := pgb.initiatePgUpgrade(dcrdClient, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.2.0 --> 3.3.0
	case version.major == 3 && version.minor == 2 && version.patch == 0:
		toVersion = TableVersion{3, 3, 0}

		// The order of these upgrades is critical
		theseUpgrades := []TableUpgradeType{
			{"blocks", blocksTableMainchainUpgrade}, // also patches block_chain
			{"transactions", transactionsTableMainchainUpgrade},
			{"vins", vinsTableMainchainUpgrade},
			{"addresses", addressesTableMainchainUpgrade},
			{"votes", votesTableMainchainUpgrade},
		}

		isSuccess, er := pgb.initiatePgUpgrade(dcrdClient, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.3.0 --> 3.4.0
	case version.major == 3 && version.minor == 3 && version.patch == 0:
		toVersion = TableVersion{3, 4, 0}

		// The order of these upgrades is critical
		theseUpgrades := []TableUpgradeType{
			{"tickets", ticketsTableMainchainUpgrade},
		}

		isSuccess, er := pgb.initiatePgUpgrade(dcrdClient, theseUpgrades)
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
		toVersion = TableVersion{3, 5, 5} // dcrdata 3.1 release

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

		isSuccess, er := pgb.initiatePgUpgrade(dcrdClient, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.7.0 --> 3.8.0
	case version.major == 3 && version.minor == 7 && version.patch == 0:
		toVersion = TableVersion{3, 8, 0}

		theseUpgrades := []TableUpgradeType{
			{"blocks", blocksRemoveRootColumns},
		}

		isSuccess, er := pgb.initiatePgUpgrade(nil, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.8.0 --> 3.9.0
	case version.major == 3 && version.minor == 8 && version.patch == 0:
		toVersion = TableVersion{3, 9, 0}

		theseUpgrades := []TableUpgradeType{
			{"all", timestamptzUpgrade},
		}

		isSuccess, er := pgb.initiatePgUpgrade(nil, theseUpgrades)
		if !isSuccess {
			return isSuccess, er
		}

		// Go on to next upgrade
		fallthrough

	// Upgrade from 3.9.0 --> 3.10.0
	case version.major == 3 && version.minor == 9 && version.patch == 0:
		toVersion = TableVersion{3, 10, 0}

		theseUpgrades := []TableUpgradeType{
			{"agenda_votes", agendaVotesTableCreationUpdate},
			{"votes", votesTableBlockTimeUpdate},
			// Should be the last to run.
			{"agendas", agendasTablePruningUpdate},
		}

		// chainInfo is needed for this upgrade.
		rawChainInfo, err := dcrdClient.GetBlockChainInfo()
		if err != nil {
			return false, err
		}
		pgb.UpdateChainState(rawChainInfo)

		isSuccess, er := pgb.initiatePgUpgrade(dcrdClient, theseUpgrades)
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

	upgradeFailed := fmt.Errorf("failed to upgrade tables to required version %v",
		needVersion)

	upgradeInfo := TableUpgradesRequired(TableVersions(pgb.db))
	// If no upgrade information exists return an error
	if len(upgradeInfo) == 0 {
		return false, upgradeFailed
	}

	// Ensure that the required version was reached for all the table upgrades
	// otherwise return an error.
	for _, info := range upgradeInfo {
		if info.UpgradeType != OK {
			return false, upgradeFailed
		}
	}

	// Unsupported upgrades are caught by the default case, so we've succeeded.
	log.Infof("Table upgrades completed.")
	return true, nil
}

// initiatePgUpgrade starts the specific auxiliary database.
func (pgb *ChainDB) initiatePgUpgrade(client *rpcclient.Client, theseUpgrades []TableUpgradeType) (bool, error) {
	for it := range theseUpgrades {
		upgradeSuccess, err := pgb.handleUpgrades(client, theseUpgrades[it].upgradeType)
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
func (pgb *ChainDB) handleUpgrades(client *rpcclient.Client,
	tableUpgrade tableUpgradeType) (bool, error) {
	var err error
	var startHeight int64
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
		//  startHeight is set to a block height of 128000 since that is when
		//  the first vote for a mainnet agenda was cast. For vins upgrades
		//  (coin supply and mainchain), startHeight is not set since all the
		//  blocks are considered.
		startHeight = 128000
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
	case blocksRemoveRootColumns:
		tableReady, err = deleteRootsColumns(pgb.db)
		tableName, upgradeTypeStr = "blocks", "remove columns: extra_data, merkle_root, stake_root, final_state"
	case timestamptzUpgrade:
		tableReady = true
		tableName, upgradeTypeStr = "every", "convert timestamp columns to timestamptz type"
	case agendasTablePruningUpdate:
		// StakeValidationHeight defines the height from where votes transactions
		// exists as from. They only exist after block 4090 on mainnet.
		startHeight = pgb.chainParams.StakeValidationHeight
		tableReady, err = pruneAgendasTable(pgb.db)
		tableName, upgradeTypeStr = "agendas", "prune agendas table"
	case agendaVotesTableCreationUpdate:
		tableReady, err = createNewAgendaVotesTable(pgb.db)
		tableName, upgradeTypeStr = "agendas", "create agenda votes table"
	case votesTableBlockTimeUpdate:
		tableReady, err = addVotesBlockTimeColumn(pgb.db)
		tableName, upgradeTypeStr = "votes", "new block_time column"
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
	case vinsTableCoinSupplyUpgrade, agendasTableUpgrade, agendasTablePruningUpdate:
		// height is the best block where this table upgrade should stop at.
		height, err := pgb.HeightDB()
		if err != nil {
			return false, err
		}
		log.Infof("found the best block at height: %v", height)

		// For each block on the main chain, perform upgrade operations.
		for i := startHeight; i <= height; i++ {
			block, _, err := rpcutils.GetBlock(i, client)
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
			case agendasTablePruningUpdate:
				rows, err = pgb.handleAgendaAndAgendaVotesTablesUpgrade(msgBlock)
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
		var height int64
		// height is the best block where this table upgrade should stop at.
		height, err = pgb.HeightDB()
		if err != nil {
			return false, err
		}

		log.Infof("found the best block at height: %v", height)
		rowsUpdated, err = pgb.handleTxTypeHistogramUpgrade(uint64(height), tableUpgrade)

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

	case timestamptzUpgrade:
		log.Infof("Converting all TIMESTAMP columns to TIMESTAMPTZ...")
		err = handleConvertTimeStampTZ(pgb.db)

	case addressesBlockTimeDataTypeUpdate, blocksBlockTimeDataTypeUpdate,
		agendasBlockTimeDataTypeUpdate, transactionsBlockTimeDataTypeUpdate,
		vinsBlockTimeDataTypeUpdate, blocksRemoveRootColumns,
		agendaVotesTableCreationUpdate, votesTableBlockTimeUpdate:
		// agendaVotesTableCreationUpdate and votesTableBlockTimeUpdate dbs
		// population is fully handled by agendasTablePruningUpdate

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

	case agendasTablePruningUpdate:
		log.Infof("Index the agendas table on Agenda ID...")
		if err = IndexAgendasTableOnAgendaID(pgb.db); err != nil {
			return false, fmt.Errorf("failed to index agendas table: %v", err)
		}

	case agendaVotesTableCreationUpdate:
		log.Infof("Index the agenda votes table on Agenda ID...")
		if err = IndexAgendaVotesTableOnAgendaID(pgb.db); err != nil {
			return false, fmt.Errorf("failed to index agenda votes table: %v", err)
		}

	case votesTableBlockTimeUpdate:
		log.Infof("Index the votes table on Block Time...")
		if err = IndexVotesTableOnBlockTime(pgb.db); err != nil {
			return false, fmt.Errorf("failed to index votes table on Block Time: %v", err)
		}

		log.Infof("Index the votes table on Block Height...")
		if err = IndexVotesTableOnHeight(pgb.db); err != nil {
			return false, fmt.Errorf("failed to index votes table on Height: %v", err)
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
		columnsUpdate = []dataTypeUpgrade{{Column: "block_time", DataType: "TIMESTAMPTZ"}}
	case blocksBlockTimeDataTypeUpdate:
		columnsUpdate = []dataTypeUpgrade{{Column: "time", DataType: "TIMESTAMPTZ"}}
	case transactionsBlockTimeDataTypeUpdate:
		columnsUpdate = []dataTypeUpgrade{
			{Column: "time", DataType: "TIMESTAMPTZ"},
			{Column: "block_time", DataType: "TIMESTAMPTZ"},
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

// dropBlockTimeIndexes drops all indexes that are likely to slow down the db
// upgrade.
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
	errorLog := func(agenda, idType string, err error) bool {
		if err != nil {
			log.Warnf("%s height for agenda %s wasn't set:  %v", agenda, idType, err)
			return true
		}
		return false
	}

	var rowsUpdated int64
	for id, milestones := range votingMilestones {
		for name, val := range map[string]int64{
			"activated":   milestones.Activated,
			"locked_in":   milestones.VotingDone,
			"hard_forked": milestones.HardForked,
		} {
			if val > 0 {
				query := fmt.Sprintf("UPDATE agendas SET %v=true WHERE block_height=$1 AND agenda_id=$2", name)
				_, err := pgb.db.Exec(query, val, id)
				if errorLog(name, id, err) {
					return rowsUpdated, err
				}
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
			return rowsUpdated, err
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
			return rowsUpdated, fmt.Errorf("db ID %d not found: %v", vinDbID, err)
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
				closeRows(rows)
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
			var progress, ok = votingMilestones[val.ID]
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
				progress.VotingDone == tx.BlockHeight,
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

// handleAgendaAndAgendaVotesTableUpgrade restructures the agendas table, creates
// a new agenda_votes table and adds a new block time column to votes table.
func (pgb *ChainDB) handleAgendaAndAgendaVotesTablesUpgrade(msgBlock *wire.MsgBlock) (int64, error) {
	// neither isValid or isMainchain are important
	dbTxns, _, _ := dbtypes.ExtractBlockTransactions(msgBlock,
		wire.TxTreeStake, pgb.chainParams, true, false)

	var rowsUpdated int64
	query := `UPDATE votes SET block_time = $3 WHERE tx_hash =$1` +
		` AND block_hash = $2 RETURNING id;`

	for i, tx := range dbTxns {
		if tx.TxType != int16(stake.TxTypeSSGen) {
			continue
		}

		var voteRowID int64
		err := pgb.db.QueryRow(query, tx.TxID, tx.BlockHash, tx.BlockTime).Scan(&voteRowID)
		if err != nil || voteRowID == 0 {
			return -1, err
		}
		_, _, _, choices, err := txhelpers.SSGenVoteChoices(msgBlock.STransactions[i],
			pgb.chainParams)
		if err != nil {
			return -1, err
		}

		var rowID uint64
		chainInfo := pgb.ChainInfo()
		for _, val := range choices {
			// check if agendas row id is not set then save the agenda details.
			progress := chainInfo.AgendaMileStones[val.ID]
			if progress.ID == 0 {
				err = pgb.db.QueryRow(internal.MakeAgendaInsertStatement(false),
					val.ID, progress.Status, progress.VotingDone, progress.Activated,
					progress.HardForked).Scan(&progress.ID)
				if err != nil {
					return -1, err
				}
				chainInfo.AgendaMileStones[val.ID] = progress
			}

			var index, err = dbtypes.ChoiceIndexFromStr(val.Choice.Id)
			if err != nil {
				return -1, err
			}

			err = pgb.db.QueryRow(internal.MakeAgendaVotesInsertStatement(false),
				voteRowID, progress.ID, index).Scan(&rowID)
			if err != nil {
				return -1, err
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

func handleConvertTimeStampTZ(db *sql.DB) error {
	timestampTables := []struct {
		TableName   string
		ColumnNames []string
	}{
		{
			"blocks",
			[]string{"time"},
		},
		{
			"addresses",
			[]string{"block_time"},
		},
		{
			"vins",
			[]string{"block_time"},
		},
		{
			"agendas",
			[]string{"block_time"},
		},
		{
			"transactions",
			[]string{"block_time", "time"},
		},
	}

	// Old times were stored as a "timestamp without time zone", and the system
	// time zone was set to local (which may not be UTC). Conversion must be
	// done in that zone to introduce the correct offsets when computing the UTC
	// times for the TIMESTAMPTZ values.
	_, err := db.Exec(`SET TIME ZONE DEFAULT`)
	if err != nil {
		return fmt.Errorf("failed to set time zone to UTC: %v", err)
	}

	// Convert the affected columns of each table.
	for i := range timestampTables {
		// Convert the timestamp columns of this table.
		tableName := timestampTables[i].TableName
		for _, col := range timestampTables[i].ColumnNames {
			log.Infof("Converting column %s.%s to TIMESTAMPTZ...", tableName, col)
			_, err = db.Exec(fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s TYPE TIMESTAMPTZ`,
				tableName, col))
			if err != nil {
				return fmt.Errorf("failed to convert %s.%s to TIMESTAMPTZ: %v",
					tableName, col, err)
			}
		}
	}

	// Switch server time zone for this sesssion back to UTC to read correct
	// time stamps.
	if _, err = db.Exec(`SET TIME ZONE UTC`); err != nil {
		return fmt.Errorf("failed to set time zone to UTC: %v", err)
	}

	return nil
}

func pruneAgendasTable(db *sql.DB) (bool, error) {
	isSuccess, err := dropTableIfExists(db, "agendas")
	if !isSuccess {
		return isSuccess, err
	}

	return runQuery(db, internal.CreateAgendasTable)
}

func createNewAgendaVotesTable(db *sql.DB) (bool, error) {
	return runQuery(db, internal.CreateAgendaVotesTable)
}

func runQuery(db *sql.DB, sqlQuery string) (bool, error) {
	_, err := db.Exec(sqlQuery)
	if err != nil {
		return false, err
	}
	return true, nil
}

func dropTableIfExists(db *sql.DB, table string) (bool, error) {
	dropStmt := fmt.Sprintf("DROP TABLE IF EXISTS %s;", table)
	_, err := db.Exec(dropStmt)
	if err != nil {
		return false, err
	}
	return true, nil
}

func makeDeleteColumnsStmt(table string, columns []string) string {
	dropStmt := fmt.Sprintf("ALTER TABLE %s", table)
	for i := range columns {
		dropStmt += fmt.Sprintf(" DROP COLUMN IF EXISTS %s", columns[i])
		if i < len(columns)-1 {
			dropStmt += ","
		}
	}
	dropStmt += ";"
	return dropStmt
}

func deleteColumnsIfFound(db *sql.DB, table string, columns []string) (bool, error) {
	dropStmt := makeDeleteColumnsStmt(table, columns)
	_, err := db.Exec(dropStmt)
	if err != nil {
		return false, err
	}
	log.Infof("Reclaiming disk space from removed columns...")
	_, err = db.Exec(fmt.Sprintf("VACUUM FULL %s;", table))
	if err != nil {
		return false, err
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
		{"chainwork", "TEXT", "0"},
	}
	return addNewColumnsIfNotFound(db, "blocks", newColumns)
}

func deleteRootsColumns(db *sql.DB) (bool, error) {
	table := "blocks"
	cols := []string{"merkle_root", "stake_root", "final_state", "extra_data"}
	return deleteColumnsIfFound(db, table, cols)
}

func addVotesBlockTimeColumn(db *sql.DB) (bool, error) {
	newColumns := []newColumn{
		{"block_time", "TIMESTAMPTZ", ""},
	}
	return addNewColumnsIfNotFound(db, "votes", newColumns)
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
func verifyChainWork(client *rpcclient.Client, db *sql.DB) (int64, error) {
	// Count rows with missing chainWork.
	var count int64
	countRow := db.QueryRow(`SELECT COUNT(hash) FROM blocks WHERE chainwork = '0';`)
	err := countRow.Scan(&count)
	if err != nil {
		log.Error("Failed to count null chainwork columns: %v", err)
		return 0, err
	}
	if count == 0 {
		return 0, nil
	}

	// Prepare the insertion statement. Parameters: 1. chainwork; 2. blockhash.
	stmt, err := db.Prepare(`UPDATE blocks SET chainwork=$1 WHERE hash=$2;`)
	if err != nil {
		log.Error("Failed to prepare chainwork insertion statement: %v", err)
		return 0, err
	}
	defer stmt.Close()

	// Grab the blockhashes from rows that don't have chainwork.
	rows, err := db.Query(`SELECT hash, is_mainchain FROM blocks WHERE chainwork = '0';`)
	if err != nil {
		log.Error("Failed to query database for missing chainwork: %v", err)
		return 0, err
	}
	defer rows.Close()

	var updated int64
	tReport := time.Now()
	for rows.Next() {
		var hashStr string
		var isMainchain bool
		err = rows.Scan(&hashStr, &isMainchain)
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
			// If it's an orphaned block, it may not be in dcrd database. OK to skip.
			if strings.HasPrefix(err.Error(), "-5: Block not found") {
				if isMainchain {
					// Although every mainchain block should have a corresponding
					// blockNode and chainwork value in drcd, this upgrade is run before
					// the chain is synced, so it's possible an orphaned block is still
					// marked mainchain.
					log.Warnf("No chainwork found for mainchain block %s. Skipping.", hashStr)
				}
				updated++
				continue
			}
			log.Errorf("GetChainWork failed (%s). Aborting chainwork sync.: %v", hashStr, err)
			return updated, err
		}

		_, err = stmt.Exec(chainWork, hashStr)
		if err != nil {
			log.Errorf("Failed to insert chainwork (%s) for block %s. Aborting chainwork sync: %v", chainWork, hashStr, err)
			return updated, err
		}
		updated++
		// Every two minutes, report the sync status.
		if updated%100 == 0 && time.Since(tReport) > 2*time.Minute {
			tReport = time.Now()
			log.Infof("Chainwork sync is %.1f%% complete.", float64(updated)/float64(count)*100)
		}
	}

	log.Info("Chainwork sync complete.")
	return updated, nil
}
