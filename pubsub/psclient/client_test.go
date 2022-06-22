package psclient

import (
	"encoding/json"
	"errors"
	"testing"

	pstypes "github.com/decred/dcrdata/v8/pubsub/types"
)

var msgMempool5Latest = &pstypes.WebSocketMessage{
	EventId: "mempool",
	Message: json.RawMessage(`{
		"block_height": 312590,
		"block_hash": "00000000000000002c8ce3113b78c76fcccb7ddc6f8c114bae2a054a66959745",
		"block_time": 1548362218,
		"time": 0,
		"total": 1000.4917563699998,
		"size": 3756,
		"num_tickets": 6,
		"num_votes": 2,
		"num_regular": 1,
		"num_revokes": 0,
		"num_all": 9,
		"latest": [
			{
				"txid": "3a5ec5e7de5ce5df46d0a3eaac519c51a1aaf5092b71ad743295a698915e5833",
				"fees": 0.000419,
				"vin_count": 2,
				"vout_count": 2,
				"vin": [
					{
						"txid": "0b591ca907cac9a16fc8d8d52a6dab86b051ac55fe2ff151db52cc4ffa06cb13",
						"index": 0,
						"vout": 1
					},
					{
						"txid": "c413b730af5292df717a4206e4511383b16a5bf4b22e4a577afb976a5efd387e",
						"index": 1,
						"vout": 2
					}
				],
				"coinbase": false,
				"hash": "3a5ec5e7de5ce5df46d0a3eaac519c51a1aaf5092b71ad743295a698915e5833",
				"time": 1548362229,
				"size": 415,
				"total": 113.15606752999999,
				"Type": "Regular"
			},
			{
				"txid": "91be1871e359575af3b4d740afe4c90eee2a6410c67dc99975d940190f864fbc",
				"fees": 0.0003,
				"vin_count": 1,
				"vout_count": 3,
				"vin": [
					{
						"txid": "3a5ec5e7de5ce5df46d0a3eaac519c51a1aaf5092b71ad743295a698915e5833",
						"index": 0,
						"vout": 0
					}
				],
				"coinbase": false,
				"hash": "91be1871e359575af3b4d740afe4c90eee2a6410c67dc99975d940190f864fbc",
				"time": 1548362229,
				"size": 296,
				"total": 111.46803222,
				"Type": "Ticket"
			},
			{
				"txid": "398d6af5eedc9f693298eaca242920269936f71685da3646b0f2f406c67efffd",
				"fees": 0.00055,
				"vin_count": 2,
				"vout_count": 5,
				"vin": [
					{
						"txid": "19f02b97d73ad93e72e04eb5cba7e60b0615a4eb8c23c7446326b8eabdbc5531",
						"index": 0,
						"vout": 0
					},
					{
						"txid": "19f02b97d73ad93e72e04eb5cba7e60b0615a4eb8c23c7446326b8eabdbc5531",
						"index": 1,
						"vout": 1
					}
				],
				"coinbase": false,
				"hash": "398d6af5eedc9f693298eaca242920269936f71685da3646b0f2f406c67efffd",
				"time": 1548362180,
				"size": 538,
				"total": 111.46803222,
				"Type": "Ticket"
			},
			{
				"txid": "aa24c3a74da4d2e652399da9786164ec078063ceca27fbb49d7f02646e465da9",
				"fees": 0.00055,
				"vin_count": 2,
				"vout_count": 5,
				"vin": [
					{
						"txid": "26718e159ea939eadbe88edba907c23273dc5e9c615567fa5d2f5cb8d7483721",
						"index": 0,
						"vout": 0
					},
					{
						"txid": "26718e159ea939eadbe88edba907c23273dc5e9c615567fa5d2f5cb8d7483721",
						"index": 1,
						"vout": 1
					}
				],
				"coinbase": false,
				"hash": "aa24c3a74da4d2e652399da9786164ec078063ceca27fbb49d7f02646e465da9",
				"time": 1548362179,
				"size": 539,
				"total": 111.46803222,
				"Type": "Ticket"
			},
			{
				"txid": "ac9203b46755c8580bca2a5d825e773079a8936194077884d247273003f9cc48",
				"fees": 0.00055,
				"vin_count": 2,
				"vout_count": 5,
				"vin": [
					{
						"txid": "95c884bde20f1dcce9ef449744a46b1001d9a9c11205534dd452ece099e20b52",
						"index": 0,
						"vout": 0
					},
					{
						"txid": "95c884bde20f1dcce9ef449744a46b1001d9a9c11205534dd452ece099e20b52",
						"index": 1,
						"vout": 1
					}
				],
				"coinbase": false,
				"hash": "ac9203b46755c8580bca2a5d825e773079a8936194077884d247273003f9cc48",
				"time": 1548362179,
				"size": 538,
				"total": 111.46803222,
				"Type": "Ticket"
			}
		],
		"formatted_size": "3.8 kB",
		"voting_info": {
			"tickets_voted": 0,
			"max_votes_per_block": 5
		}
	}`),
}

