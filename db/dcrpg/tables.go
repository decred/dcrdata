// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"

	"github.com/dcrdata/dcrdata/db/dcrpg/internal"
)

var createTableStatements = map[string]string{
	"blocks":       internal.CreateBlockTable,
	"transactions": internal.CreateTransactionTable,
	"vins":         internal.CreateVinTable,
	"vouts":        internal.CreateVoutTable,
	"block_chain":  internal.CreateBlockPrevNextTable,
	"addresses":    internal.CreateAddressTable,
	"tickets":      internal.CreateTicketsTable,
	"votes":        internal.CreateVotesTable,
	"misses":       internal.CreateMissesTable,
}

var createTypeStatements = map[string]string{
	"vin_t":  internal.CreateVinType,
	"vout_t": internal.CreateVoutType,
}

func TableExists(db *sql.DB, tableName string) (bool, error) {
	rows, err := db.Query(`select relname from pg_class where relname = $1`,
		tableName)
	if err == nil {
		defer func() {
			if e := rows.Close(); e != nil {
				log.Errorf("Close of Query failed: %v", e)
			}
		}()
		return rows.Next(), nil
	}
	return false, err
}

func DropTables(db *sql.DB) {
	for tableName := range createTableStatements {
		log.Infof("DROPPING the \"%s\" table.", tableName)
		if err := dropTable(db, tableName); err != nil {
			log.Errorf("DROP TABLE %s failed.", tableName)
		}
	}

	_, err := db.Exec(`DROP TYPE IF EXISTS vin;`)
	if err != nil {
		log.Errorf("DROP TYPE vin failed.")
	}
}

func dropTable(db *sql.DB, tableName string) error {
	_, err := db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, tableName))
	return err
}

func CreateTypes(db *sql.DB) error {
	var err error
	for typeName, createCommand := range createTypeStatements {
		var exists bool
		exists, err = TypeExists(db, typeName)
		if err != nil {
			return err
		}

		if !exists {
			log.Infof("Creating the \"%s\" type.", typeName)
			_, err = db.Exec(createCommand)
			if err != nil {
				return err
			}
			_, err = db.Exec(fmt.Sprintf(`COMMENT ON TYPE %s
				IS 'v1';`, typeName))
			if err != nil {
				return err
			}
		} else {
			log.Debugf("Type \"%s\" exist.", typeName)
		}
	}
	return err
}

func TypeExists(db *sql.DB, tableName string) (bool, error) {
	rows, err := db.Query(`select typname from pg_type where typname = $1`,
		tableName)
	if err == nil {
		defer func() {
			if e := rows.Close(); e != nil {
				log.Errorf("Close of Query failed: %v", e)
			}
		}()
		return rows.Next(), nil
	}
	return false, err
}

func CreateTables(db *sql.DB) error {
	var err error
	for tableName, createCommand := range createTableStatements {
		var exists bool
		exists, err = TableExists(db, tableName)
		if err != nil {
			return err
		}

		if !exists {
			log.Infof("Creating the \"%s\" table.", tableName)
			_, err = db.Exec(createCommand)
			if err != nil {
				return err
			}
			_, err = db.Exec(fmt.Sprintf(`COMMENT ON TABLE %s
				IS 'v1';`, tableName))
			if err != nil {
				return err
			}
		} else {
			log.Debugf("Table \"%s\" exist.", tableName)
		}
	}
	return err
}

// CreateTable creates one of the known tables by name
func CreateTable(db *sql.DB, tableName string) error {
	var err error
	createCommand, tableNameFound := createTableStatements[tableName]
	if !tableNameFound {
		log.Errorf("Unknown table name %v", tableName)
	}

	var exists bool
	exists, err = TableExists(db, tableName)
	if err != nil {
		return err
	}

	if !exists {
		log.Infof("Creating the \"%s\" table.", tableName)
		_, err = db.Exec(createCommand)
		if err != nil {
			return err
		}
		_, err = db.Exec(fmt.Sprintf(`COMMENT ON TABLE %s
			IS 'v1';`, tableName))
		if err != nil {
			return err
		}
	} else {
		log.Debugf("Table \"%s\" exist.", tableName)
	}

	return err
}

