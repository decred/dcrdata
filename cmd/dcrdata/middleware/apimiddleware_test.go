// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package middleware

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/go-chi/chi/v5"
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
		wantCode int
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
			wantCode: http.StatusUnprocessableEntity,
		},
		{
			testName: "bad3",
			args: args{2, []string{"Dcur2mcGjmENx4DhNqDctW5wJCVyT3Qeqkx",
				"DseXBL6g6GxvfYAnKqdao2f7WkXDmYTYW87",
				"Dsi8hhDzr3SvcGcv4NEGvRqFkwZ2ncRhukk"}},
			want:     nil,
			wantErr:  true,
			errMsg:   "maximum of 2 addresses allowed",
			wantCode: http.StatusUnprocessableEntity,
		},
		{
			// This tests that the middleware counts before removing dups.
			testName: "bad_dup3",
			args: args{2, []string{"Dsi8hhDzr3SvcGcv4NEGvRqFkwZ2ncRhukk",
				"DseXBL6g6GxvfYAnKqdao2f7WkXDmYTYW87",
				"Dsi8hhDzr3SvcGcv4NEGvRqFkwZ2ncRhukk"}},
			want:     nil,
			wantErr:  true,
			errMsg:   "maximum of 2 addresses allowed",
			wantCode: http.StatusUnprocessableEntity,
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
			errMsg:   `invalid address "DcxxxxcGjmENx4DhNqDctW5wJCVyT3Qeqkx" for this network: checksum mismatch`},
		{
			testName: "wrong_net",
			args:     args{2, []string{"TsWmwignm9Q6iBQMSHw9WhBeR5wgUPpD14Q"}},
			want:     nil,
			wantErr:  true,
			errMsg:   `invalid address "TsWmwignm9Q6iBQMSHw9WhBeR5wgUPpD14Q" for this network: unknown address type`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			router := chi.NewRouter()
			tAddrCtx := AddressPathCtxN(tt.args.maxAddrs)
			var run bool
			router.With(tAddrCtx).Get("/{address}", func(w http.ResponseWriter, r *http.Request) {
				run = true
				got, err := GetAddressCtx(r, activeNetParams) //, tt.args.maxAddrs)
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
			writer := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/"+strings.Join(tt.args.addrs, ","), nil)
			router.ServeHTTP(writer, req)
			switch tt.wantCode {
			case 0, 200:
				if !run {
					t.Errorf("handler not reached")
				}
				if writer.Code != 200 {
					t.Errorf("expected 200, got %d", writer.Code)
				}
			default:
				if tt.wantCode != writer.Code {
					t.Errorf("expected response code %d, got %d", tt.wantCode, writer.Code)
				}
			}
		})
	}
}