var msgNewTxs5 = &pstypes.WebSocketMessage{
	EventId: "newtxs",
	Message: json.RawMessage(`[
		{
			"txid": "0c5bb28d9c33c5e73fd2d60a8dfad2b0a62c68af5be6dee0a9cc3734b897d22b",
			"fees": 0.000253,
			"vin_count": 1,
			"vout_count": 2,
			"vin": [
				{
					"txid": "be49deed3e666930174d2bfef304e07118ed0903bb27e0a51e3090ee6c546f41",
					"index": 0,
					"vout": 0
				}
			],
			"coinbase": false,
			"hash": "0c5bb28d9c33c5e73fd2d60a8dfad2b0a62c68af5be6dee0a9cc3734b897d22b",
			"time": 1548355201,
			"size": 251,
			"total": 199.88847161,
			"Type": "Regular"
		},
		{
			"txid": "1f03df275cc1625a7d16a8104d8e3583ed0156dcc4b83450a60b213019740e84",
			"fees": 0.000419,
			"vin_count": 2,
			"vout_count": 2,
			"vin": [
				{
					"txid": "55fdd419585afc2d20b86a9eab6ba1fecf8c5af07d0de624286effc7646f8cfa",
					"index": 0,
					"vout": 2
				},
				{
					"txid": "813a62f4d4f84cbb3680b98043a7d57d098528675de96fca9cea46963e67d8fb",
					"index": 1,
					"vout": 1
				}
			],
			"coinbase": false,
			"hash": "1f03df275cc1625a7d16a8104d8e3583ed0156dcc4b83450a60b213019740e84",
			"time": 1548355201,
			"size": 416,
			"total": 197.29827223,
			"Type": "Regular"
		},
		{
			"txid": "a8a106d4ace0b09b37cc81485bca9ec40b2d059907a7e9f07052c6e67a4af8d4",
			"fees": 0.0003,
			"vin_count": 1,
			"vout_count": 3,
			"vin": [
				{
					"txid": "1f03df275cc1625a7d16a8104d8e3583ed0156dcc4b83450a60b213019740e84",
					"index": 0,
					"vout": 0
				}
			],
			"coinbase": false,
			"hash": "a8a106d4ace0b09b37cc81485bca9ec40b2d059907a7e9f07052c6e67a4af8d4",
			"time": 1548355201,
			"size": 297,
			"total": 111.46803222,
			"Type": "Ticket"
		},
		{
			"txid": "0f69479950286986f52703c4c2595ecca564babe9efa4c79bece7b8705e83cb0",
			"fees": -4.8109873,
			"vin_count": 2,
			"vout_count": 1,
			"vin": [
				{
					"txid": "1ba6793e4e4e6e5c7a216375f0d66cbd2284f1233a39dafe853223ef9a5ea1bc",
					"index": 0,
					"vout": 0
				},
				{
					"txid": "00796ad37675a419c5bb9767f84942535f1e78143f12bea5a719da49c8f2b435",
					"index": 1,
					"vout": 0
				}
			],
			"coinbase": false,
			"hash": "0f69479950286986f52703c4c2595ecca564babe9efa4c79bece7b8705e83cb0",
			"time": 1548355220,
			"size": 381,
			"total": 4.81098728,
			"Type": "Regular"
		},
		{
			"txid": "039875f3771978559c5544ccec8cd7d50a52a1de78404124e193850e7861fd4b",
			"fees": -2.76749516,
			"vin_count": 2,
			"vout_count": 1,
			"vin": [
				{
					"txid": "87de5dbfcf68f41bc254e1a242b3340b2b70fb853db9444f529486f81589cf2e",
					"index": 0,
					"vout": 0
				},
				{
					"txid": "afc7050fcd6bcfa7bfdf294f8badba6e2c7679faa15c4b661f826bfe5c56ed03",
					"index": 1,
					"vout": 0
				}
			],
			"coinbase": false,
			"hash": "039875f3771978559c5544ccec8cd7d50a52a1de78404124e193850e7861fd4b",
			"time": 1548355246,
			"size": 381,
			"total": 2.76749514,
			"Type": "Regular"
		}
	]
	`),
}

