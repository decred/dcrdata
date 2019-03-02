package types

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestTimeDefMarshal(t *testing.T) {
	tref := time.Unix(1548363687, 0)
	trefJSON := `"` + tref.Format(timeDefFmtJS) + `"`

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

func TestTimeDefUnmarshal(t *testing.T) {
	tref := time.Unix(1548363687, 0).UTC()
	trefJSON := tref.Format(timeDefFmtJS)

	timedef := new(TimeDef)
	err := timedef.UnmarshalJSON([]byte(trefJSON))
	if err != nil {
		t.Errorf("UnmarshalJSON failed: %v", err)
	}

	if timedef.T != tref {
		t.Errorf("expected %v, got %v", tref, timedef.T)
	}
}

func TestDeepCopys(t *testing.T) {
	tickets := []MempoolTx{
		{
			TxID:      "96e10d7ce108b1a357168b0a923d86d2744ba9777a2d81cbff71ffb982381c95",
			Fees:      0.0001,
			VinCount:  2,
			VoutCount: 5,
			Vin: []MempoolInput{
				{
					TxId:   "43f26841e744ce2f901e21400f275eda27ba2a3fa962110d52e7dd37f2193c78",
					Index:  0,
					Outdex: 0,
				},
				{
					TxId:   "43f26841e744ce2f901e21400f275eda27ba2a3fa962110d52e7dd37f2193c78",
					Index:  1,
					Outdex: 1,
				},
			},
			Hash:     "96e10d7ce108b1a357168b0a923d86d2744ba9777a2d81cbff71ffb982381c95",
			Size:     539,
			TotalOut: 106.39717461,
			Type:     "Ticket",
		},
		{
			TxID:      "8eb2f6c8f3a9cdc8d6de2ef3bfca9efcffed4484dd4fde2d01dc0fc0e415c75a",
			Fees:      0.0001,
			VinCount:  2,
			VoutCount: 5,
			Vin: []MempoolInput{
				{
					TxId:   "4e9221f790916b4d891b40ef82b8a6dc89f5c0719d5d5ddcf46ac3673d8446aa",
					Index:  0,
					Outdex: 0,
				},
				{
					TxId:   "4e9221f790916b4d891b40ef82b8a6dc89f5c0719d5d5ddcf46ac3673d8446aa",
					Index:  1,
					Outdex: 1,
				},
			},
			Hash:     "8eb2f6c8f3a9cdc8d6de2ef3bfca9efcffed4484dd4fde2d01dc0fc0e415c75a",
			Size:     538,
			TotalOut: 106.39717461,
			Type:     "Ticket",
		},
	}

	votes := []MempoolTx{
		{
			TxID:      "64ce0422cb6ba1aefa63c8df1d872250d181261ff3acd5a71bc1f521096207c9",
			Fees:      0,
			VinCount:  2,
			VoutCount: 3,
			Vin: []MempoolInput{
				{
					TxId:   "",
					Index:  0,
					Outdex: 0,
				},
				{
					TxId:   "7884ecd8fb5934e77708f82f0aa052ad86cccc5749602be14d93745d8272538e",
					Index:  1,
					Outdex: 0,
				},
			},
			Hash:     "64ce0422cb6ba1aefa63c8df1d872250d181261ff3acd5a71bc1f521096207c9",
			Size:     344,
			TotalOut: 102.86278351,
			Type:     "Vote",
		},
		{
			TxID:      "07aa38f10fe1a849a52b9d4812081854e4ac7268751a0ea661e8f499d7de91f1",
			Fees:      0,
			VinCount:  2,
			VoutCount: 3,
			Vin: []MempoolInput{
				{
					TxId:   "",
					Index:  0,
					Outdex: 0,
				},
				{
					TxId:   "99045541c481e7e694598a2d77967f5e4e053cee3265d77c5b49f1eb8b282176",
					Index:  1,
					Outdex: 0,
				},
			},
			Hash:     "07aa38f10fe1a849a52b9d4812081854e4ac7268751a0ea661e8f499d7de91f1",
			Size:     345,
			TotalOut: 105.29923146,
			Type:     "Vote",
		},
		{
			TxID:      "1df658e1b0de08112adcfb9b8b17dcc2b64f756b1e21f6b1f715fd2b86439955",
			Fees:      0,
			VinCount:  2,
			VoutCount: 3,
			Vin: []MempoolInput{
				{
					TxId:   "",
					Index:  0,
					Outdex: 0,
				},
				{
					TxId:   "28cc0b43bf79908115323f16dbd17d0e44a5366ca5d49e2d4f5a9c5f741e5699",
					Index:  1,
					Outdex: 0,
				},
			},
			Hash:     "1df658e1b0de08112adcfb9b8b17dcc2b64f756b1e21f6b1f715fd2b86439955",
			Size:     345,
			TotalOut: 111.28226529,
			Type:     "Vote",
		},
	}

	regular := []MempoolTx{
		{
			TxID:      "0572b2d121322d3a9b20fe5d5024c73d8bb817398948a167ddb668e52bbb21f6",
			Fees:      0.000585,
			VinCount:  3,
			VoutCount: 2,
			Vin: []MempoolInput{
				{
					TxId:   "4a9aaca49784586d3abc0dbd5d7d3dcdf70940c60bc5cbaa39379690d9ac5c6d",
					Index:  0,
					Outdex: 9,
				},
				{
					TxId:   "f621d45fb440307f151c1470619e37209aca7f8c12379e82f5c2ebcf882fb884",
					Index:  1,
					Outdex: 1,
				},
				{
					TxId:   "08b01afd1c252fbef8bbad933c1d7e3da1d3e3011ef3d4cdd532f5803ea173b9",
					Index:  2,
					Outdex: 0,
				},
			},
			Hash:     "0572b2d121322d3a9b20fe5d5024c73d8bb817398948a167ddb668e52bbb21f6",
			Size:     581,
			TotalOut: 139.11389736,
			Type:     "Regular",
		},
		{
			TxID:      "9e11deaae5ecd1d3288468a491f820b66adfb74be70eba582c0b13a25e76bb3b",
			Fees:      0.000585,
			VinCount:  3,
			VoutCount: 2,
			Vin: []MempoolInput{
				{
					TxId:   "bf9d371a9f3fd510ec5d6b485c0fd64ca1b6dac9c3b915973ba8fc86fc788e8c",
					Index:  0,
					Outdex: 1,
				},
				{
					TxId:   "a245d62d3916869f930afd80dce6f47c7291145c36fccae7ba73c0e462ff4cd5",
					Index:  1,
					Outdex: 1,
				},
				{
					TxId:   "b8274d92cac36a08cc28600fec66a09e9d429486506da1c8616c93544ce0f2ee",
					Index:  2,
					Outdex: 2,
				},
			},
			Hash:     "9e11deaae5ecd1d3288468a491f820b66adfb74be70eba582c0b13a25e76bb3b",
			Size:     580,
			TotalOut: 204.94920773,
			Type:     "Regular",
		},
	}

	allTxns := regular
	allTxns = append(allTxns, votes...)
	allTxns = append(allTxns, tickets...)

	allCopy := CopyMempoolTxSlice(allTxns)
	if !reflect.DeepEqual(allTxns, allCopy) {
		t.Errorf("MempoolTx slices not equal: %v\n\n%v\n", allTxns, allCopy)
	}

	latest := regular
	latest = append(latest, tickets...)
	latest = append(latest, votes[0])

	invRegular := make(map[string]struct{}, len(regular))
	for i := range regular {
		invRegular[regular[i].TxID] = struct{}{}
	}

	invStake := make(map[string]struct{}, len(votes)+len(tickets))
	for i := range votes {
		invStake[votes[i].TxID] = struct{}{}
	}
	for i := range tickets {
		invStake[tickets[i].TxID] = struct{}{}
	}

	mps := &MempoolShort{
		LastBlockHash:      "000000000000000043a1e65fe3309ab5b2a0f4fb3e46036bbee2be6294790c98",
		LastBlockHeight:    310278,
		LastBlockTime:      1547677417,
		FormattedBlockTime: (TimeDef{T: time.Unix(1547677417, 0)}).String(),
		Time:               1547677417 + 1e5,
		TotalOut:           1292.76211530,
		TotalSize:          2479,
		NumTickets:         2,
		NumVotes:           3,
		NumRegular:         2,
		NumRevokes:         0,
		NumAll:             7,
		LikelyMineable: LikelyMineable{
			Total:         1292.76211530,
			Size:          2479,
			FormattedSize: "134134 B",
			RegularTotal:  200.52,
			TicketTotal:   100.86,
			VoteTotal:     300.45,
			RevokeTotal:   0,
			Count:         7,
		},
		LatestTransactions: latest,
		FormattedTotalSize: "134134 B",
		TicketIndexes: BlockValidatorIndex{
			"00000000000000003b6e0e24c75575911edabbbae181fc6e8e686c6aadcc2ce2": TicketIndex{
				"28cc0b43bf79908115323f16dbd17d0e44a5366ca5d49e2d4f5a9c5f741e5699": 0,
				"0969ba6af7da6b63b122e0c9d57e743397ca0bc0ad39ca1927422db0e70ec19b": 1,
				"99045541c481e7e694598a2d77967f5e4e053cee3265d77c5b49f1eb8b282176": 2,
				"a5f35e94af7945f06adba39598faa4c65d9063fc1c4e7d502eb349c924364a10": 3,
				"7884ecd8fb5934e77708f82f0aa052ad86cccc5749602be14d93745d8272538e": 4,
			},
		},
		VotingInfo: VotingInfo{
			TicketsVoted:     3,
			MaxVotesPerBlock: 5,
			VotedTickets: map[string]bool{
				"7884ecd8fb5934e77708f82f0aa052ad86cccc5749602be14d93745d8272538e": true,
				"28cc0b43bf79908115323f16dbd17d0e44a5366ca5d49e2d4f5a9c5f741e5699": true,
				"99045541c481e7e694598a2d77967f5e4e053cee3265d77c5b49f1eb8b282176": false,
			},
			VoteTallys: map[string]*VoteTally{
				"de563e0f0ee0f4717c553ce456fa5ff37c784e3f52059f2e3e64ddfbcf2aaffb": {
					TicketsPerBlock: 5,
					Marks:           []bool{true, true, true, true, true},
				},
			},
		},
		InvRegular: invRegular,
		InvStake:   invStake,
	}

	mps2 := mps.DeepCopy()

	if !reflect.DeepEqual(*mps, *mps2) {
		t.Errorf("MempoolShort structs not equal: %v\n\n%v\n", *mps, *mps2)
	}

	mpi := &MempoolInfo{
		MempoolShort: *mps,
		Transactions: regular,
		Tickets:      tickets,
		Votes:        votes,
		Revocations:  nil,
	}

	mpi2 := mpi.DeepCopy()

	copts := cmpopts.IgnoreTypes(sync.RWMutex{})
	if !cmp.Equal(mpi, mpi2, copts) {
		t.Errorf("MempoolInfo structs not equal: \n%s\n", cmp.Diff(mpi, mpi2, copts))
	}
}
