// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"

	"github.com/decred/dcrdata/db/dcrpg/internal"
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
	"agendas":      internal.CreateAgendasTable,
}

var createTypeStatements = map[string]string{
	"vin_t":  internal.CreateVinType,
	"vout_t": internal.CreateVoutType,
}

// The tables are versioned as follows. The major version is the same for all
// the tables. A bump of this version is used to signal that all tables should
// be dropped and rebuilt. The minor versions may be different, and they are
// used to indicate a change requiring a table upgrade, which would be handled
// by dcrdata or rebuilddb2. The patch versions may also be different. They
// indicate a change of a table's index or constraint, which may require
// re-indexing and a duplicate scan/purge.
const (
	tableMajor = 3
	tableMinor = 4
)

var requiredVersions = map[string]TableVersion{
	"blocks":       NewTableVersion(tableMajor, tableMinor, 0),
	"transactions": NewTableVersion(tableMajor, tableMinor, 0),
	"vins":         NewTableVersion(tableMajor, tableMinor, 0),
	"vouts":        NewTableVersion(tableMajor, tableMinor, 0),
	"block_chain":  NewTableVersion(tableMajor, tableMinor, 0),
	"addresses":    NewTableVersion(tableMajor, tableMinor, 0),
	"tickets":      NewTableVersion(tableMajor, tableMinor, 0),
	"votes":        NewTableVersion(tableMajor, tableMinor, 0),
	"misses":       NewTableVersion(tableMajor, tableMinor, 0),
	"agendas":      NewTableVersion(tableMajor, tableMinor, 0),
}

// TableVersion models a table version by major.minor.patch
type TableVersion struct {
	major, minor, patch uint32
}

// TableVersionCompatible indicates if the table versions are compatible
// (equal), and if not, what is the required action (rebuild, upgrade, or
// reindex).
func TableVersionCompatible(required, actual TableVersion) string {
	switch {
	case required.major != actual.major:
		return "rebuild"
	case required.minor != actual.minor:
		return "upgrade"
	case required.patch != actual.patch:
		return "reindex"
	default:
		return "ok"
	}
}

func (s TableVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", s.major, s.minor, s.patch)
}

// NewTableVersion returns a new TableVersion with the version major.minor.patch
func NewTableVersion(major, minor, patch uint32) TableVersion {
	return TableVersion{major, minor, patch}
}

// TableUpgrade is used to define a required upgrade for a table
type TableUpgrade struct {
	TableName, UpgradeType  string
	CurrentVer, RequiredVer TableVersion
}

func (s TableUpgrade) String() string {
	return fmt.Sprintf("Table %s requires %s (%s -> %s).", s.TableName,
		s.UpgradeType, s.CurrentVer, s.RequiredVer)
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
			log.Tracef("Type \"%s\" exist.", typeName)
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

		tableVersion, ok := requiredVersions[tableName]
		if !ok {
			return fmt.Errorf("no version assigned to table %s", tableName)
		}

		if !exists {
			log.Infof("Creating the \"%s\" table.", tableName)
			_, err = db.Exec(createCommand)
			if err != nil {
				return err
			}
			_, err = db.Exec(fmt.Sprintf(`COMMENT ON TABLE %s IS 'v%s';`,
				tableName, tableVersion))
			if err != nil {
				return err
			}
		} else {
			log.Tracef("Table \"%s\" exist.", tableName)
		}
	}
	return err
}

// CreateTable creates one of the known tables by name
func CreateTable(db *sql.DB, tableName string) error {
	var err error
	createCommand, tableNameFound := createTableStatements[tableName]
	if !tableNameFound {
		return fmt.Errorf("table name %s unknown", tableName)
	}
	tableVersion, ok := requiredVersions[tableName]
	if !ok {
		return fmt.Errorf("no version assigned to table %s", tableName)
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
		_, err = db.Exec(fmt.Sprintf(`COMMENT ON TABLE %s IS 'v%s';`,
			tableName, tableVersion))
		if err != nil {
			return err
		}
	} else {
		log.Tracef("Table \"%s\" exist.", tableName)
	}

	return err
}