var msgNewBlock312592 = &pstypes.WebSocketMessage{
	EventId: "newblock",
	Message: json.RawMessage(`{
		"block": {
			"height": 312592,
			"hash": "0000000000000000027e6f3738ab0aa01ffb9972b573bb445f4a62f28e02d92a",
			"version": 6,
			"size": 5560,
			"valid": true,
			"mainchain": true,
			"votes": 5,
			"tx": 5,
			"windowIndex": 2171,
			"tickets": 4,
			"revocations": 0,
			"tx_count": 14,
			"time": "2019-01-24T14:43:21Z",
			"formatted_bytes": "5.6 kB",
			"Confirmations": 1,
			"StakeRoot": "cab467f611badd5bd732c528b8a78989c24edc892d6d677a516a6d70f8427298",
			"MerkleRoot": "c0bf56d9c5e4a0bbf6e803e74e2c2c8a5fe9a8889aaf6fde9b1b95380864dad1",
			"TxAvailable": false,
			"Tx": [
				{
					"TxID": "f38e9be7dd5cda89245c79a4e0dcce2276af5b737def7bc2b674b805c6df5591",
					"FormattedSize": "581 B",
					"Total": 24517.6341217,
					"Fee": 5850,
					"FeeRate": 10068,
					"VoteInfo": null,
					"Coinbase": false,
					"Fees": 0.0000585,
					"VinCount": 3,
					"VoutCount": 2,
					"VoteValid": false
				},
				{
					"TxID": "4e7869920b1a8ea4899cd7f2eace7c9e4225a5c1e5d9cd031d5f607af33fd9ad",
					"FormattedSize": "453 B",
					"Total": 190.24534405,
					"Fee": 45500,
					"FeeRate": 100441,
					"VoteInfo": null,
					"Coinbase": false,
					"Fees": 0.000455,
					"VinCount": 2,
					"VoutCount": 3,
					"VoteValid": false
				},
				{
					"TxID": "329c9f65b63135a2bfc46f15120767d26e1fb7eeae1f5f5501e5239e3c4750ff",
					"FormattedSize": "417 B",
					"Total": 174.54406394,
					"Fee": 41900,
					"FeeRate": 100479,
					"VoteInfo": null,
					"Coinbase": false,
					"Fees": 0.000419,
					"VinCount": 2,
					"VoutCount": 2,
					"VoteValid": false
				},
				{
					"TxID": "78655819d62c2eae0c3bd5dc76e78e4c2ec7f398b901dba324eb211c2dd11d3c",
					"FormattedSize": "452 B",
					"Total": 164.37367617,
					"Fee": 45500,
					"FeeRate": 100663,
					"VoteInfo": null,
					"Coinbase": false,
					"Fees": 0.000455,
					"VinCount": 2,
					"VoutCount": 3,
					"VoteValid": false
				},
				{
					"TxID": "53dbbea8291f7ce4c4af72c9467b473acf91e648c20111d72d7dde49c63537ce",
					"FormattedSize": "176 B",
					"Total": 13.280629,
					"Fee": 0,
					"FeeRate": 0,
					"VoteInfo": null,
					"Coinbase": true,
					"Fees": 0,
					"VinCount": 1,
					"VoutCount": 3,
					"VoteValid": false
				}
			],
			"Tickets": [
				{
					"TxID": "790318718e79e16e94a7a2e860ae939fda40f198f2ff09acfc904cc20979982e",
					"FormattedSize": "538 B",
					"Total": 111.46803222,
					"Fee": 55000,
					"FeeRate": 102230,
					"VoteInfo": null,
					"Coinbase": false,
					"Fees": 0.00055,
					"VinCount": 2,
					"VoutCount": 5,
					"VoteValid": false
				},
				{
					"TxID": "85fc878571e6786922b3997dab8c67ca0d79f9ba2c0d210fa7137066ca0cc595",
					"FormattedSize": "296 B",
					"Total": 111.46803222,
					"Fee": 30000,
					"FeeRate": 101351,
					"VoteInfo": null,
					"Coinbase": false,
					"Fees": 0.0003,
					"VinCount": 1,
					"VoutCount": 3,
					"VoteValid": false
				},
				{
					"TxID": "91be1871e359575af3b4d740afe4c90eee2a6410c67dc99975d940190f864fbc",
					"FormattedSize": "296 B",
					"Total": 111.46803222,
					"Fee": 30000,
					"FeeRate": 101351,
					"VoteInfo": null,
					"Coinbase": false,
					"Fees": 0.0003,
					"VinCount": 1,
					"VoutCount": 3,
					"VoteValid": false
				},
				{
					"TxID": "d207a4c9c5bc375c386a60803de3b2d879d31cc5b8658ee8c6ddb5ed684facc4",
					"FormattedSize": "296 B",
					"Total": 111.46803222,
					"Fee": 30000,
					"FeeRate": 101351,
					"VoteInfo": null,
					"Coinbase": false,
					"Fees": 0.0003,
					"VinCount": 1,
					"VoutCount": 3,
					"VoteValid": false
				}
			],
			"Revs": [],
			"Votes": [
				{
					"TxID": "a77b706ee90dbc3c390fa587fcc8d250c92f82e3eb0f16bc8084f43a650e4608",
					"FormattedSize": "344 B",
					"Total": 111.42803761,
					"Fee": 0,
					"FeeRate": 0,
					"VoteInfo": {
						"block_validation": {
							"hash": "00000000000000002ae59390be4abc41ee2af999ff8cbfbd95b6c12f17782847",
							"height": 312591,
							"validity": true
						},
						"vote_version": 5,
						"vote_bits": 5,
						"vote_choices": [
							{
								"id": "lnfeatures",
								"description": "Enable features defined in DCP0002 and DCP0003 necessary to support Lightning Network (LN)",
								"mask": 6,
								"vote_version": 5,
								"vote_index": 0,
								"choice_index": 2,
								"choice": {
									"Id": "yes",
									"Description": "change to the new consensus rules",
									"Bits": 4,
									"IsAbstain": false,
									"IsNo": false
								}
							}
						],
						"ticket_spent": "",
						"mempool_ticket_index": 0,
						"last_block": false
					},
					"Coinbase": false,
					"Fees": 0,
					"VinCount": 2,
					"VoutCount": 3,
					"VoteValid": true
				},
				{
					"TxID": "bb9ca02caff15f8e625c7946ef6e898d0479310a7c8f2abb78b39583377e652e",
					"FormattedSize": "344 B",
					"Total": 111.23146262,
					"Fee": 0,
					"FeeRate": 0,
					"VoteInfo": {
						"block_validation": {
							"hash": "00000000000000002ae59390be4abc41ee2af999ff8cbfbd95b6c12f17782847",
							"height": 312591,
							"validity": true
						},
						"vote_version": 5,
						"vote_bits": 1,
						"vote_choices": [
							{
								"id": "lnfeatures",
								"description": "Enable features defined in DCP0002 and DCP0003 necessary to support Lightning Network (LN)",
								"mask": 6,
								"vote_version": 5,
								"vote_index": 0,
								"choice_index": 0,
								"choice": {
									"Id": "abstain",
									"Description": "abstain voting for change",
									"Bits": 0,
									"IsAbstain": true,
									"IsNo": false
								}
							}
						],
						"ticket_spent": "",
						"mempool_ticket_index": 0,
						"last_block": false
					},
					"Coinbase": false,
					"Fees": 0,
					"VinCount": 2,
					"VoutCount": 3,
					"VoteValid": true
				},
				{
					"TxID": "bea4bebe45040a634cdeeaef4dfa72b1de0f340ac7e7a67784af2684d90cc716",
					"FormattedSize": "420 B",
					"Total": 106.0232147,
					"Fee": 1,
					"FeeRate": 2,
					"VoteInfo": {
						"block_validation": {
							"hash": "00000000000000002ae59390be4abc41ee2af999ff8cbfbd95b6c12f17782847",
							"height": 312591,
							"validity": true
						},
						"vote_version": 5,
						"vote_bits": 5,
						"vote_choices": [
							{
								"id": "lnfeatures",
								"description": "Enable features defined in DCP0002 and DCP0003 necessary to support Lightning Network (LN)",
								"mask": 6,
								"vote_version": 5,
								"vote_index": 0,
								"choice_index": 2,
								"choice": {
									"Id": "yes",
									"Description": "change to the new consensus rules",
									"Bits": 4,
									"IsAbstain": false,
									"IsNo": false
								}
							}
						],
						"ticket_spent": "",
						"mempool_ticket_index": 0,
						"last_block": false
					},
					"Coinbase": false,
					"Fees": 1e-8,
					"VinCount": 2,
					"VoutCount": 4,
					"VoteValid": true
				},
				{
					"TxID": "44e24d9310a8d155320c6999f97058a00833992c08f9a11b0df1bd06f4098824",
					"FormattedSize": "420 B",
					"Total": 104.08096756,
					"Fee": 1,
					"FeeRate": 2,
					"VoteInfo": {
						"block_validation": {
							"hash": "00000000000000002ae59390be4abc41ee2af999ff8cbfbd95b6c12f17782847",
							"height": 312591,
							"validity": true
						},
						"vote_version": 5,
						"vote_bits": 5,
						"vote_choices": [
							{
								"id": "lnfeatures",
								"description": "Enable features defined in DCP0002 and DCP0003 necessary to support Lightning Network (LN)",
								"mask": 6,
								"vote_version": 5,
								"vote_index": 0,
								"choice_index": 2,
								"choice": {
									"Id": "yes",
									"Description": "change to the new consensus rules",
									"Bits": 4,
									"IsAbstain": false,
									"IsNo": false
								}
							}
						],
						"ticket_spent": "",
						"mempool_ticket_index": 0,
						"last_block": false
					},
					"Coinbase": false,
					"Fees": 1e-8,
					"VinCount": 2,
					"VoutCount": 4,
					"VoteValid": true
				},
				{
					"TxID": "20279fb01de39f4d0a6d58023a2dd33246b8e5ba578bf8cd10c8bc03c04309d3",
					"FormattedSize": "345 B",
					"Total": 102.28556991,
					"Fee": 0,
					"FeeRate": 0,
					"VoteInfo": {
						"block_validation": {
							"hash": "00000000000000002ae59390be4abc41ee2af999ff8cbfbd95b6c12f17782847",
							"height": 312591,
							"validity": true
						},
						"vote_version": 5,
						"vote_bits": 5,
						"vote_choices": [
							{
								"id": "lnfeatures",
								"description": "Enable features defined in DCP0002 and DCP0003 necessary to support Lightning Network (LN)",
								"mask": 6,
								"vote_version": 5,
								"vote_index": 0,
								"choice_index": 2,
								"choice": {
									"Id": "yes",
									"Description": "change to the new consensus rules",
									"Bits": 4,
									"IsAbstain": false,
									"IsNo": false
								}
							}
						],
						"ticket_spent": "",
						"mempool_ticket_index": 0,
						"last_block": false
					},
					"Coinbase": false,
					"Fees": 0,
					"VinCount": 2,
					"VoutCount": 3,
					"VoteValid": true
				}
			],
			"Misses": null,
			"Nonce": 128279579,
			"VoteBits": 1,
			"FinalState": "902195ad0569",
			"PoolSize": 41018,
			"Bits": "183f53ab",
			"SBits": 111.46803222,
			"Difficulty": 17362228383.034344,
			"ExtraData": "001472af8c2c3400000000000000000000000000000000000000000000000000",
			"StakeVersion": 5,
			"PreviousHash": "00000000000000002ae59390be4abc41ee2af999ff8cbfbd95b6c12f17782847",
			"NextHash": "",
			"TotalSent": 26040.99921614,
			"MiningFee": 0.00283752,
			"StakeValidationHeight": 4096,
			"Subsidy": {
				"developer": 189682735,
				"pos": 569048205,
				"pow": 1138096413,
				"total": 1896827353
			}
		},
		"extra": {
			"coin_supply": 920450852764806,
			"sdiff": 111.46803222,
			"next_expected_sdiff": 111.34409787,
			"next_expected_min": 110.57557454,
			"next_expected_max": 113.85751329,
			"window_idx": 113,
			"reward_idx": 5392,
			"difficulty": 17362228383.034344,
			"dev_fund": 0,
			"dev_address": "Dcur2mcGjmENx4DhNqDctW5wJCVyT3Qeqkx",
			"reward": 1.0210069984493713,
			"reward_period": "29.07 days",
			"ASR": 0,
			"subsidy": {
				"total": 1896827353,
				"pow": 1138096413,
				"pos": 569048205,
				"dev": 189682735
			},
			"params": {
				"window_size": 144,
				"reward_window_size": 6144,
				"target_pool_size": 0,
				"target_block_time": 300000000000,
				"MeanVotingBlocks": 7860
			},
			"pool_info": {
				"size": 41014,
				"value": 4344994.29801412,
				"valavg": 105.9392962894163,
				"percent": 47.2050657019094,
				"target": 40960,
				"percent_target": 100.1318359375
			},
			"total_locked_dcr": 0,
			"hash_rate": 248.56734363605156,
			"hash_rate_change_day": 7.2007116787749466,
			"hash_rate_change_month": 134.0
		}
	}
	`),
}

