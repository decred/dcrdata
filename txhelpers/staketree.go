package txhelpers

import (
	"os"
	"path/filepath"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/database"
	_ "github.com/decred/dcrd/database/ffldb"
	//"github.com/decred/dcrd/wire"
	"fmt"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

const (
	// dbType is the database backend type to use for the tests.
	dbType = "ffldb"

	// dbRoot is the root directory used to create all test databases.
	dbRoot = "dbs"

	// dbName is the database name.
	dbName = "ffldb_stake"
)

//var netParams = &chaincfg.MainNetParams

func BuildStakeTree(blocks map[int64]*dcrutil.Block,
	netParams *chaincfg.Params, nodeClient *dcrrpcclient.Client) (database.DB, []int64, error) {

	height := int64(len(blocks) - 1)

	// Create a new database to store the accepted stake node data into.
	dbName := "ffldb_staketest"
	dbPath := filepath.Join(dbRoot, dbName)
	_ = os.RemoveAll(dbPath)
	db, err := database.Create(dbType, dbPath, netParams.Net)
	if err != nil {
		return db, nil, fmt.Errorf("error creating db: %v\n", err)
	}

	// Setup a teardown.
	//defer os.RemoveAll(dbPath)
	//defer os.RemoveAll(dbRoot)
	//defer db.Close()

	// Load the genesis block and begin testing exported functions.
	var bestNode *stake.Node
	err = db.Update(func(dbTx database.Tx) error {
		var errLocal error
		bestNode, errLocal = stake.InitDatabaseState(dbTx, netParams)
		return errLocal
	})
	if err != nil {
		db.Close()
		return nil, nil, err
	}

	// Cache all of our nodes so that we can check them when we start
	// disconnecting and going backwards through the blocks.
	poolValues := make([]int64, height+1)
	nodes := make([]*stake.Node, height+1)
	nodes[0] = bestNode
	err = db.Update(func(dbTx database.Tx) error {
		for i := int64(1); i <= height; i++ {
			if i%200 == 0 {
				fmt.Printf("%d\n", i)
			}
			block := blocks[i]
			ticketsToAdd := make([]chainhash.Hash, 0)
			if i >= netParams.StakeEnabledHeight {
				matureHeight := (i - int64(netParams.TicketMaturity))
				ticketsToAdd = ticketsInBlock(blocks[matureHeight])
			}
			header := block.MsgBlock().Header
			numLive := len(bestNode.LiveTickets())
			if int(header.PoolSize) != numLive {
				fmt.Printf("bad number of live tickets: want %v, got %v (%v)\n",
					header.PoolSize, numLive, numLive-int(header.PoolSize))
			}
			if header.FinalState != bestNode.FinalState() {
				fmt.Printf("bad final state: want %x, got %x\n",
					header.FinalState, bestNode.FinalState())
			}

			// In memory addition teslog.
			bestNode, err = bestNode.ConnectNode(header,
				ticketsSpentInBlock(block), revokedTicketsInBlock(block),
				ticketsToAdd)
			if err != nil {
				return fmt.Errorf("couldn't connect node: %v\n", err.Error())
			}

			// Write the new node to db.
			nodes[i] = bestNode
			blockHash := block.Hash()
			err = stake.WriteConnectedBestNode(dbTx, bestNode, *blockHash)
			if err != nil {
				return fmt.Errorf("failure writing the best node: %v\n",
					err.Error())
			}

			// var amt int64
			// for _, hash := range bestNode.LiveTickets() {
			// 	txid, err := nodeClient.GetRawTransactionVerbose(&hash)
			// 	if err != nil {
			// 		fmt.Printf("Unable to get transaction %v: %v\n", hash, err)
			// 		continue
			// 	}

			// 	// This isn't right for pool tickets because the pennies
			// 	// included for pool fees are in vout[0]
			// 	coins := txid.Vout[0].Value
			// 	atoms, err := dcrutil.NewAmount(coins)
			// 	if err != nil {
			// 		fmt.Printf("Invalid Vout amount %v: %v\n", coins, err)
			// 		continue
			// 	}
			// 	amt += int64(atoms) // utxo.sparseOutputs[0].amount
			// }
			// poolValues[i] = amt

			// Reload the node from DB and make sure it's the same.
			// blockHash = block.Hash()
			// loadedNode, err := stake.LoadBestNode(dbTx, bestNode.Height(),
			// 	*blockHash, header, netParams)
			// if err != nil {
			// 	return log.Errorf("failed to load the best node: %v",
			// 		err.Error())
			// }
			// err = nodesEqual(loadedNode, bestNode)
			// if err != nil {
			// 	return log.Errorf("loaded best node was not same as "+
			// 		"in memory best node: %v", err.Error())
			// }
			// loadedNodesForward[i] = loadedNode
		}

		return nil
	})

	return db, poolValues, err
}

/// kang

// ticketsInBlock finds all the new tickets in the block.
func ticketsInBlock(bl *dcrutil.Block) []chainhash.Hash {
	tickets := make([]chainhash.Hash, 0)
	for _, stx := range bl.STransactions() {
		if stake.DetermineTxType(stx.MsgTx()) == stake.TxTypeSStx {
			h := stx.Hash()
			tickets = append(tickets, *h)
		}
	}

	return tickets
}

// ticketsSpentInBlock finds all the tickets spent in the block.
func ticketsSpentInBlock(bl *dcrutil.Block) []chainhash.Hash {
	tickets := make([]chainhash.Hash, 0)
	for _, stx := range bl.STransactions() {
		if stake.DetermineTxType(stx.MsgTx()) == stake.TxTypeSSGen {
			tickets = append(tickets, stx.MsgTx().TxIn[1].PreviousOutPoint.Hash)
		}
	}

	return tickets
}

// votesInBlock finds all the votes in the block.
func votesInBlock(bl *dcrutil.Block) []chainhash.Hash {
	votes := make([]chainhash.Hash, 0)
	for _, stx := range bl.STransactions() {
		if stake.DetermineTxType(stx.MsgTx()) == stake.TxTypeSSGen {
			h := stx.Hash()
			votes = append(votes, *h)
		}
	}

	return votes
}

// revokedTicketsInBlock finds all the revoked tickets in the block.
func revokedTicketsInBlock(bl *dcrutil.Block) []chainhash.Hash {
	tickets := make([]chainhash.Hash, 0)
	for _, stx := range bl.STransactions() {
		if stake.DetermineTxType(stx.MsgTx()) == stake.TxTypeSSRtx {
			tickets = append(tickets, stx.MsgTx().TxIn[0].PreviousOutPoint.Hash)
		}
	}

	return tickets
}
