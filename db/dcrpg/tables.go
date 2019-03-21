// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package dcrpg

import (
	"database/sql"
	"fmt"

	"github.com/decred/dcrdata/v4/db/dbtypes"
	"github.com/decred/dcrdata/v4/db/dcrpg/internal"
	"github.com/decred/dcrdata/v4/semver"
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
	"agenda_votes": internal.CreateAgendaVotesTable,
	"testing":      internal.CreateTestingTable,
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
	tableMinor = 10
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
	"agenda_votes": NewTableVersion(tableMajor, tableMinor, tablePatch),
	"testing":      NewTableVersion(tableMajor, tableMinor, tablePatch),
}

// TableVersion models a table version by major.minor.patch
type TableVersion struct {
	major, minor, patch uint32
}

// CompatibilityAction defines the action to be taken once the current and the
// required pg table versions are compared.
type CompatibilityAction int8

const (
	Rebuild CompatibilityAction = iota
	Upgrade
	Reindex
	OK
	Unknown
)

// TableVersionCompatible indicates if the table versions are compatible
// (equal), and if not, what is the required action (rebuild, upgrade, or
// reindex).
func TableVersionCompatible(required, actual TableVersion) CompatibilityAction {
	switch {
	case required.major != actual.major:
		return Rebuild
	case required.minor != actual.minor:
		return Upgrade
	case required.patch != actual.patch:
		return Reindex
	default:
		return OK
	}
}

func (s TableVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", s.major, s.minor, s.patch)
}

// String implements Stringer for CompatibilityAction.
func (v CompatibilityAction) String() string {
	actions := map[CompatibilityAction]string{
		Rebuild: "rebuild",
		Upgrade: "upgrade",
		Reindex: "reindex",
		OK:      "ok",
	}
	if actionStr, ok := actions[v]; ok {
		return actionStr
	}
	return "unknown"
}

// NewTableVersion returns a new TableVersion with the version major.minor.patch
func NewTableVersion(major, minor, patch uint32) TableVersion {
	return TableVersion{major, minor, patch}
}

// TableUpgrade is used to define a required upgrade for a table
type TableUpgrade struct {
	TableName               string
	UpgradeType             CompatibilityAction
	CurrentVer, RequiredVer TableVersion
}

func (s TableUpgrade) String() string {
	return fmt.Sprintf("Table %s requires %s (%s -> %s).", s.TableName,
		s.UpgradeType, s.CurrentVer, s.RequiredVer)
}

// TableExists checks if the specified table exists.
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

func dropTable(db *sql.DB, tableName string) error {
	_, err := db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, tableName))
	return err
}

// DropTables drops all of the tables internally recognized tables.
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

// DropTestingTable drops only the "testing" table.
func DropTestingTable(db *sql.DB) error {
	_, err := db.Exec(`DROP TABLE IF EXISTS testing;`)
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

	return ClearTestingTable(db)
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
				UpgradeType: Unknown,
			})
			continue
		}
		if act, ok = versions[t]; !ok {
			log.Errorf("current version unknown for table %s", t)
			tableUpgrades = append(tableUpgrades, TableUpgrade{
				TableName:   t,
				UpgradeType: Rebuild,
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

// TableVersions retrieves the versions of the tables in the auxiliary db.
func TableVersions(db *sql.DB) map[string]TableVersion {
	versions := map[string]TableVersion{}
	for tableName := range createTableStatements {
		// Retrieve the table description.
		var desc string
		err := db.QueryRow(`select obj_description($1::regclass);`, tableName).Scan(&desc)
		if err != nil {
			log.Errorf("Query of table %s description failed: %v", tableName, err)
			continue
		}

		// Attempt to parse a version out of the table description.
		sv, err := semver.ParseVersionStr(desc)
		if err != nil {
			log.Errorf("Failed to parse version from table description %s: %v",
				desc, err)
			continue
		}

		versions[tableName] = NewTableVersion(sv.Split())
	}
	return versions
}

// CheckColumnDataType gets the data type of specified table column .
func CheckColumnDataType(db *sql.DB, table, column string) (dataType string, err error) {
	err = db.QueryRow(`SELECT data_type
		FROM information_schema.columns
		WHERE table_name=$1 AND column_name=$2`,
		table, column).Scan(&dataType)
	return
}

// CheckCurrentTimeZone queries for the currently set postgres time zone.
func CheckCurrentTimeZone(db *sql.DB) (currentTZ string, err error) {
	if err = db.QueryRow(`SHOW TIME ZONE`).Scan(&currentTZ); err != nil {
		err = fmt.Errorf("unable to query current time zone: %v", err)
	}
	return
}

// CheckCurrentTimeZone queries for the default postgres time zone. This is the
// value that would be observed if postgres were restarted using its current
// configuration. The currently set time zone is also returned.
func CheckDefaultTimeZone(db *sql.DB) (defaultTZ, currentTZ string, err error) {
	// Remember the current time zone before switching to default.
	currentTZ, err = CheckCurrentTimeZone(db)
	if err != nil {
		return
	}

	// Switch to DEFAULT/LOCAL.
	_, err = db.Exec(`SET TIME ZONE DEFAULT`)
	if err != nil {
		err = fmt.Errorf("failed to set time zone to UTC: %v", err)
		return
	}

	// Get the default time zone now that it is current.
	defaultTZ, err = CheckCurrentTimeZone(db)
	if err != nil {
		return
	}

	// Switch back to initial time zone.
	_, err = db.Exec(fmt.Sprintf(`SET TIME ZONE %s`, currentTZ))
	if err != nil {
		err = fmt.Errorf("failed to set time zone back to %s: %v", currentTZ, err)
	}
	return
}

// DeleteDuplicates attempts to delete "duplicate" rows in tables where unique
// indexes are to be created.
func (pgb *ChainDB) DeleteDuplicates(barLoad chan *dbtypes.ProgressBarLoad) error {
	allDuplicates := []dropDuplicatesInfo{
		// Remove duplicate vins
		{TableName: "vins", DropDupsFunc: pgb.DeleteDuplicateVins},

		// Remove duplicate vouts
		{TableName: "vouts", DropDupsFunc: pgb.DeleteDuplicateVouts},

		// Remove duplicate transactions
		{TableName: "transactions", DropDupsFunc: pgb.DeleteDuplicateTxns},

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