var block312592Tickets = []string{
	"790318718e79e16e94a7a2e860ae939fda40f198f2ff09acfc904cc20979982e",
	"85fc878571e6786922b3997dab8c67ca0d79f9ba2c0d210fa7137066ca0cc595",
	"91be1871e359575af3b4d740afe4c90eee2a6410c67dc99975d940190f864fbc",
	"d207a4c9c5bc375c386a60803de3b2d879d31cc5b8658ee8c6ddb5ed684facc4",
}

func TestDecodeMsgNil(t *testing.T) {
	Msg, err := DecodeMsg(nil)
	if err == nil {
		t.Fatalf("expected error for DecodeMsg(nil)")
	}
	if Msg != nil {
		t.Errorf("expected a nil return message, got: %v", Msg)
	}
}

func TestDecodeMsgUnknown(t *testing.T) {
	msg := &pstypes.WebSocketMessage{
		EventId: "fakeevent",
		Message: json.RawMessage(`"pointless message"`),
	}
	expectedErr := errors.New("unrecognized event type")

	Msg, err := DecodeMsg(msg)
	if err.Error() != expectedErr.Error() {
		t.Fatalf("incorrect error returned: got %v, expected %v", err, expectedErr)
	}
	if Msg != nil {
		t.Errorf("expected a nil return message, got: %v", Msg)
	}
}

