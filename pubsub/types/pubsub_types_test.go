package types

import (
	"testing"

	exptypes "github.com/decred/dcrdata/v8/explorer/types"
)

func TestHubSignal_String(t *testing.T) {
	tests := []struct {
		name string
		s    HubSignal
		want string
	}{
		{"ok", SigNewTx, "newtx"},
		{"ok", SigNewTxs, "newtxs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.String(); got != tt.want {
				t.Errorf("HubSignal.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHubMessage_String(t *testing.T) {
	tests := []struct {
		name       string
		hubMessage HubMessage
		want       string
	}{
		{
			"ok newblock",
			HubMessage{Signal: SigNewBlock},
			"newblock",
		},
		{
			"ok address",
			HubMessage{
				Signal: SigAddressTx,
				Msg: &AddressMessage{
					Address: "DsgRwmcnwLrNaY3gsrn2MXGMmaKAymnnFUR",
					TxHash:  "992cf0fa8fcb88f0cfa9a9808a02907c0a66a39ba588f1434c3bd779feb530e0",
				},
			},
			"address:DsgRwmcnwLrNaY3gsrn2MXGMmaKAymnnFUR:992cf0fa8fcb88f0cfa9a9808a02907c0a66a39ba588f1434c3bd779feb530e0",
		},
		{
			"ok newtx",
			HubMessage{Signal: SigNewTx, Msg: &exptypes.MempoolTx{Hash: "4811246cb13f6e74c8c661242064664aba79e0baaae273c320b884cf461b28d7"}},
			"newtx:4811246cb13f6e74c8c661242064664aba79e0baaae273c320b884cf461b28d7",
		},
		{
			"ok newtxs",
			HubMessage{Signal: SigNewTxs, Msg: []*exptypes.MempoolTx{{Hash: "4811246cb13f6e74c8c661242064664aba79e0baaae273c320b884cf461b28d7"}}},
			"newtxs:len=1",
		},
		{
			"wrong Msg type newtx",
			HubMessage{Signal: SigNewTx, Msg: exptypes.MempoolTx{Hash: "4811246cb13f6e74c8c661242064664aba79e0baaae273c320b884cf461b28d7"}},
			"invalid",
		},
		{
			"wrong Msg type newtxs",
			HubMessage{Signal: SigNewTxs, Msg: &exptypes.MempoolTx{Hash: "4811246cb13f6e74c8c661242064664aba79e0baaae273c320b884cf461b28d7"}},
			"invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.hubMessage.String(); got != tt.want {
				t.Errorf(`HubMessage.String() = "%v", want "%v"`, got, tt.want)
			}
		})
	}
}