func TableUpgradesRequired(versions map[string]TableVersion) []TableUpgrade {
	var tableUpgrades []TableUpgrade
	for t := range createTableStatements {
		var ok bool
		var req, act TableVersion
		if req, ok = requiredVersions[t]; !ok {
			log.Errorf("required version unknown for table %s", t)
			tableUpgrades = append(tableUpgrades, TableUpgrade{
				TableName:   t,
				UpgradeType: "unknown",
			})
			continue
		}
		if act, ok = versions[t]; !ok {
			log.Errorf("current version unknown for table %s", t)
			tableUpgrades = append(tableUpgrades, TableUpgrade{
				TableName:   t,
				UpgradeType: "rebuild",
				RequiredVer: req,
			})
			continue
		}
		versionCompatibility := TableVersionCompatible(req, act)
		tableUpgrades = append(tableUpgrades, TableUpgrade{
			TableName:   t,
			UpgradeType: versionCompatibility,
			CurrentVer:  act,
			RequiredVer: req,
		})
	}
	return tableUpgrades
}

func TableVersions(db *sql.DB) map[string]TableVersion {
	versions := map[string]TableVersion{}
	for tableName := range createTableStatements {
		Result := db.QueryRow(`select obj_description($1::regclass);`, tableName)
		var s string
		var v, m, p int
		if Result != nil {
			err := Result.Scan(&s)
			if err != nil {
				log.Errorf("Scan of QueryRow failed: %v", err)
				continue
			}
			re := regexp.MustCompile(`^v(\d+)\.?(\d?)\.?(\d?)$`)
			subs := re.FindStringSubmatch(s)
			if len(subs) > 1 {
				v, err = strconv.Atoi(subs[1])
				if err != nil {
					fmt.Println(err)
				}
				if len(subs) > 2 && len(subs[2]) > 0 {
					m, err = strconv.Atoi(subs[2])
					if err != nil {
						fmt.Println(err)
					}
					if len(subs) > 3 && len(subs[3]) > 0 {
						p, err = strconv.Atoi(subs[3])
						if err != nil {
							fmt.Println(err)
						}
					}
				}
			}
		}
		versions[tableName] = NewTableVersion(uint32(v), uint32(m), uint32(p))
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

func IndexBlockTableOnHeight(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexBlocksTableOnHeight)
	return
}

func DeindexBlockTableOnHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexBlockTableOnHash)
	return
}

func DeindexBlockTableOnHeight(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexBlocksTableOnHeight)
	return
}

// Vouts table indexes

func IndexVoutTableOnTxHashIdx(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexVoutTableOnTxHashIdx)
	return
}

func DeindexVoutTableOnTxHashIdx(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexVoutTableOnTxHashIdx)
	return
}

// Addresses table indexes
func IndexBlockTimeOnTableAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexBlockTimeOnTableAddress)
	return
}

func DeindexBlockTimeOnTableAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexBlockTimeOnTableAddress)
	return
}

func IndexMatchingTxHashOnTableAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexMatchingTxHashOnTableAddress)
	return
}

func DeindexMatchingTxHashOnTableAddress(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexMatchingTxHashOnTableAddress)
	return
}

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
	_, err = db.Exec(internal.IndexAddressTableOnTxHash)
	return
}

func DeindexAddressTableOnTxHash(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAddressTableOnTxHash)
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

// agendas

func IndexAgendasTableOnBlockTime(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAgendasTableOnBlockTime)
	return
}

func DeindexAgendasTableOnBlockTime(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAgendasTableOnBlockTime)
	return
}

func IndexAgendasTableOnAgendaID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.IndexAgendasTableOnAgendaID)
	return
}

func DeindexAgendasTableOnAgendaID(db *sql.DB) (err error) {
	_, err = db.Exec(internal.DeindexAgendasTableOnAgendaID)
	return
}
