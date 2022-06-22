//go:build pgonline

package dcrpg

import (
	"testing"
	"time"

	"github.com/decred/dcrdata/v8/db/dbtypes"
)

var (
	// Two times in different locations for the same instant in time.
	trefLocal = time.Unix(trefUNIX, 0).Local()
	trefUTC   = time.Unix(trefUNIX, 0).UTC()
)

const (
	insertTestingTimestamp = `INSERT INTO testing (timestamp) VALUES ($1) RETURNING id;`
	selectTestingTimestamp = `SELECT timestamp FROM testing;`
)

func TestTimeRoundTripCorrectTimeDef(t *testing.T) {
	// Clear the testing table.
	if err := ClearTestingTable(sqlDb); err != nil {
		t.Fatalf("Failed to clear the testing table: %v", err)
	}

	// tref := time.Unix(trefUNIX, 0)               // time.Time of genesis block.
	// timedef := &dbtypes.TimeDef{T: tref}         // No constructor, location not changed (Local from time.Unix).
	// timedef := dbtypes.NewTimeDef(tref)          // Constructor sets location to UTC.
	timedef := dbtypes.NewTimeDefFromUNIX(trefUNIX) // Alt. constructor also sets location to UTC.

	t.Logf("Inserting TimeDef at %d. Location set to: %v", timedef.UNIX(),
		timedef.T.Location()) // Inserting TimeDef at 1454954400. Location set to: UTC

	var id uint64
	err := sqlDb.QueryRow(insertTestingTimestamp, timedef).Scan(&id)
	if err != nil {
		t.Error(err)
	}

	var tsScanned dbtypes.TimeDef
	err = sqlDb.QueryRow(selectTestingTimestamp).Scan(&tsScanned)
	if err != nil {
		t.Error(err)
	}

	t.Logf("Scanned TimeDef at %d. Location set to: %v", tsScanned.UNIX(),
		tsScanned.T.Location()) // Scanned TimeDef at 1454954400. Location set to: UTC  <== correct

	if tsScanned.UNIX() != timedef.UNIX() {
		t.Errorf("Time did not survive round trip: got %v, expected %v",
			tsScanned.UNIX(), timedef.UNIX())
	}

	if tsScanned.T.Location() != time.UTC {
		t.Errorf("scanned time was not UTC: %v", tsScanned.T.Location())
	}
}

func TestTimeRoundTripCorrectValuer(t *testing.T) {
	// Clear the testing table.
	if err := ClearTestingTable(sqlDb); err != nil {
		t.Fatalf("Failed to clear the testing table: %v", err)
	}

	tref := time.Unix(trefUNIX, 0)       // time.Time of genesis block.
	timedef := &dbtypes.TimeDef{T: tref} // No constructor, location not changed (Local from time.Unix).
	// timedef := dbtypes.NewTimeDef(tref)             // Constructor sets location to UTC.
	// timedef := dbtypes.NewTimeDefFromUNIX(trefUNIX) // Alt. constructor also sets location to UTC.

	t.Logf("Inserting TimeDef at %d. Location set to: %v", timedef.UNIX(),
		timedef.T.Location()) // Inserting TimeDef at 1454954400. Location set to: Local

	// TimeDef.Value converts the time.Time into UTC on the fly, so the stored
	// value will be correct regardless of the location of the TimeDef.
	var id uint64
	err := sqlDb.QueryRow(insertTestingTimestamp, timedef).Scan(&id)
	if err != nil {
		t.Error(err)
	}

	var tsScanned dbtypes.TimeDef
	err = sqlDb.QueryRow(selectTestingTimestamp).Scan(&tsScanned)
	if err != nil {
		t.Error(err)
	}

	t.Logf("Scanned TimeDef at %d. Location set to: %v", tsScanned.UNIX(),
		tsScanned.T.Location()) // Scanned TimeDef at 1454954400. Location set to: UTC  <== correct

	if tsScanned.UNIX() != timedef.UNIX() {
		t.Errorf("Time did not survive round trip: got %v, expected %v",
			tsScanned.UNIX(), timedef.UNIX())
	}

	if tsScanned.T.Location() != time.UTC {
		t.Errorf("scanned time was not UTC: %v", tsScanned.T.Location())
	}
}

