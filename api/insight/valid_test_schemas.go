// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package insight

// Valid response Schema for test case [10] without any mempool transactions
// http://127.0.0.1:7777/insight-api/addr/Dca7Vsv42RAJC6cEdw7dyhLER8QJCHiUYNL/utxo?noCache=1
// Valid response Schema for test case [11] without any mempool transactions
// http://127.0.0.1:7777/insight-api/addrs/Dca7Vsv42RAJC6cEdw7dyhLER8QJCHiUYNL,Dcgh6jmtEbgGjUUMBaNrRRUfn2Jp5rKL5Aj/utxo
const ValidUtxoSchema = `{
  "$id": "http://example.com/example.json",
  "type": "array",
  "definitions": {},
  "$schema": "http://json-schema.org/draft-07/schema#",
  "items": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "address": {
        "type": "string"
      },
      "txid": {
        "type": "string"
      },
      "vout": {
        "type": "integer"
      },
      "scriptPubKey": {
        "type": "string"
      },
      "height": {
        "type": "integer"
      },
      "amount": {
        "type": "number"
      },
      "satoshis": {
        "type": "integer"
      },
      "confirmations": {
        "type": "integer"
      }
    },
    "required": [
      "address",
      "txid",
      "vout",
      "scriptPubKey",
      "height",
      "amount",
      "satoshis",
      "confirmations"
    ]
  }
}`

// Valid response Schema for test case [6] Tx
// http://127.0.0.1:7777/insight-api/tx/2766fdf592de6c27c6049af24d8e03f64efd72e0f20aa362a78d97d4643c7cb0

const ValidTxSchema = `{
	"$id": "http://example.com/example.json",
	"type": "object",
	"definitions": {},
	"$schema": "http://json-schema.org/draft-07/schema#",
	"properties": {
	  "txid": {
		"$id": "/properties/txid",
		"type": "string"
	  },
	  "version": {
		"$id": "/properties/version",
		"type": "integer"
	  },
	  "locktime": {
		"$id": "/properties/locktime",
		"type": "integer"
	  },
	  "blockhash": {
		"$id": "/properties/blockhash",
		"type": "string"
	  },
	  "blockheight": {
		"$id": "/properties/blockheight",
		"type": "integer"
	  },
	  "confirmations": {
		"$id": "/properties/confirmations",
		"type": "integer"
	  },
	  "time": {
		"$id": "/properties/time",
		"type": "integer"
	  },
	  "blocktime": {
		"$id": "/properties/blocktime",
		"type": "integer"
	  },
	  "valueOut": {
		"$id": "/properties/valueOut",
		"type": "number"
	  },
	  "size": {
		"$id": "/properties/size",
		"type": "integer"
	  },
	  "valueIn": {
		"$id": "/properties/valueIn",
		"type": "number"
	  },
	  "fees": {
		"$id": "/properties/fees",
		"type": "number"
	  },
	  "vin": {
		"$id": "/properties/vin",
		"type": "array",
		"items": {
		  "$id": "/properties/vin/items",
		  "type": "object",
		  "properties": {
			"txid": {
			  "$id": "/properties/vin/items/properties/txid",
			  "type": "string"
			},
			"vout": {
			  "$id": "/properties/vin/items/properties/vout",
			  "type": "integer"
			},
			"sequence": {
			  "$id": "/properties/vin/items/properties/sequence",
			  "type": "integer"
			},
			"scriptSig": {
			  "$id": "/properties/vin/items/properties/scriptSig",
			  "type": "object",
			  "properties": {
				"asm": {
				  "$id": "/properties/vin/items/properties/scriptSig/properties/asm",
				  "type": "string"
				},
				"hex": {
				  "$id": "/properties/vin/items/properties/scriptSig/properties/hex",
				  "type": "string"
				}
			  },
			  "required": [
				"asm",
				"hex"
			  ]
			},
			"n": {
			  "$id": "/properties/vin/items/properties/n",
			  "type": "integer"
			},
			"addr": {
			  "$id": "/properties/vin/items/properties/addr",
			  "type": "string"
			},
			"valueSat": {
			  "$id": "/properties/vin/items/properties/valueSat",
			  "type": "integer"
			},
			"value": {
			  "$id": "/properties/vin/items/properties/value",
			  "type": "number"
			},
		  },
		  "required": [
			"txid",
			"vout",
			"sequence",
			"scriptSig",
			"n",
			"addr",
			"valueSat",
			"value",
		  ]
		}
	  },
	  "vout": {
		"$id": "/properties/vout",
		"type": "array",
		"items": {
		  "$id": "/properties/vout/items",
		  "type": "object",
		  "properties": {
			"value": {
			  "$id": "/properties/vout/items/properties/value",
			  "type": "number"
			},
			"n": {
			  "$id": "/properties/vout/items/properties/n",
			  "type": "integer"
			},
			"scriptPubKey": {
			  "$id": "/properties/vout/items/properties/scriptPubKey",
			  "type": "object",
			  "properties": {
				"asm": {
				  "$id": "/properties/vout/items/properties/scriptPubKey/properties/asm",
				  "type": "string"
				},
				"hex": {
				  "$id": "/properties/vout/items/properties/scriptPubKey/properties/hex",
				  "type": "string"
				},
				"type": {
				  "$id": "/properties/vout/items/properties/scriptPubKey/properties/type",
				  "type": "string"
				},
				"addresses": {
				  "$id": "/properties/vout/items/properties/scriptPubKey/properties/addresses",
				  "type": "array",
				  "items": {
					"$id": "/properties/vout/items/properties/scriptPubKey/properties/addresses/items",
					"type": "string"
				  }
				}
			  },
			  "required": [
				"asm",
				"hex",
				"type",
				"addresses"
			  ]
			},
			"spentTxId": {
			  "$id": "/properties/vout/items/properties/spentTxId",
			  "type": "string"
			},
			"spentIndex": {
			  "$id": "/properties/vout/items/properties/spentIndex",
			  "type": "integer"
			},
			 "spentHeight": {
			  "$id": "/properties/vout/items/properties/spentHeight",
			  "type": "integer"
			},
		  },
		  "required": [
			"value",
			"n",
			"scriptPubKey",
			"spentTxId",
			"spentIndex",
			"spentHeight"
		  ]
		}
	  },
	  
	},
	"required": [
	  "txid",
	  "version",
	  "locktime",
	  "vin",
	  "vout",
	  "blockhash",
	  "blockheight",
	  "confirmations",
	  "time",
	  "blocktime",
	  "valueOut",
	  "size",
	  "valueIn",
	  "fees"
	]
  };
  `