func TableVersions(db *sql.DB) map[string]int32 {
	versions := map[string]int32{}
	for tableName := range createTableStatements {
		Result := db.QueryRow(`select obj_description($1::regclass);`, tableName)
		var s string
		v := int(-1)
		if Result != nil {
			err := Result.Scan(&s)
			if err != nil {
				log.Errorf("Scan of QueryRow failed: %v", err)
				continue
			}
			re := regexp.MustCompile(`^v(\d+)$`)
			subs := re.FindStringSubmatch(s)
			if len(subs) > 1 {
				v, err = strconv.Atoi(subs[1])
				if err != nil {
					fmt.Println(err)
				}
			}
		}
		versions[tableName] = int32(v)
	}
	return versions
}

// Vins table indexes

func IndexVinTableOnVins(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVinTableOnVins)
	return
}

func IndexVinTableOnPrevOuts(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVinTableOnPrevOuts)
	return
}

func DeindexVinTableOnVins(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVinTableOnVins)
	return
}

func DeindexVinTableOnPrevOuts(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVinTableOnPrevOuts)
	return
}

// Transactions table indexes

func IndexTransactionTableOnHashes(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexTransactionTableOnHashes)
	return
}

func DeindexTransactionTableOnHashes(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexTransactionTableOnHashes)
	return
}

func IndexTransactionTableOnBlockIn(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexTransactionTableOnBlockIn)
	return
}

func DeindexTransactionTableOnBlockIn(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexTransactionTableOnBlockIn)
	return
}

// Blocks table indexes

func IndexBlockTableOnHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexBlockTableOnHash)
	return
}

func DeindexBlockTableOnHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexBlockTableOnHash)
	return
}

// Vouts table indexes

func IndexVoutTableOnTxHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVoutTableOnTxHash)
	return
}

func IndexVoutTableOnTxHashIdx(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVoutTableOnTxHashIdx)
	return
}

func DeindexVoutTableOnTxHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVoutTableOnTxHash)
	return
}

func DeindexVoutTableOnTxHashIdx(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVoutTableOnTxHashIdx)
	return
}

// Addresses table indexes

func IndexAddressTableOnAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAddressTableOnAddress)
	return
}

func DeindexAddressTableOnAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAddressTableOnAddress)
	return
}

func IndexAddressTableOnVoutID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAddressTableOnVoutID)
	return
}

func DeindexAddressTableOnVoutID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAddressTableOnVoutID)
	return
}

func IndexAddressTableOnTxHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAddressTableOnFundingTx)
	return
}

func DeindexAddressTableOnTxHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAddressTableOnFundingTx)
	return
}

// Votes table indexes

func IndexVotesTableOnHashes(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVotesTableOnHashes)
	return
}

func DeindexVotesTableOnHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVotesTableOnHashes)
	return
}

func IndexVotesTableOnCandidate(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVotesTableOnCandidate)
	return
}

func DeindexVotesTableOnCandidate(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVotesTableOnCandidate)
	return
}

func IndexVotesTableOnVoteVersion(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVotesTableOnVoteVersion)
	return
}

func DeindexVotesTableOnVoteVersion(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVotesTableOnVoteVersion)
	return
}

// Tickets table indexes

func IndexTicketsTableOnHashes(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexTicketsTableOnHashes)
	return
}

func DeindexTicketsTableOnHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexTicketsTableOnHashes)
	return
}

func IndexTicketsTableOnTxDbID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexTicketsTableOnTxDbID)
	return
}

func DeindexTicketsTableOnTxDbID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexTicketsTableOnTxDbID)
	return
}

// Missed votes table indexes

func IndexMissesTableOnHashes(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexMissesTableOnHashes)
	return
}

func DeindexMissesTableOnHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexMissesTableOnHashes)
	return
}
