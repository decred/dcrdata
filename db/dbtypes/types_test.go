package dbtypes

import (
	"testing"
	"time"
)

const (
	trefUNIX = 1454954400
	trefStr  = "2016-02-08T12:00:00-06:00"
)

var (
	// Two times in different locations for the same instant in time.
	trefLocal = time.Unix(trefUNIX, 0).Local()
	trefUTC   = time.Unix(trefUNIX, 0).UTC()
)

func TestTimeDefMarshal(t *testing.T) {
	tref := time.Unix(trefUNIX, 0)
	trefJSON := `"` + tref.Format(timeDefFmtJS) + `"`
	t.Log(trefJSON)

	timedef := &TimeDef{
		T: tref,
	}
	jsonTime, err := timedef.MarshalJSON()
	if err != nil {
		t.Errorf("MarshalJSON failed: %v", err)
	}

	if string(jsonTime) != trefJSON {
		t.Errorf("expected %s, got %s", trefJSON, string(jsonTime))
	}
}

func TestNewTimeDef(t *testing.T) {
	// Create a time with Local location.
	tref, err := time.Parse(time.RFC3339, trefStr)
	if err != nil {
		t.Error(err)
	}
	tref = tref.Local()

	// Create the TimeDef, and verify that the location is now UTC.
	td := NewTimeDef(tref)
	if td.T.Location() != time.UTC {
		t.Errorf("NewTimeDef should return a time in UTC (not local).")
	}

	t.Log(td)
}

func TestTimeDef_Value(t *testing.T) {
	// Create the TimeDef from a Local time.
	td := NewTimeDef(trefLocal)
	// Verify the Location of the time returned by Value is UTC.
	tdSqlValue, _ := td.Value()
	tdSqlTime, ok := tdSqlValue.(time.Time)
	if !ok {
		t.Error("not a time.Time")
	}
	t.Log(tdSqlTime)
	if tdSqlTime.Location() != time.UTC {
		t.Errorf("TimeDef.Value should return a UTC time.")
	}

	// Create the TimeDef from the equivalent time in UTC.
	td2 := NewTimeDef(trefUTC)
	// Verify the Location of the time returned by Value is UTC.
	tdSqlValue2, _ := td2.Value()
	tdSqlTime2, ok := tdSqlValue2.(time.Time)
	if !ok {
		t.Error("not a time.Time")
	}
	t.Log(tdSqlTime2)
	if tdSqlTime2.Location() != time.UTC {
		t.Errorf("TimeDef.Value should return a UTC time.")
	}

	// Verify the string formats of the time.Time are the same.
	if tdSqlTime.String() != tdSqlTime2.String() {
		t.Errorf("time strings do not match: %s != %s",
			tdSqlTime.String(), tdSqlTime2.String())
	}

	// Verify the instants in time are the same.
	if tdSqlTime.Unix() != tdSqlTime2.Unix() {
		t.Logf("unix epoch times do not match: %d != %d",
			tdSqlTime.Unix(), tdSqlTime2.Unix())
	}

	// Create the TimeDef from a Local time, but do not use the constructor.
	// This shows that Value will ensure the correct time.Time in UTC for sql
	// regardless of the location of TimeDef.T.
	td3 := TimeDef{T: trefLocal}
	// Verify the Location of the time returned by Value is UTC.
	tdSqlValue3, _ := td3.Value()
	tdSqlTime3, ok := tdSqlValue3.(time.Time)
	if !ok {
		t.Error("not a time.Time")
	}
	t.Log(tdSqlTime3)
	if tdSqlTime3.Location() != time.UTC {
		t.Errorf("TimeDef.Value should return a UTC time.")
	}
}

func TestTimeDef_Scan(t *testing.T) {
	// Scan the reference time with Local Location.
	var td TimeDef
	err := td.Scan(trefLocal)
	if err != nil {
		t.Fatal(err)
	}

	// TimeDef.T location should be stored as UTC.
	if td.T.Location() != time.UTC {
		t.Errorf("TimeDef.Value should return a UTC time.")
	}

	t.Log(td)

	// Scan the reference time with UTC Location.
	var td2 TimeDef
	err = td2.Scan(trefUTC)
	if err != nil {
		t.Fatal(err)
	}

	// TimeDef.T location should be stored as UTC.
	if td2.T.Location() != time.UTC {
		t.Errorf("TimeDef.Value should return a UTC time.")
	}

	t.Log(td)

	// Ensure they are the same instant in time.
	if td.T.Unix() != td2.T.Unix() {
		t.Logf("unix epoch times do not match: %d != %d",
			td.T.Unix(), td2.T.Unix())
	}

	// Verify the string formats of the time.Time are the same.
	if td.T.String() != td2.T.String() {
		t.Errorf("time strings do not match: %s != %s",
			td.T.String(), td2.T.String())
	}

	// Scan with an unsupported type.
	var td3 TimeDef
	err = td3.Scan(trefUNIX)
	if err == nil {
		t.Fatal("TimeDef.Scan(int64) should have failed")
	}

}
