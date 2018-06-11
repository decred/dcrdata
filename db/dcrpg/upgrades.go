// Copyright (c) 2018, The dcrdata developers.
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

// CheckForAuxDBUpgrade checks if an upgrade is required and currently supported.
// A boolean value is returned to indicate that the db upgrade was successfully completed.
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

	// When the required table version is 2.x.0 where x is greater than or equal to 3
	case version.major == 2 && version.minor > 3 && version.patch == 0:
		smartClient := rpcutils.NewBlockGate(dcrdClient, 10)

		err := pgb.handleAgendasTableUpgrade(smartClient)
		if err != nil {
			return false, err
		}

		return true, commentTable(pgb.db, version)
	}

	return false, nil
}

// handleAgendasTableUpgrade implements the upgrade to the newly added agenda table.
// If the table exists, the db upgrade fails to proceed
func (pgb *ChainDB) handleAgendasTableUpgrade(client *rpcutils.BlockGate) error {
	var rowsUpdated int64
	c, err := isAgendasTable(pgb.db)
	if c == 0 {
		return err
	}

	height, err := pgb.HeightDB()
	if err != nil {
		return err
	}

	log.Infof("Found the best block at height: %v", height)
	var limit, i uint64

	var milestones = map[string]dbtypes.MileStone{
		"sdiffalgorithm": dbtypes.MileStone{
			Activated:  149248,
			HardForked: 149328,
			LockedIn:   141184},
		"lnsupport": dbtypes.MileStone{
			Activated: 149248,
			LockedIn:  141184},
		"lnfeatures": dbtypes.MileStone{
			Activated: 189568,
			LockedIn:  181504},
	}

	// Range (block height) from where the first the vote for an agenda was cast
	i, limit = 128000, 128000

	// Fetch the block associated with the provided block height
	for ; i < height+1; i++ {
		var block, err = client.UpdateToBlock(int64(i))
		if err != nil {
			return err
		}

		if i%5000 == 0 {
			limit += 5000
			if height < limit {
				limit = height + 1
			}
			log.Infof("Upgrading the Agendas (New Table Upgrade) from height %v to %v ", i, limit-1)
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
			index, err := dbtypes.ToChoiceIndex(val.Choice.Id)
			if err != nil {
				return 0, err
			}

			err = pgb.db.QueryRow(internal.MakeAgendaInsertStatement(false),
				val.ID, index, tx.TxID, tx.BlockHeight, tx.BlockTime,
				milestones[val.ID].LockedIn == tx.BlockHeight,
				milestones[val.ID].Activated == tx.BlockHeight,
				milestones[val.ID].HardForked == tx.BlockHeight).Scan(&rowID)
			if err != nil {
				return 0, err
			}

			rowsUpdated++
		}
	}
	return rowsUpdated, nil
}

// isAgendasTable checks if the agendas table is empty
func isAgendasTable(db *sql.DB) (int, error) {
	var isExists int

	err := db.QueryRow(`SELECT COUNT(*) FROM agendas;`).Scan(&isExists)
	if err != nil {
		return 0, err
	}
	// If the table is not empty then this upgrade shouldn't proceed
	if isExists != 0 {
		return 0, nil
	}

	return 1, nil
}

// commentTable comments the tables with the upgraded table version
func commentTable(db *sql.DB, version TableVersion) error {
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
