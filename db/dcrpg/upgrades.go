// Copyright (c) 2018, The Decred developers
// See LICENSE for details.

package dcrpg

import (
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
	vinsTableUpgrade tableUpgradeTypes = iota
	agendasTableUpgrade
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

	// When the required table version is 3.x.0 where x is greater than or equal to 1
	case version.major >= 3 && version.minor >= 1 && version.patch == 0:
		smartClient := rpcutils.NewBlockGate(dcrdClient, 10)

		isVinsUpgraded, err := pgb.handleUpgrades(smartClient, agendasTableUpgrade)
		if err != nil {
			return false, err
		}

		isAgendasUpgrade, err := pgb.handleUpgrades(smartClient, vinsTableUpgrade)
		if err != nil {
			return false, err
		}

		// If no upgrade took place, table versioning should not
		// proceed and an error should be returned.
		if !isAgendasUpgrade && !isVinsUpgraded {
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
	var isSuccess bool
	var i uint64
	var rowsUpdated int64
	var tableName string
	var upgradeTypeStr string

	// height is the best block where this table upgrade should stop at.
	var height, err = pgb.HeightDB()
	if err != nil {
		return false, err
	}

	log.Infof("Found the best block at height: %v", height)

	// For the agendas upgrade i is set to a block height of 128000 since its
	// when the first vote for an agenda was cast. For vins upgrade (coin supply)
	// i should is not set since all the blocks are considered.
	switch tableUpgrade {
	case vinsTableUpgrade:
		isSuccess, err = addNewColumnsIfNotFound(pgb.db)
		tableName, upgradeTypeStr = "vins", "New Columns"
	case agendasTableUpgrade:
		isSuccess, err = haveEmptyAgendasTable(pgb.db)
		tableName, upgradeTypeStr, i = "agendas", "New Table", 128000
	default:
		return false, fmt.Errorf("Upgrade provided is '%v' unknown", tableUpgrade)
	}

	// If isSuccess is false this upgrade should not proceed whether the error
	// is nil or not.
	if !isSuccess {
		return false, err
	}

	// Fetch the block associated with the provided block height
	for ; i <= height; i++ {
		var block, err = client.UpdateToBlock(int64(i))
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
		case vinsTableUpgrade:
			rows, err = pgb.handleVinsTableUpgrade(msgBlock)
		case agendasTableUpgrade:
			rows, err = pgb.handleAgendasTableUpgrade(msgBlock)
		}
		if err != nil {
			return false, err
		}

		rowsUpdated += rows
	}

	log.Infof(" %v rows in %s table (%s Upgrade) were successfully upgraded.",
		rowsUpdated, tableName, upgradeTypeStr)

	switch tableUpgrade {
	case vinsTableUpgrade:
		log.Infof("Index the agendas table on Agenda ID...")
		IndexAgendasTableOnAgendaID(pgb.db)

		log.Infof("Index the agendas table on Block Time...")
		IndexAgendasTableOnBlockTime(pgb.db)
	case agendasTableUpgrade:
	}

	return true, nil
}

// handleVinsTableUpgrade implements the upgrade to the new newly added columns
// in the vins table. The new columns are mainly used for the coin supply chart.
// If all the new columns are not added, quit the db upgrade.
func (pgb *ChainDB) handleVinsTableUpgrade(msgBlock *wire.MsgBlock) (int64, error) {
	var isValid bool
	var rowsUpdated int64

	var err = pgb.db.QueryRow(`SELECT is_valid FROM blocks WHERE hash = $1 ;`,
		msgBlock.BlockHash().String()).Scan(&isValid)
	if err != nil {
		return 0, err
	}

	_, _, stakedDbTxVins := dbtypes.ExtractBlockTransactions(
		msgBlock, wire.TxTreeStake, pgb.chainParams, isValid)
	_, _, regularDbTxVins := dbtypes.ExtractBlockTransactions(
		msgBlock, wire.TxTreeRegular, pgb.chainParams, isValid)
	dbTxVins := append(stakedDbTxVins, regularDbTxVins...)

	for _, v := range dbTxVins {
		for _, s := range v {
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
	var rowsUpdated int64
	var milestones = map[string]dbtypes.MileStone{
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

	var dbTxns, _, _ = dbtypes.ExtractBlockTransactions(msgBlock,
		wire.TxTreeStake, pgb.chainParams, true)

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
// them if they are missing. If any of the expected new columns exist the
// the upgrade will not proceed.
func addNewColumnsIfNotFound(db *sql.DB) (bool, error) {
	for name, dataType := range map[string]string{
		"is_valid": "BOOLEAN", "block_time": "INT8", "value_in": "INT8"} {
		var isRowFound bool

		err := db.QueryRow(`SELECT EXISTS( SELECT column_name FROM INFORMATION_SCHEMA.COLUMNS 
			WHERE table_name = 'vins' AND column_name = $1 );`, name).Scan(&isRowFound)
		if err != nil {
			return false, err
		}

		if isRowFound {
			return false, nil
		}

		result, err := db.Exec(fmt.Sprintf("ALTER TABLE vins ADD COLUMN %s %s ;", name, dataType))
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
