// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package middleware

import (
	"context"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/decred/dcrd/chaincfg/v2"
)

func TestGetAddressCtx(t *testing.T) {
	activeNetParams := chaincfg.MainNetParams()
	type args struct {
		maxAddrs int
		addrs    []string
	}
	tests := []struct {
		testName string
		args     args
		want     []string
		wantErr  bool
		errMsg   string
	}{
		{
			testName: "ok2",
			args:     args{2, []string{"Dcur2mcGjmENx4DhNqDctW5wJCVyT3Qeqkx"}},
			want:     []string{"Dcur2mcGjmENx4DhNqDctW5wJCVyT3Qeqkx"},
			wantErr:  false,
		},
		{
			testName: "ok1",
			args:     args{1, []string{"Dcur2mcGjmENx4DhNqDctW5wJCVyT3Qeqkx"}},
			want:     []string{"Dcur2mcGjmENx4DhNqDctW5wJCVyT3Qeqkx"},
			wantErr:  false,
		},
		{
			testName: "bad0",
			args:     args{0, []string{"Dcur2mcGjmENx4DhNqDctW5wJCVyT3Qeqkx"}},
			want:     nil, // not []string{}
			wantErr:  true,
			errMsg:   "maximum of 0 addresses allowed",
		},
		{
			testName: "bad3",
			args: args{2, []string{"Dcur2mcGjmENx4DhNqDctW5wJCVyT3Qeqkx",
				"DseXBL6g6GxvfYAnKqdao2f7WkXDmYTYW87",
				"Dsi8hhDzr3SvcGcv4NEGvRqFkwZ2ncRhukk"}},
			want:    nil,
			wantErr: true,
			errMsg:  "maximum of 2 addresses allowed",
		},
		{
			// This tests that the middleware counts before removing dups.
			testName: "bad_dup3",
			args: args{2, []string{"Dsi8hhDzr3SvcGcv4NEGvRqFkwZ2ncRhukk",
				"DseXBL6g6GxvfYAnKqdao2f7WkXDmYTYW87",
				"Dsi8hhDzr3SvcGcv4NEGvRqFkwZ2ncRhukk"}},
			want:    nil,
			wantErr: true,
			errMsg:  "maximum of 2 addresses allowed",
		},
		{
			// This tests that the middleware counts removes dups.
			testName: "ok_dup3",
			args: args{3, []string{"Dsi8hhDzr3SvcGcv4NEGvRqFkwZ2ncRhukk",
				"DseXBL6g6GxvfYAnKqdao2f7WkXDmYTYW87",
				"Dsi8hhDzr3SvcGcv4NEGvRqFkwZ2ncRhukk"}},
			want: []string{"Dsi8hhDzr3SvcGcv4NEGvRqFkwZ2ncRhukk",
				"DseXBL6g6GxvfYAnKqdao2f7WkXDmYTYW87"},
			wantErr: false,
		},
		{
			testName: "invalid",
			args:     args{2, []string{"DcxxxxcGjmENx4DhNqDctW5wJCVyT3Qeqkx"}},
			want:     nil,
			wantErr:  true,
			errMsg:   "invalid address 'DcxxxxcGjmENx4DhNqDctW5wJCVyT3Qeqkx': checksum mismatch",
		},
		{
			testName: "not_set",
			args:     args{2, nil},
			want:     nil,
			wantErr:  true,
			errMsg:   "address not set",
		},
		{
			testName: "wrong_net",
			args:     args{2, []string{"TsWmwignm9Q6iBQMSHw9WhBeR5wgUPpD14Q"}},
			want:     nil,
			wantErr:  true,
			errMsg:   `TsWmwignm9Q6iBQMSHw9WhBeR5wgUPpD14Q is invalid for this network`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			addrs := strings.Join(tt.args.addrs, ",")
			if len(addrs) > 0 {
				ctx := context.WithValue(r.Context(), CtxAddress, addrs)
				r = r.WithContext(ctx)
			}

			got, err := GetAddressCtx(r, activeNetParams, tt.args.maxAddrs)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetAddressCtx() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetAddressCtx() = %v, want %v", got, tt.want)
			}
			if err != nil && tt.errMsg != err.Error() {
				t.Fatalf(`GetAddressCtx() error = "%v", expected "%s"`, err, tt.errMsg)
			}
		})
	}
}