func TestTimeRoundTripIncorrectValuerTime(t *testing.T) {
	// Clear the testing table.
	if err := ClearTestingTable(sqlDb); err != nil {
		t.Fatalf("Failed to clear the testing table: %v", err)
	}

	tref := time.Unix(trefUNIX, 0)       // time.Time of genesis block.
	timedef := &dbtypes.TimeDef{T: tref} // No constructor, location not changed (Local from time.Unix).
	// timedef := dbtypes.NewTimeDef(tref) // Constructor sets location to UTC.
	// timedef := dbtypes.NewTimeDefFromUNIX(trefUNIX) // Alt. constructor also sets location to UTC.

	t.Logf("Inserting TimeDef at %d. Location set to: %v", timedef.UNIX(),
		timedef.T.Location()) // Inserting TimeDef at 1454954400. Location set to: Local

	// Using time.Time with Location as Local instead of TimeDef, where Value
	// ensures UTC, stores the incorrect time.
	var id uint64
	err := sqlDb.QueryRow(insertTestingTimestamp, timedef.T).Scan(&id)
	if err != nil {
		t.Error(err)
	}

	var tsScanned dbtypes.TimeDef
	err = sqlDb.QueryRow(selectTestingTimestamp).Scan(&tsScanned)
	if err != nil {
		t.Error(err)
	}

	t.Logf("Scanned TimeDef at %d. Location set to: %v", tsScanned.UNIX(),
		tsScanned.T.Location()) // Scanned TimeDef at 1454932800. Location set to: UTC  <== incorrect

	// Negative test. The absolute time should not survive this way (storing
	// from time.Time in Local time).
	if tsScanned.UNIX() == timedef.UNIX() {
		t.Errorf("Time did not survive round trip: got %v, expected %v",
			tsScanned.UNIX(), timedef.UNIX())
	}
}

func TestTimeRoundTripIncorrectValuerTimeDefLocal(t *testing.T) {
	// Clear the testing table.
	if err := ClearTestingTable(sqlDb); err != nil {
		t.Fatalf("Failed to clear the testing table: %v", err)
	}

	// tref := time.Unix(trefUNIX, 0)       // time.Time of genesis block.
	// timedef := &dbtypes.TimeDef{T: tref} // No constructor, location not changed (Local from time.Unix).
	// timedef := dbtypes.NewTimeDef(tref) // Constructor sets location to UTC.
	timedef := dbtypes.NewTimeDefFromUNIX(trefUNIX) // Alt. constructor also sets location to UTC.

	t.Logf("Inserting TimeDef at %d. Location set to: %v", timedef.UNIX(),
		timedef.T.Location()) // Inserting TimeDef at 1454954400. Location set to: Local

	// Using dbtypes.TimeDefLocal, where Value ensures Local, stores the
	// incorrect time.
	var id uint64
	err := sqlDb.QueryRow(insertTestingTimestamp, dbtypes.TimeDefLocal(timedef)).Scan(&id)
	if err != nil {
		t.Error(err)
	}

	var tsScanned dbtypes.TimeDef
	err = sqlDb.QueryRow(selectTestingTimestamp).Scan(&tsScanned)
	if err != nil {
		t.Error(err)
	}

	t.Logf("Scanned TimeDef at %d. Location set to: %v", tsScanned.UNIX(),
		tsScanned.T.Location()) // Scanned TimeDef at 1454932800. Location set to: UTC  <== incorrect

	// Negative test. The absolute time should not survive this way (storing
	// from time.Time in Local time).
	if tsScanned.UNIX() == timedef.UNIX() {
		t.Errorf("Time did unexpectedly survived the round trip: got %v, expected %v",
			tsScanned.UNIX(), timedef.UNIX())
	}
}

