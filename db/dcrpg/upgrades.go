// Copyright (c) 2018, The Decred developers
// See LICENSE for details.

package dcrpg

import (
	"database/sql"
	"fmt"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/db/dcrpg/internal"
	"github.com/decred/dcrdata/rpcutils"
	"github.com/decred/dcrdata/txhelpers"
)

// CheckForAuxDBUpgrade checks if an upgrade is required and currently
// supported. A boolean value is returned to indicate if the db upgrade was
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

		err := pgb.handleAgendasTableUpgrade(smartClient)
		if err != nil {
			return false, err
		}

		return true, versionAllTables(pgb.db, version)
	}

	return false, nil
}

// handleAgendasTableUpgrade implements the upgrade to the newly added agenda
// table. If the table exists, the db upgrade fails to proceed
func (pgb *ChainDB) handleAgendasTableUpgrade(client *rpcutils.BlockGate) error {
	var rowsUpdated int64
	c, err := haveEmptyAgendasTable(pgb.db)
	if c == 0 {
		return err
	}

	height, err := pgb.HeightDB()
	if err != nil {
		return err
	}

	log.Infof("Found the best block at height: %v", height)

	// last (block height) from where the first vote for an agenda was cast
	var i, last int64 = 128000, int64(height) + 1
	chunkEnd := i

	// Fetch the block associated with the provided block height
	for ; i < last; i++ {
		var block, err = client.UpdateToBlock(i)
		if err != nil {
			return err
		}

		if i%5000 == 0 {
			chunkEnd += 5000
			if int64(height) < chunkEnd {
				chunkEnd = last
			}
			log.Infof("Upgrading the Agendas (New Table Upgrade) from height %v to %v ",
				i, chunkEnd-1)
		}

		p, err := pgb.tableUpgrade(block)
		if err != nil {
			return err
		}

		rowsUpdated += p
	}

	log.Infof(" %v rows in Agendas (New Table Upgrade) were successfully upgraded.", rowsUpdated)

	log.Infof("Index the Agendas table on Agenda ID...")
	IndexAgendasTableOnAgendaID(pgb.db)

	log.Infof("Index the Agendas table on Block Time...")
	IndexAgendasTableOnBlockTime(pgb.db)

	return nil
}

func (pgb *ChainDB) tableUpgrade(block *dcrutil.Block) (int64, error) {
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

	var msgBlock = block.MsgBlock()
	var dbTxns, _, _ = dbtypes.ExtractBlockTransactions(msgBlock,
		wire.TxTreeStake, pgb.chainParams)

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

// haveEmptyAgendasTable checks if the agendas table is empty.
// If the agenda table exists 0 is returned otherwise 1 is returned.
// If the table is not empty then this upgrade doesn't proceed.
func haveEmptyAgendasTable(db *sql.DB) (int, error) {
	var isExists int

	err := db.QueryRow(`SELECT COUNT(*) FROM agendas;`).Scan(&isExists)
	if err != nil {
		return 0, err
	}

	if isExists != 0 {
		return 0, nil
	}

	return 1, nil
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

// versionTable comments the specified table with the upgraded table version.
func versionTable(db *sql.DB, tableName string, version TableVersion) error {
	_, err := db.Exec(fmt.Sprintf(`COMMENT ON TABLE %s IS 'v%s';`,
		tableName, version.String()))
	if err != nil {
		return err
	}

	log.Infof("Modified the %v table version to %v", tableName, version)
	return nil
}