func TestDecodeMsg(t *testing.T) {
	Msg, err := DecodeMsg(msgNewTxs5)
	if err != nil {
		t.Fatalf("failed to decode message: %v", err)
	}

	txlist, ok := Msg.(*pstypes.TxList)
	if !ok {
		t.Errorf("Msg was not a *TxList")
	}

	// Spot check the number of transactions.
	if len(*txlist) != 5 {
		t.Errorf("expecting 5 txns, got %d", len(*txlist))
	}

	for _, tx := range *txlist {
		t.Log(*tx)
	}
}

func TestDecodeMsgTxList(t *testing.T) {
	txlist, err := DecodeMsgTxList(msgNewTxs5)
	if err != nil {
		t.Fatalf("failed to decode message: %v", err)
	}

	// Spot check the number of transactions.
	if len(*txlist) != 5 {
		t.Errorf("expecting 5 txns, got %d", len(*txlist))
	}

	for _, tx := range *txlist {
		t.Log(*tx)
	}
}

func TestDecodeMsgMempool(t *testing.T) {
	mpShort, err := DecodeMsgMempool(msgMempool5Latest)
	if err != nil {
		t.Fatalf("failed to decode message: %v", err)
	}

	// Spot check the number of latest transactions.
	if len(mpShort.LatestTransactions) != 5 {
		t.Errorf("expecting 5 txns, got %d", len(mpShort.LatestTransactions))
	}

	for _, tx := range mpShort.LatestTransactions {
		t.Log(tx)
	}
}

