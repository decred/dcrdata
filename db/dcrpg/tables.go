// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"

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
	tableMinor = 7
	tablePatch = 0
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

func (pgb *ChainDB) DeleteDuplicates(barLoad chan *dbtypes.ProgressBarLoad) error {
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

func (pgb *ChainDB) DeleteDuplicatesRecovery(barLoad chan *dbtypes.ProgressBarLoad) error {
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
