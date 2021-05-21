// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"database/sql"
	"fmt"

	"github.com/decred/dcrdata/db/dcrpg/v6/internal"
	"github.com/decred/dcrdata/v6/db/dbtypes"
)

var createTableStatements = [][2]string{
	{"meta", internal.CreateMetaTable},
	{"blocks", internal.CreateBlockTable},
	{"transactions", internal.CreateTransactionTable},
	{"vins", internal.CreateVinTable},
	{"vouts", internal.CreateVoutTable},
	{"block_chain", internal.CreateBlockPrevNextTable},
	{"addresses", internal.CreateAddressTable},
	{"tickets", internal.CreateTicketsTable},
	{"votes", internal.CreateVotesTable},
	{"misses", internal.CreateMissesTable},
	{"agendas", internal.CreateAgendasTable},
	{"agenda_votes", internal.CreateAgendaVotesTable},
	{"testing", internal.CreateTestingTable},
	{"proposals", internal.CreateProposalsTable},
	{"proposal_votes", internal.CreateProposalVotesTable},
	{"stats", internal.CreateStatsTable},
	{"treasury", internal.CreateTreasuryTable},
	{"swaps", internal.CreateAtomicSwapTable},
}

func createTableMap() map[string]string {
	m := make(map[string]string, len(createTableStatements))
	for _, pair := range createTableStatements {
		m[pair[0]] = pair[1]
	}
	return m
}

// dropDuplicatesInfo defines a minimalistic structure that can be used to
// append information needed to delete duplicates in a given table.
type dropDuplicatesInfo struct {
	TableName    string
	DropDupsFunc func() (int64, error)
}

// TableExists checks if the specified table exists.
func TableExists(db *sql.DB, tableName string) (bool, error) {
	rows, err := db.Query(`select relname from pg_class where relname = $1`,
		tableName)
	if err != nil {
		return false, err
	}

	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()
	return rows.Next(), nil
}

func dropTable(db SqlExecutor, tableName string) error {
	_, err := db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, tableName))
	return err
}

// DropTables drops all of the tables internally recognized tables.
func DropTables(db *sql.DB) {
	lastIndex := len(createTableStatements) - 1
	for i := range createTableStatements {
		pair := createTableStatements[lastIndex-i]
		tableName := pair[0]
		log.Infof("DROPPING the %q table.", tableName)
		if err := dropTable(db, tableName); err != nil {
			log.Errorf("DROP TABLE %q; failed.", tableName)
		}
	}
}

// DropTestingTable drops only the "testing" table.
func DropTestingTable(db SqlExecutor) error {
	_, err := db.Exec(`DROP TABLE IF EXISTS testing;`)
	return err
}

// AnalyzeAllTables performs an ANALYZE on all tables after setting
// default_statistics_target for the transaction.
func AnalyzeAllTables(db *sql.DB, statisticsTarget int) error {
	dbTx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transactions: %v", err)
	}

	_, err = dbTx.Exec(fmt.Sprintf("SET LOCAL default_statistics_target TO %d;", statisticsTarget))
	if err != nil {
		_ = dbTx.Rollback()
		return fmt.Errorf("failed to set default_statistics_target: %v", err)
	}

	_, err = dbTx.Exec(`ANALYZE;`)
	if err != nil {
		_ = dbTx.Rollback()
		return fmt.Errorf("failed to ANALYZE all tables: %v", err)
	}

	return dbTx.Commit()
}

// AnalyzeTable performs an ANALYZE on the specified table after setting
// default_statistics_target for the transaction.
func AnalyzeTable(db *sql.DB, table string, statisticsTarget int) error {
	dbTx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transactions: %v", err)
	}

	_, err = dbTx.Exec(fmt.Sprintf("SET LOCAL default_statistics_target TO %d;", statisticsTarget))
	if err != nil {
		_ = dbTx.Rollback()
		return fmt.Errorf("failed to set default_statistics_target: %v", err)
	}

	_, err = dbTx.Exec(fmt.Sprintf(`ANALYZE %s;`, table))
	if err != nil {
		_ = dbTx.Rollback()
		return fmt.Errorf("failed to ANALYZE all tables: %v", err)
	}

	return dbTx.Commit()
}

func ClearTestingTable(db *sql.DB) error {
	// Clear the scratch table and reset the serial value.
	_, err := db.Exec(`TRUNCATE TABLE testing;`)
	if err == nil {
		_, err = db.Exec(`SELECT setval('testing_id_seq', 1, false);`)
	}
	return err
}

// CreateTables creates all tables required by dcrdata if they do not already
// exist.
func CreateTables(db *sql.DB) error {
	// Create all of the data tables.
	for _, pair := range createTableStatements {
		err := createTable(db, pair[0], pair[1])
		if err != nil {
			return err
		}
	}

	return ClearTestingTable(db)
}

// CreateTable creates one of the known tables by name.
func CreateTable(db *sql.DB, tableName string) error {
	tableMap := createTableMap()
	createCommand, tableNameFound := tableMap[tableName]
	if !tableNameFound {
		return fmt.Errorf("table name %s unknown", tableName)
	}

	return createTable(db, tableName, createCommand)
}

// createTable creates a table with the given name using the provided SQL
// statement, if it does not already exist.
func createTable(db *sql.DB, tableName, stmt string) error {
	exists, err := TableExists(db, tableName)
	if err != nil {
		return err
	}

	if !exists {
		log.Infof(`Creating the "%s" table.`, tableName)
		_, err = db.Exec(stmt)
		if err != nil {
			return err
		}
	} else {
		log.Tracef(`Table "%s" exists.`, tableName)
	}

	return err
}