func TestDecodeMsgNewBlock(t *testing.T) {
	newBlock, err := DecodeMsgNewBlock(msgNewBlock312592)
	if err != nil {
		t.Fatalf("failed to decode message: %v", err)
	}

	if len(newBlock.Block.Tickets) != 4 {
		t.Errorf("expecting 4 tickets, got %d", len(newBlock.Block.Tickets))
	}

	// Spot check the ticket hashes.
	for i, tx := range newBlock.Block.Tickets {
		if block312592Tickets[i] != tx.TxID {
			t.Errorf("incorrect ticket hash: expected %s, got %s",
				block312592Tickets[i], tx.TxID)
		}
	}
}

func TestDecodeMsgPing(t *testing.T) {
	expectedInt := 2
	MessageJSON, _ := json.Marshal(expectedInt)
	msgPing := &pstypes.WebSocketMessage{
		EventId: "ping",
		Message: MessageJSON,
	}

	gotInt, err := DecodeMsgPing(msgPing)
	if err != nil {
		t.Fatalf("Failed to decode int: %v", err)
	}
	if gotInt != expectedInt {
		t.Errorf("Wrong string decoded: got %d, expected %d",
			gotInt, expectedInt)
	}
}

func TestDecodeMsgString(t *testing.T) {
	expectedStr := "meow"
	MessageJSON, _ := json.Marshal(expectedStr)
	msgPing := &pstypes.WebSocketMessage{
		EventId: "message",
		Message: MessageJSON,
	}

	str, err := DecodeMsgString(msgPing)
	if err != nil {
		t.Fatalf("Failed to decode string: %v", err)
	}
	if str != expectedStr {
		t.Errorf("Wrong string decoded: got %s, expected %s",
			str, expectedStr)
	}
}

func Test_newSubscribeMsg(t *testing.T) {
	subEvent := "cheese"
	expectedMsg := `{"event":"subscribe","message":{"request_id":123,"message":"cheese"}}`

	b := newSubscribeMsg(subEvent, 123)
	if string(b) != expectedMsg {
		t.Errorf("Wrong message. Got \"%s\", expected \"%s\".",
			b, expectedMsg)
	}
}

func Test_newUnsubscribeMsg(t *testing.T) {
	unsubEvent := "banana"
	expectedMsg := `{"event":"unsubscribe","message":{"request_id":456,"message":"banana"}}`

	b := newUnsubscribeMsg(unsubEvent, 456)
	if string(b) != expectedMsg {
		t.Errorf("Wrong message. Got \"%s\", expected \"%s\".",
			b, expectedMsg)
	}
}
