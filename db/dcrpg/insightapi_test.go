// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package dcrpg

import (
	"reflect"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
)

func Test_sortTxsByTimeAndHash(t *testing.T) {
	h0, _ := chainhash.NewHashFromStr("79936a1fb658ba249443f0caf4a6a44ce73afe16d543d4f7b8dcf847dfb21a9d")
	h1, _ := chainhash.NewHashFromStr("484e1a03f7c3795b468d7d46071e73e207aec8295917dec569a76cca981f1b95")
	tests := []struct {
		name string
		txns []txSortable
		want []txSortable
	}{
		{
			name: "ok",
			txns: []txSortable{
				{
					Hash: *h0,
					Time: 16341234,
				},
				{
					Hash: *h1,
					Time: 12341234,
				},
				{
					Hash: *h0,
					Time: 12341234,
				},
				{
					Hash: *h1,
					Time: 14341234,
				},
			},
			want: []txSortable{
				{
					Hash: *h0,
					Time: 16341234,
				},
				{
					Hash: *h1,
					Time: 14341234,
				},
				{
					Hash: *h1,
					Time: 12341234,
				},
				{
					Hash: *h0,
					Time: 12341234,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortTxsByTimeAndHash(tt.txns)
			if !reflect.DeepEqual(tt.txns, tt.want) {
				t.Errorf("txsByTimeAndHash failed. Wanted %v, got %v.",
					tt.want, tt.txns)
			}
		})
	}
}
