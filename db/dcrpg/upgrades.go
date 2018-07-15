// Copyright (c) 2018, The Decred developers
// See LICENSE for details.

package dcrpg

import (
	"bytes"
	"database/sql"
	"fmt"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/db/dcrpg/internal"
	"github.com/decred/dcrdata/rpcutils"
	"github.com/decred/dcrdata/txhelpers"
)

// tableUpgradeTypes defines the types of upgrades that currently exists and
// happen automatically. This upgrade run on normal start up the first the
// style is run after updating dcrdata past version 3.0.0.
type tableUpgradeTypes int

const (
	vinsTableCoinSuppleUpgrade tableUpgradeTypes = iota
	agendasTableUpgrade
	vinsTableMainchainUpgrade
	blocksTableMainchainUpgrade
	transactionsTableMainchainUpgrade
	addressesTableMainchainUpgrade
)

// CheckForAuxDBUpgrade checks if an upgrade is required and currently supported.
// A boolean value is returned to indicate if the db upgrade was
// successfully completed.
func (pgb *ChainDB) CheckForAuxDBUpgrade(dcrdClient *rpcclient.Client) (bool, error) {
	var (
		version     = TableVersion{}
		upgradeInfo = TableUpgradesRequired(TableVersions(pgb.db))
	)

	if len(upgradeInfo) > 0 {
		version = upgradeInfo[0].RequiredVer
	} else {
		return false, nil
	}

	switch {
	case upgradeInfo[0].UpgradeType != "upgrade":
		return false, nil

	// Upgrading from 3.1.0 --> 3.2.0
	case version.major == 3 && version.minor == 1 && version.patch == 0:
		smartClient := rpcutils.NewBlockGate(dcrdClient, 10)

		isAgendasUpgrade, err := pgb.handleUpgrades(smartClient, agendasTableUpgrade)
		if err != nil {
			return false, err
		}

		isVinsUpgraded, err := pgb.handleUpgrades(smartClient, vinsTableCoinSuppleUpgrade)
		if err != nil {
			return false, err
		}

		// If no upgrade took place, table versioning should not
		// proceed and an error should be returned.
		if !isAgendasUpgrade && !isVinsUpgraded {
			return false, fmt.Errorf("Aux db upgrade for version %s does not exist yet", version)
		}

		// This upgrade bumps patch.
		versionAllTables(pgb.db, TableVersion{3, 2, 0})
		// Go on to next upgrade
		fallthrough

	// Upgrading from 3.2.0 --> 3.3.0
	case version.major == 3 && version.minor == 2 && version.patch == 0:
		smartClient := rpcutils.NewBlockGate(dcrdClient, 10)
		isVinsMainchainUpgraded, err := pgb.handleUpgrades(smartClient, vinsTableMainchainUpgrade)
		if err != nil {
			return false, err
		}

		if !isVinsMainchainUpgraded {
			return false, fmt.Errorf("Aux db upgrade for version %s does not exist yet", version)
		}

		return true, versionAllTables(pgb.db, version)
	}

	return false, nil
}

// handleUpgrades the individual upgrade and returns a bool and an error
// indicating if the upgrade was successful or not.
func (pgb *ChainDB) handleUpgrades(client *rpcutils.BlockGate,
	tableUpgrade tableUpgradeTypes) (bool, error) {
	// height is the best block where this table upgrade should stop at.
	height, err := pgb.HeightDB()
	if err != nil {
		return false, err
	}

	log.Infof("Found the best block at height: %v", height)

	// For the agendas upgrade, i is set to a block height of 128000 since that
	// is when the first vote for an agenda was cast. For vins upgrades (coin
	// supply and mainchain), i is not set since all the blocks are considered.
	var i uint64
	var columnsAdded bool
	var tableName, upgradeTypeStr string
	switch tableUpgrade {
	case vinsTableCoinSuppleUpgrade:
		columnsAdded, err = addVinsColumnsForCoinSupply(pgb.db)
		tableName, upgradeTypeStr = "vins", "New Columns"
	case vinsTableMainchainUpgrade:
		columnsAdded, err = addVinsColumnsForMainchain(pgb.db)
		tableName, upgradeTypeStr = "vins", "New Columns"
	case blocksTableMainchainUpgrade:
		columnsAdded, err = addBlocksColumnsForMainchain(pgb.db)
		tableName, upgradeTypeStr = "blocks", "New Columns"
	case addressesTableMainchainUpgrade:
		columnsAdded, err = addAddressesColumnsForMainchain(pgb.db)
		tableName, upgradeTypeStr = "addresses", "New Columns"
	case transactionsTableMainchainUpgrade:
		columnsAdded, err = addTransactionsColumnsForMainchain(pgb.db)
		tableName, upgradeTypeStr = "transactions", "New Columns"
	case agendasTableUpgrade:
		columnsAdded, err = haveEmptyAgendasTable(pgb.db)
		tableName, upgradeTypeStr = "agendas", "New Table"
		i = 128000
	default:
		return false, fmt.Errorf(`upgrade "%v" is unknown`, tableUpgrade)
	}

	// Ensure new columns were added successfully.
	if !columnsAdded {
		return false, err
	}

	var rowsUpdated int64

	switch tableUpgrade {
	case vinsTableCoinSuppleUpgrade, agendasTableUpgrade, vinsTableMainchainUpgrade,
		transactionsTableMainchainUpgrade, addressesTableMainchainUpgrade:
		// Fetch the block associated with the provided block height
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
				log.Infof("Upgrading the %s table (%s Upgrade) from height %v to %v ",
					tableName, upgradeTypeStr, i, limit-1)
			}

			var rows int64
			var msgBlock = block.MsgBlock()

			switch tableUpgrade {
			case vinsTableCoinSuppleUpgrade:
				rows, err = pgb.handlevinsTableCoinSuppleUpgrade(msgBlock)
			case agendasTableUpgrade:
				rows, err = pgb.handleAgendasTableUpgrade(msgBlock)
			}
			if err != nil {
				return false, err
			}

			rowsUpdated += rows
		}

	case blocksTableMainchainUpgrade:
		// This proceeds from best block back to genesis
		block, err := client.BestBlock()
		if err != nil {
			return false, err
		}
		rowsUpdated, err = pgb.handleBlocksTableMainchainUpgrade(block.Hash().String())
		if err != nil {
			return false, fmt.Errorf(`upgrade of blocks table ended prematurely at %d`, rowsUpdated)
		}
	default:
		return false, fmt.Errorf(`upgrade "%v" unknown`, tableUpgrade)
	}

	log.Infof(" %v rows in %s table (%s Upgrade) were successfully upgraded.",
		rowsUpdated, tableName, upgradeTypeStr)

	switch tableUpgrade {
	case vinsTableCoinSuppleUpgrade:
		log.Infof("Index the agendas table on Agenda ID...")
		IndexAgendasTableOnAgendaID(pgb.db)

		log.Infof("Index the agendas table on Block Time...")
		IndexAgendasTableOnBlockTime(pgb.db)
	case agendasTableUpgrade:
	}

	return true, nil
}