// TestTimeTZRoundTripRobust demonstrates how the TIMESTAMPTZ data type
// does not drop a non-UTC offset when storing.  Thus, a time survives the round
// trip regardless of what the time.Time's Location.
func TestTimeTZRoundTripRobust(t *testing.T) {
	// Clear the testing table.
	if err := ClearTestingTable(sqlDb); err != nil {
		t.Fatalf("Failed to clear the testing table: %v", err)
	}

	tref := time.Unix(trefUNIX, 0)       // time.Time of genesis block.
	timedef := &dbtypes.TimeDef{T: tref} // No constructor, location not changed (Local from time.Unix).
	// timedef := dbtypes.NewTimeDef(tref) // Constructor sets location to UTC.
	// timedef := dbtypes.NewTimeDefFromUNIX(trefUNIX) // Alt. constructor also sets location to UTC.

	t.Logf("Inserting TimeDef at %d. Location set to: %v", timedef.UNIX(),
		timedef.T.Location()) // Inserting TimeDef at 1454954400. Location set to: Local

	// Using time.Time with Location as Local instead of TimeDef, where Value
	// ensures UTC, stores the incorrect time.
	var id uint64
	err := sqlDb.QueryRow(`INSERT INTO testing (timestamptz) VALUES ($1) RETURNING id;`,
		timedef.T).Scan(&id)
	if err != nil {
		t.Error(err)
	}

	var tsScanned dbtypes.TimeDef
	err = sqlDb.QueryRow(`SELECT timestamptz FROM testing;`).Scan(&tsScanned)
	if err != nil {
		t.Error(err)
	}

	t.Logf("Scanned TimeDef at %d. Location set to: %v", tsScanned.UNIX(),
		tsScanned.T.Location()) // Scanned TimeDef at 1454932800. Location set to: UTC  <== incorrect

	if tsScanned.UNIX() != timedef.UNIX() {
		t.Errorf("Time did not survive round trip: got %v, expected %v",
			tsScanned.UNIX(), timedef.UNIX())
	}

	if tsScanned.T.Location() != time.UTC {
		t.Errorf("scanned time was not UTC: %v", tsScanned.T.Location())
	}
}

// TestTimeTZRoundTripCorrectTimeDef ensures that the TimeDef Value and Scan
// implementations are also compatible with TIMESTAMPTZ.
func TestTimeTZRoundTripCorrectTimeDef(t *testing.T) {
	// Clear the testing table.
	if err := ClearTestingTable(sqlDb); err != nil {
		t.Fatalf("Failed to clear the testing table: %v", err)
	}

	// tref := time.Unix(trefUNIX, 0)               // time.Time of genesis block.
	// timedef := &dbtypes.TimeDef{T: tref}         // No constructor, location not changed (Local from time.Unix).
	// timedef := dbtypes.NewTimeDef(tref)          // Constructor sets location to UTC.
	timedef := dbtypes.NewTimeDefFromUNIX(trefUNIX) // Alt. constructor also sets location to UTC.

	t.Logf("Inserting TimeDef at %d. Location set to: %v", timedef.UNIX(),
		timedef.T.Location()) // Inserting TimeDef at 1454954400. Location set to: UTC

	var id uint64
	err := sqlDb.QueryRow(insertTestingTimestamp, timedef).Scan(&id)
	if err != nil {
		t.Error(err)
	}

	var tsScanned dbtypes.TimeDef
	err = sqlDb.QueryRow(selectTestingTimestamp).Scan(&tsScanned)
	if err != nil {
		t.Error(err)
	}

	t.Logf("Scanned TimeDef at %d. Location set to: %v", tsScanned.UNIX(),
		tsScanned.T.Location()) // Scanned TimeDef at 1454954400. Location set to: UTC  <== correct

	if tsScanned.UNIX() != timedef.UNIX() {
		t.Errorf("Time did not survive round trip: got %v, expected %v",
			tsScanned.UNIX(), timedef.UNIX())
	}

	if tsScanned.T.Location() != time.UTC {
		t.Errorf("scanned time was not UTC: %v", tsScanned.T.Location())
	}
}
