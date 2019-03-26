// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package insight

import (
	"reflect"
	"testing"
	"time"
)

func Test_dateFromStr(t *testing.T) {
	ymdFormat := "2006-01-02"
	today := time.Now().UTC().Truncate(24 * time.Hour)
	tests := []struct {
		testName    string
		dateStr     string
		wantDate    time.Time
		wantIsToday bool
		wantErr     bool
	}{
		{
			testName:    "ok not today",
			dateStr:     "2018-04-18",
			wantDate:    time.Unix(1524009600, 0).UTC(),
			wantIsToday: false,
			wantErr:     false,
		},
		{
			testName:    "ok today",
			dateStr:     today.Format(ymdFormat),
			wantDate:    today,
			wantIsToday: true,
			wantErr:     false,
		},
		{
			testName:    "invalid date string",
			dateStr:     "da future",
			wantDate:    time.Time{},
			wantIsToday: false,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			gotDate, gotIsToday, err := dateFromStr(ymdFormat, tt.dateStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("dateFromStr() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotDate, tt.wantDate) {
				t.Errorf("dateFromStr() gotDate = %v, want %v", gotDate, tt.wantDate)
			}
			if gotIsToday != tt.wantIsToday {
				t.Errorf("dateFromStr() gotIsToday = %v, want %v", gotIsToday, tt.wantIsToday)
			}
		})
	}
}
