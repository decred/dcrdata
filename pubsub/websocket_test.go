// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package pubsub

import (
	"errors"
	"testing"

	pstypes "github.com/decred/dcrdata/v8/pubsub/types"
)

func Test_client_subscribe(t *testing.T) {
	tests := []struct {
		name    string
		cl      *client
		hubMsg  pstypes.HubMessage
		wantErr error
		wantOK  bool
	}{
		{"ping not a sub", newClient(), pstypes.HubMessage{Signal: sigPingAndUserCount}, nil, false},
		{"ok newtx", newClient(), pstypes.HubMessage{Signal: sigNewTx}, nil, true},
		{"ok addr", newClient(), pstypes.HubMessage{
			Signal: sigAddressTx,
			Msg:    &pstypes.AddressMessage{Address: "DsfX4WrSecUwGoRd9B7Lz1JjYssYaVKnjGC"},
		}, nil, true},
		{"bad addr", newClient(), pstypes.HubMessage{
			Signal: sigAddressTx,
			Msg:    pstypes.AddressMessage{Address: "DsfX4WrSecUwGoRd9B7Lz1JjYssYaVKnjGC"},
		}, errors.New("msg.Msg not a string (SigAddressTx): types.AddressMessage"), false},
		{"bad addr", newClient(), pstypes.HubMessage{
			Signal: sigAddressTx,
			Msg:    nil,
		}, errors.New("msg.Msg not a string (SigAddressTx): <nil>"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := tt.cl.subscribe(tt.hubMsg)
			if (err != nil) != (tt.wantErr != nil) ||
				(err != nil && err.Error() != tt.wantErr.Error()) {
				t.Errorf(`subscribe() error = "%v", wantErr "%v"`, err, tt.wantErr)
				return
			}
			if ok != tt.wantOK {
				t.Errorf("Did not subscribe to %v.", tt.hubMsg)
			}
		})
	}
}
