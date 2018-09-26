// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/decred/dcrdata/v3/db/dbtypes"
	"github.com/decred/dcrdata/v3/db/dcrpg/internal"
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

// indexingInfo defines a minimalistic structure used to append new indexes
// to be implemented with minimal code duplication.
type indexingInfo struct {
	Msg       string
	IndexFunc func(db *sql.DB) error
}

// deIndexingInfo defines a minimalistic structure used to append new deindexes
// to be implemented with minimal code duplication.
type deIndexingInfo struct {
	DeIndexFunc func(db *sql.DB) error
}

// dropDuplicatesInfo defines a minimalistic structure that can be used to
// append information needed to delete duplicates in a given table.
type dropDuplicatesInfo struct {
	TableName    string
	DropDupsFunc func() (int64, error)
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
	tableMinor = 5
	tablePatch = 5
)

// TODO eliminiate this map since we're actually versioning each table the same.
var requiredVersions = map[string]TableVersion{
	"blocks":       NewTableVersion(tableMajor, tableMinor, tablePatch),
	"transactions": NewTableVersion(tableMajor, tableMinor, tablePatch),
	"vins":         NewTableVersion(tableMajor, tableMinor, tablePatch),
	"vouts":        NewTableVersion(tableMajor, tableMinor, tablePatch),
	"block_chain":  NewTableVersion(tableMajor, tableMinor, tablePatch),
	"addresses":    NewTableVersion(tableMajor, tableMinor, tablePatch),
	"tickets":      NewTableVersion(tableMajor, tableMinor, tablePatch),
	"votes":        NewTableVersion(tableMajor, tableMinor, tablePatch),
	"misses":       NewTableVersion(tableMajor, tableMinor, tablePatch),
	"agendas":      NewTableVersion(tableMajor, tableMinor, tablePatch),
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

func (pgb *ChainDB) DeleteDuplicates() error {
	allDuplicates := []dropDuplicatesInfo{
		// Remove duplicate vins
		dropDuplicatesInfo{TableName: "vins", DropDupsFunc: pgb.DeleteDuplicateVins},
		// Remove duplicate vouts
		dropDuplicatesInfo{TableName: "vouts", DropDupsFunc: pgb.DeleteDuplicateVouts},
		// Remove duplicate transactions
		dropDuplicatesInfo{TableName: "transactions", DropDupsFunc: pgb.DeleteDuplicateTxns},

		// TODO: remove entries from addresses table that reference removed
		// vins/vouts.
	}

	var err error
	for _, val := range allDuplicates {
		msg := fmt.Sprintf("Finding and removing duplicate %s entries...", val.TableName)
		dbtypes.SyncStatusUpdateOtherMsg(dbtypes.InitialDBLoad, msg)
		log.Info(msg)

		var numRemoved int64
		if numRemoved, err = val.DropDupsFunc(); err != nil {
			return fmt.Errorf("delete %s duplicate failed: %v", val.TableName, err)
		}

		msg = fmt.Sprintf("Removed %d duplicate %s entries.", numRemoved, val.TableName)
		dbtypes.SyncStatusUpdateOtherMsg(dbtypes.InitialDBLoad, msg)
		log.Info(msg)
	}
	// signal task is done
	dbtypes.SyncStatusUpdateOtherMsg(dbtypes.InitialDBLoad, "")
	return nil
}

func (pgb *ChainDB) DeleteDuplicatesRecovery() error {
	allDuplicates := []dropDuplicatesInfo{
		// Remove duplicate vins
		dropDuplicatesInfo{TableName: "vins", DropDupsFunc: pgb.DeleteDuplicateVins},

		// Remove duplicate vouts
		dropDuplicatesInfo{TableName: "vouts", DropDupsFunc: pgb.DeleteDuplicateVouts},

		// TODO: remove entries from addresses table that reference removed
		// vins/vouts.

		// Remove duplicate transactions
		dropDuplicatesInfo{TableName: "transactions", DropDupsFunc: pgb.DeleteDuplicateTxns},

		// Remove duplicate tickets
		dropDuplicatesInfo{TableName: "tickets", DropDupsFunc: pgb.DeleteDuplicateTickets},

		// Remove duplicate votes
		dropDuplicatesInfo{TableName: "votes", DropDupsFunc: pgb.DeleteDuplicateVotes},

		// Remove duplicate misses
		dropDuplicatesInfo{TableName: "misses", DropDupsFunc: pgb.DeleteDuplicateMisses},
	}

	var err error
	for _, val := range allDuplicates {
		msg := fmt.Sprintf("Finding and removing duplicate %s entries...", val.TableName)
		dbtypes.SyncStatusUpdateOtherMsg(dbtypes.InitialDBLoad, msg)
		log.Info(msg)

		var numRemoved int64
		if numRemoved, err = val.DropDupsFunc(); err != nil {
			return fmt.Errorf("delete %s duplicate failed: %v", val.TableName, err)
		}

		msg = fmt.Sprintf("Removed %s duplicate %s entries.", numRemoved, val.TableName)
		dbtypes.SyncStatusUpdateOtherMsg(dbtypes.InitialDBLoad, msg)
		log.Info(msg)
	}
	// signal task is done
	dbtypes.SyncStatusUpdateOtherMsg(dbtypes.InitialDBLoad, "")
	return nil
}

// DeindexAll drops all of the indexes in all tables
func (pgb *ChainDB) DeindexAll() error {
	allDeIndexes := []deIndexingInfo{
		// blocks table
		deIndexingInfo{DeindexBlockTableOnHash},
		deIndexingInfo{DeindexBlockTableOnHeight},

		// transactions table
		deIndexingInfo{DeindexTransactionTableOnHashes},
		deIndexingInfo{DeindexTransactionTableOnBlockIn},

		// vins table
		deIndexingInfo{DeindexVinTableOnVins},
		deIndexingInfo{DeindexVinTableOnPrevOuts},

		// vouts table
		deIndexingInfo{DeindexVoutTableOnTxHashIdx},

		// addresses table
		deIndexingInfo{DeindexBlockTimeOnTableAddress},
		deIndexingInfo{DeindexMatchingTxHashOnTableAddress},
		deIndexingInfo{DeindexAddressTableOnAddress},
		deIndexingInfo{DeindexAddressTableOnVoutID},
		deIndexingInfo{DeindexAddressTableOnTxHash},

		// votes table
		deIndexingInfo{DeindexVotesTableOnCandidate},
		deIndexingInfo{DeindexVotesTableOnBlockHash},
		deIndexingInfo{DeindexVotesTableOnHash},
		deIndexingInfo{DeindexVotesTableOnVoteVersion},

		// misses table
		deIndexingInfo{DeindexMissesTableOnHash},

		// agendas table
		deIndexingInfo{DeindexAgendasTableOnBlockTime},
		deIndexingInfo{DeindexAgendasTableOnAgendaID},
	}

	var err error
	for _, val := range allDeIndexes {
		if err = val.DeIndexFunc(pgb.db); err != nil {
			warnUnlessNotExists(err)
			err = nil
		}
	}

	if err = pgb.DeindexTicketsTable(); err != nil {
		warnUnlessNotExists(err)
		err = nil
	}
	return err
}

// IndexAll creates all of the indexes in all tables
func (pgb *ChainDB) IndexAll() error {
	allIndexes := []indexingInfo{
		// blocks table
		indexingInfo{Msg: "blocks table on hash", IndexFunc: IndexBlockTableOnHash},
		indexingInfo{Msg: "blocks table on height", IndexFunc: IndexBlockTableOnHeight},

		// transactions table
		indexingInfo{Msg: "transactions table on tx/block hashes", IndexFunc: IndexTransactionTableOnHashes},
		indexingInfo{Msg: "transactions table on block id/indx", IndexFunc: IndexTransactionTableOnBlockIn},

		// vins table
		indexingInfo{Msg: "vins table on txin", IndexFunc: IndexVinTableOnVins},
		indexingInfo{Msg: "vins table on prevouts", IndexFunc: IndexVinTableOnPrevOuts},

		// vouts table
		indexingInfo{Msg: "vouts table on tx hash and index", IndexFunc: IndexVoutTableOnTxHashIdx},

		// votes table
		indexingInfo{Msg: "votes table on candidate block", IndexFunc: IndexVotesTableOnCandidate},
		indexingInfo{Msg: "votes table on block hash", IndexFunc: IndexVotesTableOnBlockHash},
		indexingInfo{Msg: "votes table on block+tx hash", IndexFunc: IndexVotesTableOnHashes},
		indexingInfo{Msg: "votes table on vote version", IndexFunc: IndexVotesTableOnVoteVersion},

		// misses table
		indexingInfo{Msg: "misses table", IndexFunc: IndexMissesTableOnHashes},

		// agendas table
		indexingInfo{Msg: "agendas table on Block Time", IndexFunc: IndexAgendasTableOnBlockTime},
		indexingInfo{Msg: "agendas table on Agenda ID", IndexFunc: IndexAgendasTableOnAgendaID},

		// Not indexing the address table on vout ID or address here. See
		// IndexAddressTable to create those indexes.
		indexingInfo{Msg: "addresses table on tx hash", IndexFunc: IndexAddressTableOnTxHash},
		indexingInfo{Msg: "addresses table on matching tx hash", IndexFunc: IndexMatchingTxHashOnTableAddress},
		indexingInfo{Msg: "addresses table on block time", IndexFunc: IndexBlockTimeOnTableAddress},
	}

	for _, val := range allIndexes {
		logMsg := "Indexing " + val.Msg + "..."
		log.Infof(logMsg)
		if err := val.IndexFunc(pgb.db); err != nil {
			return err
		}

		dbtypes.SyncStatusUpdateOtherMsg(dbtypes.InitialDBLoad, logMsg)
	}
	// signal task is done
	dbtypes.SyncStatusUpdateOtherMsg(dbtypes.InitialDBLoad, "")
	return nil
}

// IndexTicketsTable creates the indexes on the tickets table on ticket hash and
// tx DB ID columns, separately.
func (pgb *ChainDB) IndexTicketsTable() error {
	ticketsTableIndexes := []indexingInfo{
		indexingInfo{Msg: "ticket hash", IndexFunc: IndexTicketsTableOnHashes},
		indexingInfo{Msg: "ticket pool status", IndexFunc: IndexTicketsTableOnPoolStatus},
		indexingInfo{Msg: "transaction Db ID", IndexFunc: IndexTicketsTableOnTxDbID},
	}

	for _, val := range ticketsTableIndexes {
		logMsg := "Indexing tickets table on " + val.Msg + "..."
		log.Info(logMsg)
		if err := val.IndexFunc(pgb.db); err != nil {
			return err
		}

		dbtypes.SyncStatusUpdateOtherMsg(dbtypes.AddressesTableSync, logMsg)
	}
	// signal task is done
	dbtypes.SyncStatusUpdateOtherMsg(dbtypes.AddressesTableSync, "")
	return nil
}

// DeindexTicketsTable drops the ticket hash and tx DB ID column indexes for the
// tickets table.
func (pgb *ChainDB) DeindexTicketsTable() error {
	ticketsTablesDeIndexes := []deIndexingInfo{
		deIndexingInfo{DeindexTicketsTableOnHash},
		deIndexingInfo{DeindexTicketsTableOnPoolStatus},
		deIndexingInfo{DeindexTicketsTableOnTxDbID},
	}

	var err error
	for _, val := range ticketsTablesDeIndexes {
		if err = val.DeIndexFunc(pgb.db); err != nil {
			warnUnlessNotExists(err)
			err = nil
		}
	}
	return err
}

func warnUnlessNotExists(err error) {
	if !strings.Contains(err.Error(), "does not exist") {
		log.Warn(err)
	}
}

// IndexAddressTable creates the indexes on the address table on the vout ID,
// block_time, matching_tx_hash and address columns, separately.
func (pgb *ChainDB) IndexAddressTable() error {
	addressesTableIndexes := []indexingInfo{
		indexingInfo{Msg: "address", IndexFunc: IndexAddressTableOnAddress},
		indexingInfo{Msg: "matching tx hash", IndexFunc: IndexMatchingTxHashOnTableAddress},
		indexingInfo{Msg: "block time", IndexFunc: IndexBlockTimeOnTableAddress},
		indexingInfo{Msg: "vout Db ID", IndexFunc: IndexAddressTableOnVoutID},
	}

	for _, val := range addressesTableIndexes {
		logMsg := "Indexing addresses table on  " + val.Msg + "..."
		log.Info(logMsg)
		if err := val.IndexFunc(pgb.db); err != nil {
			return err
		}

		dbtypes.SyncStatusUpdateOtherMsg(dbtypes.AddressesTableSync, logMsg)
	}
	// signal task is done
	dbtypes.SyncStatusUpdateOtherMsg(dbtypes.AddressesTableSync, "")
	return nil
}

// DeindexAddressTable drops the vin ID, block_time, matching_tx_hash
// and address column indexes for the address table.
func (pgb *ChainDB) DeindexAddressTable() error {
	addressesDeindexes := []deIndexingInfo{
		deIndexingInfo{DeindexAddressTableOnAddress},
		deIndexingInfo{DeindexMatchingTxHashOnTableAddress},
		deIndexingInfo{DeindexBlockTimeOnTableAddress},
		deIndexingInfo{DeindexAddressTableOnVoutID},
	}

	var err error
	for _, val := range addressesDeindexes {
		if err = val.DeIndexFunc(pgb.db); err != nil {
			warnUnlessNotExists(err)
			err = nil
		}
	}
	return err
}