// CheckColumnDataType gets the data type of specified table column .
func CheckColumnDataType(db *sql.DB, table, column string) (dataType string, err error) {
	err = db.QueryRow(`SELECT data_type
		FROM information_schema.columns
		WHERE table_name=$1 AND column_name=$2`,
		table, column).Scan(&dataType)
	return
}

// DeleteDuplicates attempts to delete "duplicate" rows in tables where unique
// indexes are to be created.
func (pgb *ChainDB) DeleteDuplicates(barLoad chan *dbtypes.ProgressBarLoad) error {
	var allDuplicates []dropDuplicatesInfo
	if pgb.cockroach {
		allDuplicates = append(allDuplicates,
			// Remove duplicate vins
			dropDuplicatesInfo{TableName: "vins", DropDupsFunc: pgb.DeleteDuplicateVinsCockroach},

			// Remove duplicate vouts
			dropDuplicatesInfo{TableName: "vouts", DropDupsFunc: pgb.DeleteDuplicateVoutsCockroach},
		)
	} else {
		allDuplicates = append(allDuplicates,
			// Remove duplicate vins
			dropDuplicatesInfo{TableName: "vins", DropDupsFunc: pgb.DeleteDuplicateVins},

			// Remove duplicate vouts
			dropDuplicatesInfo{TableName: "vouts", DropDupsFunc: pgb.DeleteDuplicateVouts},
		)
	}

	allDuplicates = append(allDuplicates,
		// Remove duplicate transactions
		dropDuplicatesInfo{TableName: "transactions", DropDupsFunc: pgb.DeleteDuplicateTxns},

		// Remove duplicate agendas
		dropDuplicatesInfo{TableName: "agendas", DropDupsFunc: pgb.DeleteDuplicateAgendas},

		// Remove duplicate agenda_votes
		dropDuplicatesInfo{TableName: "agenda_votes", DropDupsFunc: pgb.DeleteDuplicateAgendaVotes},
	)

	var err error
	for _, val := range allDuplicates {
		msg := fmt.Sprintf("Finding and removing duplicate %s entries...", val.TableName)
		if barLoad != nil {
			barLoad <- &dbtypes.ProgressBarLoad{BarID: dbtypes.InitialDBLoad, Subtitle: msg}
		}
		log.Info(msg)

		var numRemoved int64
		if numRemoved, err = val.DropDupsFunc(); err != nil {
			return fmt.Errorf("delete %s duplicate failed: %v", val.TableName, err)
		}

		msg = fmt.Sprintf("Removed %d duplicate %s entries.", numRemoved, val.TableName)
		if barLoad != nil {
			barLoad <- &dbtypes.ProgressBarLoad{BarID: dbtypes.InitialDBLoad, Subtitle: msg}
		}
		log.Info(msg)
	}
	// Signal task is done
	if barLoad != nil {
		barLoad <- &dbtypes.ProgressBarLoad{BarID: dbtypes.InitialDBLoad, Subtitle: " "}
	}
	return nil
}

// DeleteDuplicatesRecovery attempts to delete "duplicate" rows in all tables
// where unique indexes are to be created.  This is like DeleteDuplicates, but
// it also includes transactions, tickets, votes, and misses.
func (pgb *ChainDB) DeleteDuplicatesRecovery(barLoad chan *dbtypes.ProgressBarLoad) error {
	allDuplicates := []dropDuplicatesInfo{
		// Remove duplicate vins
		{TableName: "vins", DropDupsFunc: pgb.DeleteDuplicateVins},

		// Remove duplicate vouts
		{TableName: "vouts", DropDupsFunc: pgb.DeleteDuplicateVouts},

		// Remove duplicate transactions
		{TableName: "transactions", DropDupsFunc: pgb.DeleteDuplicateTxns},

		// Remove duplicate tickets
		{TableName: "tickets", DropDupsFunc: pgb.DeleteDuplicateTickets},

		// Remove duplicate votes
		{TableName: "votes", DropDupsFunc: pgb.DeleteDuplicateVotes},

		// Remove duplicate misses
		{TableName: "misses", DropDupsFunc: pgb.DeleteDuplicateMisses},

		// Remove duplicate agendas
		{TableName: "agendas", DropDupsFunc: pgb.DeleteDuplicateAgendas},

		// Remove duplicate agenda_votes
		{TableName: "agenda_votes", DropDupsFunc: pgb.DeleteDuplicateAgendaVotes},
	}

	var err error
	for _, val := range allDuplicates {
		msg := fmt.Sprintf("Finding and removing duplicate %s entries...", val.TableName)
		if barLoad != nil {
			barLoad <- &dbtypes.ProgressBarLoad{BarID: dbtypes.InitialDBLoad, Subtitle: msg}
		}
		log.Info(msg)

		var numRemoved int64
		if numRemoved, err = val.DropDupsFunc(); err != nil {
			return fmt.Errorf("delete %s duplicate failed: %v", val.TableName, err)
		}

		msg = fmt.Sprintf("Removed %d duplicate %s entries.", numRemoved, val.TableName)
		if barLoad != nil {
			barLoad <- &dbtypes.ProgressBarLoad{BarID: dbtypes.InitialDBLoad, Subtitle: msg}
		}
		log.Info(msg)
	}
	// Signal task is done
	if barLoad != nil {
		barLoad <- &dbtypes.ProgressBarLoad{BarID: dbtypes.InitialDBLoad, Subtitle: " "}
	}
	return nil
}