func (pgb *ChainDB) upgradeVinsMainchainForMany(vinDbIDs []dbtypes.UInt64Array,
	areValid, areMainchain []bool) (int64, error) {
	var rowsUpdated int64
	// each transaction
	for it, vs := range vinDbIDs {
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

func (pgb *ChainDB) handleBlocksTableMainchainUpgrade(bestBlock string) (int64, error) {
	var blocksUpdated int64
	previousHash, thisBlockHash := bestBlock, bestBlock
	for !bytes.Equal(zeroHashStringBytes, []byte(previousHash)) {
		// set is_mainchain=1 and get previous_hash
		var err error
		previousHash, err = SetMainchainByBlockHash(pgb.db, thisBlockHash)
		if err != nil {
			return blocksUpdated, err
		}
		// patch block_chain table
		err = UpdateBlockNextByHash(pgb.db, previousHash, thisBlockHash)
		if err != nil {
			log.Errorf("Failed to update next_hash in block_chain for block %s", previousHash)
		}

		thisBlockHash = previousHash
		blocksUpdated++
	}
	return blocksUpdated, nil
}

func (pgb *ChainDB) handleTransactionsTableMainchainUpgrade() (int64, error) {
	return UpdateAllTxnsValidMainchain(pgb.db)
}

// handlevinsTableCoinSuppleUpgrade implements the upgrade to the new newly added columns
// in the vins table. The new columns are mainly used for the coin supply chart.
// If all the new columns are not added, quit the db upgrade.
func (pgb *ChainDB) handlevinsTableCoinSuppleUpgrade(msgBlock *wire.MsgBlock) (int64, error) {
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

// addNewColumnsIfNotFound checks if the new columns already exist and adds
// them if they are missing. If any of the expected new columns exist the the
// upgrade will not proceed.
func addNewColumnsIfNotFound(db *sql.DB, table string, newColumns map[string]string) (bool, error) {
	for col, dataType := range newColumns {
		var isRowFound bool
		err := db.QueryRow(`SELECT EXISTS( SELECT column_name FROM INFORMATION_SCHEMA.COLUMNS 
			WHERE table_name = '$1' AND column_name = $2 );`, table, col).Scan(&isRowFound)
		if err != nil {
			return false, err
		}

		if isRowFound {
			return false, nil
		}

		result, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;",
			table, col, dataType))
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
	newColumns := map[string]string{
		"is_valid":   "BOOLEAN",
		"block_time": "INT8",
		"value_in":   "INT8",
	}
	return addNewColumnsIfNotFound(db, "vins", newColumns)
}

func addVinsColumnsForMainchain(db *sql.DB) (bool, error) {
	// The new columns and their data types
	newColumns := map[string]string{
		"is_mainchain": "BOOLEAN",
	}
	return addNewColumnsIfNotFound(db, "vins", newColumns)
}

func addBlocksColumnsForMainchain(db *sql.DB) (bool, error) {
	// The new columns and their data types
	newColumns := map[string]string{
		"is_mainchain": "BOOLEAN",
	}
	return addNewColumnsIfNotFound(db, "blocks", newColumns)
}

func addTransactionsColumnsForMainchain(db *sql.DB) (bool, error) {
	// The new columns and their data types
	newColumns := map[string]string{
		"is_valid":     "BOOLEAN",
		"is_mainchain": "BOOLEAN",
	}
	return addNewColumnsIfNotFound(db, "transactions", newColumns)
}

func addAddressesColumnsForMainchain(db *sql.DB) (bool, error) {
	// The new columns and their data types
	newColumns := map[string]string{
		"valid_mainchain": "BOOLEAN",
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
