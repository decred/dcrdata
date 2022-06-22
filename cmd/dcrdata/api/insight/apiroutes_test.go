// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package insight

import (
	"reflect"
	"testing"
	"time"

	apitypes "github.com/decred/dcrdata/v8/api/types"
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

func Test_fromToForSlice(t *testing.T) {
	type args struct {
		from        int64
		to          int64
		sliceLength int64
		txLimit     int64
	}
	tests := []struct {
		testName  string
		args      args
		wantStart int64
		wantEnd   int64
		wantErr   bool
	}{
		{
			testName: "ok",
			args: args{
				from:        0,
				to:          1,
				sliceLength: 2,
				txLimit:     1000,
			},
			wantStart: 0,
			wantEnd:   2,
			wantErr:   false,
		},
		{
			testName: "ok, high to",
			args: args{
				from:        0,
				to:          3,
				sliceLength: 2,
				txLimit:     1000,
			},
			wantStart: 0,
			wantEnd:   2,
			wantErr:   false,
		},
		{
			testName: "ok, high to (edge)",
			args: args{
				from:        0,
				to:          2,
				sliceLength: 2,
				txLimit:     1000,
			},
			wantStart: 0,
			wantEnd:   2,
			wantErr:   false,
		},
		{
			testName: "ok, at limit",
			args: args{
				from:        0,
				to:          999,
				sliceLength: 1000,
				txLimit:     1000,
			},
			wantStart: 0,
			wantEnd:   1000,
			wantErr:   false,
		},
		{
			testName: "ok, one element",
			args: args{
				from:        1,
				to:          1,
				sliceLength: 2,
				txLimit:     1000,
			},
			wantStart: 1,
			wantEnd:   2,
			wantErr:   false,
		},
		{
			testName: "ok, high from",
			args: args{
				from:        1,
				to:          1,
				sliceLength: 1,
				txLimit:     1000,
			},
			wantStart: 0,
			wantEnd:   1,
			wantErr:   false,
		},
		{
			testName: "ok, low to",
			args: args{
				from:        6,
				to:          1,
				sliceLength: 20,
				txLimit:     1000,
			},
			wantStart: 6,
			wantEnd:   7,
			wantErr:   false,
		},
		{
			testName: "empty slice",
			args: args{
				from:        1,
				to:          1,
				sliceLength: 0,
				txLimit:     1000,
			},
			wantStart: 0,
			wantEnd:   0,
			wantErr:   true,
		},
		{
			testName: "over limit",
			args: args{
				from:        1,
				to:          20,
				sliceLength: 200,
				txLimit:     10,
			},
			wantStart: 1,
			wantEnd:   21,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			start, end, err := fromToForSlice(tt.args.from, tt.args.to, tt.args.sliceLength, tt.args.txLimit)
			if (err != nil) != tt.wantErr {
				t.Errorf("fromToForSlice() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if start != tt.wantStart {
				t.Errorf("fromToForSlice() start = %v, want %v", start, tt.wantStart)
			}
			if end != tt.wantEnd {
				t.Errorf("fromToForSlice() end = %v, want %v", end, tt.wantEnd)
			}
		})
	}
}

func Test_removeSliceElements(t *testing.T) {
	type outSlice []*apitypes.AddressTxnOutput
	type args struct {
		txOuts outSlice
		inds   []int
	}
	tests := []struct {
		name string
		args args
		want outSlice
	}{
		{
			name: "ok out of range inds",
			args: args{
				txOuts: outSlice{{Address: "i0"}},
				inds:   []int{6},
			},
			want: outSlice{{Address: "i0"}},
		},
		{
			name: "ok two inds",
			args: args{
				txOuts: outSlice{{Address: "i0"}, {Address: "i1"}, {Address: "i2"},
					{Address: "i3"}, {Address: "i4"}, {Address: "i5"}, {Address: "i6"}},
				inds: []int{1, 6},
			},
			want: outSlice{{Address: "i0"}, {Address: "i5"}, {Address: "i2"},
				{Address: "i3"}, {Address: "i4"}}, // [6] removed, then [1] overwritten with [5]
		},
		{
			name: "ok emptied slice",
			args: args{
				txOuts: outSlice{{Address: "i0"}},
				inds:   []int{0},
			},
			want: outSlice{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeSliceElements(tt.args.txOuts, tt.args.inds)
			if len(got) != len(tt.want) {
				t.Errorf("removeSliceElements() = %v, want %v", got, tt.want)
			}
			for i := range got {
				t.Logf("got[i]: %v", got[i])
				if got[i].Address != tt.want[i].Address {
					t.Errorf("got[%[1]d].Address = %[2]v, want[%[1]d].Address = %[3]v",
						i, got[i].Address, tt.want[i].Address)
				}
			}
		})
	}
}
