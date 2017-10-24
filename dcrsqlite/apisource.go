// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package dcrsqlite

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
	"github.com/dcrdata/dcrdata/explorer"
	"github.com/dcrdata/dcrdata/mempool"
	"github.com/dcrdata/dcrdata/rpcutils"
	"github.com/dcrdata/dcrdata/stakedb"
	"github.com/dcrdata/dcrdata/txhelpers"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/dustin/go-humanize"
)

// wiredDB is intended to satisfy APIDataSource interface. The block header is
// not stored in the DB, so the RPC client is used to get it on demand.
type wiredDB struct {
	*DBDataSaver
	MPC    *mempool.MempoolDataCache
	client *rpcclient.Client
	params *chaincfg.Params
	sDB    *stakedb.StakeDatabase
}

func newWiredDB(DB *DB, statusC chan uint32, cl *rpcclient.Client, p *chaincfg.Params) (wiredDB, func() error) {
	wDB := wiredDB{
		DBDataSaver: &DBDataSaver{DB, statusC},
		MPC:         new(mempool.MempoolDataCache),
		client:      cl,
		params:      p,
	}

	//err := wDB.openStakeDB()
	var err error
	wDB.sDB, err = stakedb.NewStakeDatabase(cl, p)
	if err != nil {
		log.Errorf("Unable to create stake DB: %v", err)
		return wDB, func() error { return nil }
	}
	return wDB, wDB.sDB.StakeDB.Close
}

// NewWiredDB creates a new wiredDB from a *sql.DB, a node client, network
// parameters, and a status update channel. It calls dcrsqlite.NewDB to create a
// new DB that wrapps the sql.DB.
func NewWiredDB(db *sql.DB, statusC chan uint32, cl *rpcclient.Client, p *chaincfg.Params) (wiredDB, func() error) {
	return newWiredDB(NewDB(db), statusC, cl, p)
}

// InitWiredDB creates a new wiredDB from a file containing the data for a
// sql.DB. The other parameters are same as those for NewWiredDB.
func InitWiredDB(dbInfo *DBInfo, statusC chan uint32, cl *rpcclient.Client, p *chaincfg.Params) (wiredDB, func() error, error) {
	db, err := InitDB(dbInfo)
	if err != nil {
		return wiredDB{}, func() error { return nil }, err
	}

	wDB, cleanup := newWiredDB(db, statusC, cl, p)
	return wDB, cleanup, nil
}

func (db *wiredDB) NewStakeDBChainMonitor(quit chan struct{}, wg *sync.WaitGroup,
	blockChan chan *chainhash.Hash, reorgChan chan *stakedb.ReorgData) *stakedb.ChainMonitor {
	return db.sDB.NewChainMonitor(quit, wg, blockChan, reorgChan)
}

func (db *wiredDB) SyncDB(wg *sync.WaitGroup, quit chan struct{}) error {
	defer wg.Done()
	var err error
	if err = db.Ping(); err != nil {
		return err
	}
	if err = db.client.Ping(); err != nil {
		return err
	}

	return db.resyncDB(quit)
}

func (db *wiredDB) SyncDBWithPoolValue(wg *sync.WaitGroup, quit chan struct{}) error {
	defer wg.Done()
	var err error
	if err = db.Ping(); err != nil {
		return err
	}
	if err = db.client.Ping(); err != nil {
		return err
	}

	return db.resyncDBWithPoolValue(quit)
}

func (db *wiredDB) GetStakeDB() *stakedb.StakeDatabase {
	return db.sDB
}

func (db *wiredDB) GetHeight() int {
	return int(db.GetBestBlockHeight())
}

func (db *wiredDB) GetBestBlockHash() (string, error) {
	hash := db.DBDataSaver.GetBestBlockHash()
	var err error
	if len(hash) == 0 {
		err = fmt.Errorf("unable to get best block hash")
	}
	return hash, err
}

func (db *wiredDB) GetBlockHash(idx int64) (string, error) {
	hash, err := db.RetrieveBlockHash(idx)
	if err != nil {
		log.Errorf("Unable to get block hash for block number %d: %v", idx, err)
		return "", err
	}
	return hash, nil
}

func (db *wiredDB) GetBlockHeight(hash string) (int64, error) {
	height, err := db.RetrieveBlockHeight(hash)
	if err != nil {
		log.Errorf("Unable to get block height for hash %s: %v", hash, err)
		return -1, err
	}
	return height, nil
}

func (db *wiredDB) GetHeader(idx int) *dcrjson.GetBlockHeaderVerboseResult {
	return rpcutils.GetBlockHeaderVerbose(db.client, db.params, int64(idx))
}

func (db *wiredDB) GetBlockVerbose(idx int, verboseTx bool) *dcrjson.GetBlockVerboseResult {
	return rpcutils.GetBlockVerbose(db.client, db.params, int64(idx), verboseTx)
}

func (db *wiredDB) GetBlockVerboseByHash(hash string, verboseTx bool) *dcrjson.GetBlockVerboseResult {
	return rpcutils.GetBlockVerboseByHash(db.client, db.params, hash, verboseTx)
}

func (db *wiredDB) GetCoinSupply() dcrutil.Amount {
	coinSupply, err := db.client.GetCoinSupply()
	if err != nil {
		return dcrutil.Amount(-1)
	}
	return coinSupply
}

func (db *wiredDB) GetBlockSubsidy(height int64, voters uint16) *dcrjson.GetBlockSubsidyResult {
	blockSubsidy, err := db.client.GetBlockSubsidy(height, voters)
	if err != nil {
		return nil
	}
	return blockSubsidy
}

func (db *wiredDB) GetTransactionsForBlock(idx int64) *apitypes.BlockTransactions {
	blockVerbose := rpcutils.GetBlockVerbose(db.client, db.params, idx, false)

	return makeBlockTransactions(blockVerbose)
}

func (db *wiredDB) GetTransactionsForBlockByHash(hash string) *apitypes.BlockTransactions {
	blockVerbose := rpcutils.GetBlockVerboseByHash(db.client, db.params, hash, false)

	return makeBlockTransactions(blockVerbose)
}

func makeBlockTransactions(blockVerbose *dcrjson.GetBlockVerboseResult) *apitypes.BlockTransactions {
	blockTransactions := new(apitypes.BlockTransactions)

	blockTransactions.Tx = make([]string, len(blockVerbose.Tx))
	copy(blockTransactions.Tx, blockVerbose.Tx)

	blockTransactions.STx = make([]string, len(blockVerbose.STx))
	copy(blockTransactions.STx, blockVerbose.STx)

	return blockTransactions
}

func (db *wiredDB) GetAllTxIn(txid string) []*apitypes.TxIn {

	txhash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		log.Errorf("Invalid transaction hash %s", txid)
		return nil
	}

	tx, err := db.client.GetRawTransaction(txhash)
	if err != nil {
		log.Errorf("Unknown transaction %s", txid)
		return nil
	}

	allTxIn0 := tx.MsgTx().TxIn
	allTxIn := make([]*apitypes.TxIn, len(allTxIn0))
	for i := range allTxIn {
		txIn := &apitypes.TxIn{
			PreviousOutPoint: apitypes.OutPoint{
				Hash:  allTxIn0[i].PreviousOutPoint.Hash.String(),
				Index: allTxIn0[i].PreviousOutPoint.Index,
				Tree:  allTxIn0[i].PreviousOutPoint.Tree,
			},
			Sequence:        allTxIn0[i].Sequence,
			ValueIn:         dcrutil.Amount(allTxIn0[i].ValueIn).ToCoin(),
			BlockHeight:     allTxIn0[i].BlockHeight,
			BlockIndex:      allTxIn0[i].BlockIndex,
			SignatureScript: hex.EncodeToString(allTxIn0[i].SignatureScript),
		}
		allTxIn[i] = txIn
	}

	return allTxIn
}

func (db *wiredDB) GetAllTxOut(txid string) []*apitypes.TxOut {

	txhash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		log.Infof("Invalid transaction hash %s", txid)
		return nil
	}

	tx, err := db.client.GetRawTransaction(txhash)
	if err != nil {
		log.Warnf("Unknown transaction %s", txid)
		return nil
	}

	allTxOut0 := tx.MsgTx().TxOut
	allTxOut := make([]*apitypes.TxOut, len(allTxOut0))
	for i := range allTxOut {
		var addresses []string
		_, txAddrs, _, err := txscript.ExtractPkScriptAddrs(
			allTxOut0[i].Version, allTxOut0[i].PkScript, db.params)
		if err != nil {
			log.Warnf("Unable to extract addresses from PkScript: %v", err)
		} else {
			addresses = make([]string, 0, len(txAddrs))
			for i := range txAddrs {
				addresses = append(addresses, txAddrs[i].String())
			}
		}

		txOut := &apitypes.TxOut{
			Value:     dcrutil.Amount(allTxOut0[i].Value).ToCoin(),
			Version:   allTxOut0[i].Version,
			PkScript:  hex.EncodeToString(allTxOut0[i].PkScript),
			Addresses: addresses,
		}

		allTxOut[i] = txOut
	}

	return allTxOut
}

// GetRawTransactionWithPrevOutAddresses looks up the previous outpoints for a
// transaction and extracts a slice of addresses encoded by the pkScript for
// each previous outpoint consumed by the transaction.
func (db *wiredDB) GetRawTransactionWithPrevOutAddresses(txid string) (*apitypes.Tx, [][]string) {
	tx, _ := db.getRawTransaction(txid)
	if tx == nil {
		return nil, nil
	}

	prevOutAddresses := make([][]string, len(tx.Vin))

	for i := range tx.Vin {
		vin := &tx.Vin[i]
		// Skip inspecting previous outpoint for coinbase transaction, and
		// vin[0] of stakebase transcation.
		if vin.IsCoinBase() || (vin.IsStakeBase() && i == 0) {
			continue
		}
		var err error
		prevOutAddresses[i], err = txhelpers.OutPointAddressesFromString(
			vin.Txid, vin.Vout, vin.Tree, db.client, db.params)
		if err != nil {
			log.Warnf("failed to get outpoint address from txid: %v", err)
		}
	}

	return tx, prevOutAddresses
}

func (db *wiredDB) GetRawTransaction(txid string) *apitypes.Tx {
	tx, _ := db.getRawTransaction(txid)
	return tx
}

func (db *wiredDB) getRawTransaction(txid string) (*apitypes.Tx, string) {
	tx := new(apitypes.Tx)

	txhash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		log.Errorf("Invalid transaction hash %s", txid)
		return nil, ""
	}

	txraw, err := db.client.GetRawTransactionVerbose(txhash)
	if err != nil {
		log.Errorf("GetRawTransactionVerbose failed for: %v", txhash)
		return nil, ""
	}

	// TxShort
	tx.Size = int32(len(txraw.Hex) / 2)
	tx.TxID = txraw.Txid
	tx.Version = txraw.Version
	tx.Locktime = txraw.LockTime
	tx.Expiry = txraw.Expiry
	tx.Vin = make([]dcrjson.Vin, len(txraw.Vin))
	copy(tx.Vin, txraw.Vin)
	tx.Vout = make([]apitypes.Vout, len(txraw.Vout))
	for i := range txraw.Vout {
		tx.Vout[i].Value = txraw.Vout[i].Value
		tx.Vout[i].N = txraw.Vout[i].N
		tx.Vout[i].Version = txraw.Vout[i].Version
		spk := &tx.Vout[i].ScriptPubKeyDecoded
		spkRaw := &txraw.Vout[i].ScriptPubKey
		spk.Asm = spkRaw.Asm
		spk.ReqSigs = spkRaw.ReqSigs
		spk.Type = spkRaw.Type
		spk.Addresses = make([]string, len(spkRaw.Addresses))
		for j := range spkRaw.Addresses {
			spk.Addresses[j] = spkRaw.Addresses[j]
		}
		if spkRaw.CommitAmt != nil {
			spk.CommitAmt = new(float64)
			*spk.CommitAmt = *spkRaw.CommitAmt
		}
	}

	tx.Confirmations = txraw.Confirmations

	// BlockID
	tx.Block = new(apitypes.BlockID)
	tx.Block.BlockHash = txraw.BlockHash
	tx.Block.BlockHeight = txraw.BlockHeight
	tx.Block.BlockIndex = txraw.BlockIndex
	tx.Block.Time = txraw.Time
	tx.Block.BlockTime = txraw.Blocktime

	return tx, txraw.Hex
}

// GetVoteVersionInfo requests stake version info from the dcrd RPC server
func (db *wiredDB) GetVoteVersionInfo(ver uint32) (*dcrjson.GetVoteInfoResult, error) {
	return db.client.GetVoteInfo(ver)
}

// GetStakeVersions requests the output of the getstakeversions RPC, which gets
// stake version information and individual vote version information starting at the
// given block and for count-1 blocks prior.
func (db *wiredDB) GetStakeVersions(txHash string, count int32) (*dcrjson.GetStakeVersionsResult, error) {
	return db.client.GetStakeVersions(txHash, count)
}

// GetStakeVersionsLatest requests the output of the getstakeversions RPC for
// just the current best block.
func (db *wiredDB) GetStakeVersionsLatest() (*dcrjson.StakeVersions, error) {
	txHash, err := db.GetBestBlockHash()
	if err != nil {
		return nil, err
	}
	stkVers, err := db.GetStakeVersions(txHash, 1)
	if err != nil || stkVers == nil || len(stkVers.StakeVersions) == 0 {
		return nil, err
	}
	stkVer := stkVers.StakeVersions[0]
	return &stkVer, nil
}

// GetVoteInfo attempts to decode the vote bits of a SSGen transaction. If the
// transaction is not a valid SSGen, the VoteInfo output will be nil. Depending
// on the stake version with which dcrdata is compiled with (chaincfg.Params),
// the Choices field of VoteInfo may be a nil slice even if the votebits were
// set for a previously-valid agenda.
func (db *wiredDB) GetVoteInfo(txid string) (*apitypes.VoteInfo, error) {
	txhash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		log.Errorf("Invalid transaction hash %s", txid)
		return nil, nil
	}

	tx, err := db.client.GetRawTransaction(txhash)
	if err != nil {
		log.Errorf("GetRawTransaction failed for: %v", txhash)
		return nil, nil
	}

	validation, version, bits, choices, err := txhelpers.SSGenVoteChoices(tx.MsgTx(), db.params)
	if err != nil {
		return nil, err
	}
	vinfo := &apitypes.VoteInfo{
		Validation: apitypes.BlockValidation{
			Hash:     validation.Hash.String(),
			Height:   validation.Height,
			Validity: validation.Validity,
		},
		Version: version,
		Bits:    bits,
		Choices: choices,
	}
	return vinfo, nil
}

func (db *wiredDB) GetStakeDiffEstimates() *apitypes.StakeDiff {
	sd := rpcutils.GetStakeDiffEstimates(db.client)

	height := db.MPC.GetHeight()
	winSize := uint32(db.params.StakeDiffWindowSize)
	sd.IdxBlockInWindow = int(height%winSize) + 1
	sd.PriceWindowNum = int(height / winSize)

	return sd
}

func (db *wiredDB) GetFeeInfo(idx int) *dcrjson.FeeInfoBlock {
	stakeInfo, err := db.RetrieveStakeInfoExtended(int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve stake info: %v", err)
		return nil
	}

	return &stakeInfo.Feeinfo
}

func (db *wiredDB) GetStakeInfoExtended(idx int) *apitypes.StakeInfoExtended {
	stakeInfo, err := db.RetrieveStakeInfoExtended(int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve stake info: %v", err)
		return nil
	}

	return stakeInfo
}

func (db *wiredDB) GetSummary(idx int) *apitypes.BlockDataBasic {
	blockSummary, err := db.RetrieveBlockSummary(int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve block summary: %v", err)
		return nil
	}

	return blockSummary
}

func (db *wiredDB) GetSummaryByHash(hash string) *apitypes.BlockDataBasic {
	blockSummary, err := db.RetrieveBlockSummaryByHash(hash)
	if err != nil {
		log.Errorf("Unable to retrieve block summary: %v", err)
		return nil
	}

	return blockSummary
}

func (db *wiredDB) GetBestBlockSummary() *apitypes.BlockDataBasic {
	dbBlkHeight := db.GetBlockSummaryHeight()
	blockSummary, err := db.RetrieveBlockSummary(dbBlkHeight)
	if err != nil {
		log.Errorf("Unable to retrieve block %d summary: %v", dbBlkHeight, err)
		return nil
	}

	return blockSummary
}

func (db *wiredDB) GetBlockSize(idx int) (int32, error) {
	blockSizes, err := db.RetrieveBlockSizeRange(int64(idx), int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve block %d size: %v", idx, err)
		return -1, err
	}
	if len(blockSizes) == 0 {
		log.Errorf("Unable to retrieve block %d size: %v", idx, err)
		return -1, fmt.Errorf("empty block size slice")
	}
	return blockSizes[0], nil
}

func (db *wiredDB) GetBlockSizeRange(idx0, idx1 int) ([]int32, error) {
	blockSizes, err := db.RetrieveBlockSizeRange(int64(idx0), int64(idx1))
	if err != nil {
		log.Errorf("Unable to retrieve block size range: %v", err)
		return nil, err
	}
	return blockSizes, nil
}

func (db *wiredDB) GetPoolInfo(idx int) *apitypes.TicketPoolInfo {
	ticketPoolInfo, err := db.RetrievePoolInfo(int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve ticket pool info: %v", err)
		return nil
	}
	return ticketPoolInfo
}

func (db *wiredDB) GetPoolInfoByHash(hash string) *apitypes.TicketPoolInfo {
	ticketPoolInfo, err := db.RetrievePoolInfoByHash(hash)
	if err != nil {
		log.Errorf("Unable to retrieve ticket pool info: %v", err)
		return nil
	}
	return ticketPoolInfo
}

func (db *wiredDB) GetPoolInfoRange(idx0, idx1 int) []apitypes.TicketPoolInfo {
	ticketPoolInfos, err := db.RetrievePoolInfoRange(int64(idx0), int64(idx1))
	if err != nil {
		log.Errorf("Unable to retrieve ticket pool info range: %v", err)
		return nil
	}
	return ticketPoolInfos
}

func (db *wiredDB) GetPoolValAndSizeRange(idx0, idx1 int) ([]float64, []float64) {
	poolvals, poolsizes, err := db.RetrievePoolValAndSizeRange(int64(idx0), int64(idx1))
	if err != nil {
		log.Errorf("Unable to retrieve ticket value and size range: %v", err)
		return nil, nil
	}
	return poolvals, poolsizes
}

func (db *wiredDB) GetSDiff(idx int) float64 {
	sdiff, err := db.RetrieveSDiff(int64(idx))
	if err != nil {
		log.Errorf("Unable to retrieve stake difficulty: %v", err)
		return -1
	}
	return sdiff
}

func (db *wiredDB) GetSDiffRange(idx0, idx1 int) []float64 {
	sdiffs, err := db.RetrieveSDiffRange(int64(idx0), int64(idx1))
	if err != nil {
		log.Errorf("Unable to retrieve stake difficulty range: %v", err)
		return nil
	}
	return sdiffs
}

func (db *wiredDB) GetMempoolSSTxSummary() *apitypes.MempoolTicketFeeInfo {
	_, feeInfo := db.MPC.GetFeeInfoExtra()
	return feeInfo
}

func (db *wiredDB) GetMempoolSSTxFeeRates(N int) *apitypes.MempoolTicketFees {
	height, timestamp, totalFees, fees := db.MPC.GetFeeRates(N)
	mpTicketFees := apitypes.MempoolTicketFees{
		Height:   height,
		Time:     timestamp,
		Length:   uint32(len(fees)),
		Total:    uint32(totalFees),
		FeeRates: fees,
	}
	return &mpTicketFees
}

func (db *wiredDB) GetMempoolSSTxDetails(N int) *apitypes.MempoolTicketDetails {
	height, timestamp, totalSSTx, details := db.MPC.GetTicketsDetails(N)
	mpTicketDetails := apitypes.MempoolTicketDetails{
		Height:  height,
		Time:    timestamp,
		Length:  uint32(len(details)),
		Total:   uint32(totalSSTx),
		Tickets: []*apitypes.TicketDetails(details),
	}
	return &mpTicketDetails
}

// GetAddressTransactions returns an apitypes.Address Object with at most the
// last count transactions the address was in
func (db *wiredDB) GetAddressTransactions(addr string, count int) *apitypes.Address {
	address, err := dcrutil.DecodeAddress(addr)
	if err != nil {
		log.Infof("Invalid address %s: %v", addr, err)
		return nil
	}
	txs, err := db.client.SearchRawTransactionsVerbose(address, 0, count, false, true, nil)
	if err != nil {
		log.Warnf("GetAddressTransactions failed for address %s: %v", addr, err)
		return nil
	}
	tx := make([]*apitypes.AddressTxShort, 0, len(txs))
	for i := range txs {
		tx = append(tx, &apitypes.AddressTxShort{
			TxID:          txs[i].Txid,
			Time:          txs[i].Time,
			Value:         txhelpers.TotalVout(txs[i].Vout).ToCoin(),
			Confirmations: int64(txs[i].Confirmations),
			Size:          int32(len(txs[i].Hex) / 2),
		})
	}
	return &apitypes.Address{
		Address:      addr,
		Transactions: tx,
	}

}

// GetAddressTransactions returns an array of apitypes.AddressTxRaw objects
// representing the raw result of SearchRawTransactionsverbose
func (db *wiredDB) GetAddressTransactionsRaw(addr string, count int) []*apitypes.AddressTxRaw {
	address, err := dcrutil.DecodeAddress(addr)
	if err != nil {
		log.Infof("Invalid address %s: %v", addr, err)
		return nil
	}
	txs, err := db.client.SearchRawTransactionsVerbose(address, 0, count, true, true, nil)
	if err != nil {
		log.Warnf("GetAddressTransactionsRaw failed for address %s: %v", addr, err)
		return nil
	}
	txarray := make([]*apitypes.AddressTxRaw, 0, len(txs))
	for i := range txs {
		tx := new(apitypes.AddressTxRaw)
		tx.Size = int32(len(txs[i].Hex) / 2)
		tx.TxID = txs[i].Txid
		tx.Version = txs[i].Version
		tx.Locktime = txs[i].LockTime
		tx.Vin = make([]dcrjson.VinPrevOut, len(txs[i].Vin))
		copy(tx.Vin, txs[i].Vin)
		tx.Confirmations = int64(txs[i].Confirmations)
		tx.BlockHash = txs[i].BlockHash
		tx.Blocktime = txs[i].Blocktime
		tx.Time = txs[i].Time
		tx.Vout = make([]apitypes.Vout, len(txs[i].Vout))
		for j := range txs[i].Vout {
			tx.Vout[j].Value = txs[i].Vout[j].Value
			tx.Vout[j].N = txs[i].Vout[j].N
			tx.Vout[j].Version = txs[i].Vout[j].Version
			spk := &tx.Vout[j].ScriptPubKeyDecoded
			spkRaw := &txs[i].Vout[j].ScriptPubKey
			spk.Asm = spkRaw.Asm
			spk.ReqSigs = spkRaw.ReqSigs
			spk.Type = spkRaw.Type
			spk.Addresses = make([]string, len(spkRaw.Addresses))
			for k := range spkRaw.Addresses {
				spk.Addresses[k] = spkRaw.Addresses[k]
			}
			if spkRaw.CommitAmt != nil {
				spk.CommitAmt = new(float64)
				*spk.CommitAmt = *spkRaw.CommitAmt
			}
		}
		txarray = append(txarray, tx)
	}

	return txarray
}

func makeExplorerBlockBasic(data *dcrjson.GetBlockVerboseResult) *explorer.BlockBasic {
	block := &explorer.BlockBasic{
		Height:         data.Height,
		Size:           data.Size,
		Voters:         data.Voters,
		Transactions:   len(data.RawTx),
		FreshStake:     data.FreshStake,
		BlockTime:      data.Time,
		FormattedBytes: humanize.Bytes(uint64(data.Size)),
		FormattedTime:  time.Unix(data.Time, 0).Format("1/_2/06 15:04:05"),
	}

	// Count the number of revocations
	for i := range data.RawSTx {
		msgTx := txhelpers.MsgTxFromHex(data.RawSTx[i].Hex)
		if msgTx == nil {
			log.Errorf("Unknown transaction %s", data.RawSTx[i].Txid)
			continue
		}
		if isRev, _ := stake.IsSSRtx(msgTx); isRev {
			block.Revocations++
		}
	}
	return block
}

func makeExplorerTxBasic(data dcrjson.TxRawResult, msgTx *wire.MsgTx, params *chaincfg.Params) *explorer.TxBasic {
	tx := new(explorer.TxBasic)
	tx.TxID = data.Txid
	tx.FormattedSize = humanize.Bytes(uint64(len(data.Hex) / 2))
	tx.Total = txhelpers.TotalVout(data.Vout).ToCoin()
	tx.Fee, tx.FeeRate = txhelpers.TxFeeRate(msgTx)
	if ok, _ := stake.IsSSGen(msgTx); ok {
		validation, version, bits, choices, err := txhelpers.SSGenVoteChoices(msgTx, params)
		if err != nil {
			log.Debugf("Cannot get vote choices for %s", tx.TxID)
			return tx
		}
		tx.VoteInfo = &explorer.VoteInfo{
			Validation: explorer.BlockValidation{
				Hash:     validation.Hash.String(),
				Height:   validation.Height,
				Validity: validation.Validity,
			},
			Version: version,
			Bits:    bits,
			Choices: choices,
		}
	}
	return tx
}

func makeExplorerAddressTx(data *dcrjson.SearchRawTransactionsResult, address string) *explorer.AddressTx {
	tx := new(explorer.AddressTx)
	tx.TxID = data.Txid
	tx.FormattedSize = humanize.Bytes(uint64(len(data.Hex) / 2))
	tx.Total = txhelpers.TotalVout(data.Vout).ToCoin()
	tx.Time = data.Time
	t := time.Unix(tx.Time, 0)
	tx.FormattedTime = t.Format("1/_2/06 15:04:05")
	tx.Confirmations = data.Confirmations

	for i := range data.Vin {
		if len(data.Vin[i].PrevOut.Addresses) > 0 {
			if data.Vin[i].PrevOut.Addresses[0] == address {
				tx.SentTotal += *data.Vin[i].AmountIn
			}
		}
	}
	for i := range data.Vout {
		if len(data.Vout[i].ScriptPubKey.Addresses) != 0 {
			if data.Vout[i].ScriptPubKey.Addresses[0] == address {
				tx.RecievedTotal += data.Vout[i].Value
			}
		}
	}
	return tx
}

func (db *wiredDB) GetExplorerBlocks(start int, end int) []*explorer.BlockBasic {
	if start < end {
		return nil
	}
	summaries := make([]*explorer.BlockBasic, 0, start-end)
	for i := start; i > end; i-- {
		data := db.GetBlockVerbose(i, true)
		block := new(explorer.BlockBasic)
		if data != nil {
			block = makeExplorerBlockBasic(data)
		}
		summaries = append(summaries, block)
	}
	return summaries
}

func (db *wiredDB) GetExplorerBlock(hash string) *explorer.BlockInfo {
	data := db.GetBlockVerboseByHash(hash, true)
	if data == nil {
		log.Error("Unable to get block for block hash " + hash)
		return nil
	}

	// Explorer Block Info
	block := &explorer.BlockInfo{
		BlockBasic:    makeExplorerBlockBasic(data),
		Hash:          data.Hash,
		Version:       data.Version,
		Confirmations: data.Confirmations,
		StakeRoot:     data.StakeRoot,
		MerkleRoot:    data.MerkleRoot,
		Nonce:         data.Nonce,
		VoteBits:      data.VoteBits,
		FinalState:    data.FinalState,
		PoolSize:      data.PoolSize,
		Bits:          data.Bits,
		SBits:         data.SBits,
		Difficulty:    data.Difficulty,
		ExtraData:     data.ExtraData,
		StakeVersion:  data.StakeVersion,
		PreviousHash:  data.PreviousHash,
		NextHash:      data.NextHash,
	}

	votes := make([]*explorer.TxBasic, 0, block.Voters)
	revocations := make([]*explorer.TxBasic, 0, block.Revocations)
	tickets := make([]*explorer.TxBasic, 0, block.FreshStake)

	for _, tx := range data.RawSTx {
		msgTx := txhelpers.MsgTxFromHex(tx.Hex)
		if msgTx == nil {
			log.Errorf("Unknown transaction %s", tx.Txid)
			return nil
		}
		switch stake.DetermineTxType(msgTx) {
		case stake.TxTypeSSGen:
			stx := makeExplorerTxBasic(tx, msgTx, db.params)
			stx.Fee, stx.FeeRate = 0.0, 0.0
			votes = append(votes, stx)
		case stake.TxTypeSStx:
			stx := makeExplorerTxBasic(tx, msgTx, db.params)
			tickets = append(tickets, stx)
		case stake.TxTypeSSRtx:
			stx := makeExplorerTxBasic(tx, msgTx, db.params)
			revocations = append(revocations, stx)
		}
	}

	txs := make([]*explorer.TxBasic, 0, block.Transactions)
	for _, tx := range data.RawTx {
		exptx := makeExplorerTxBasic(tx, txhelpers.MsgTxFromHex(tx.Hex), db.params)
		for _, vin := range tx.Vin {
			if vin.IsCoinBase() {
				exptx.Fee, exptx.FeeRate = 0.0, 0.0
			}
		}
		txs = append(txs, exptx)
	}
	block.Tx = txs
	block.Votes = votes
	block.Revs = revocations
	block.Tickets = tickets

	sortTx := func(txs []*explorer.TxBasic) {
		sort.Slice(txs, func(i, j int) bool {
			return txs[i].Total > txs[j].Total
		})
	}

	sortTx(block.Tx)
	sortTx(block.Votes)
	sortTx(block.Revs)
	sortTx(block.Tickets)

	getTotalFee := func(txs []*explorer.TxBasic) (total dcrutil.Amount) {
		for _, tx := range txs {
			total += tx.Fee
		}
		return
	}
	getTotalSent := func(txs []*explorer.TxBasic) (total dcrutil.Amount) {
		for _, tx := range txs {
			amt, err := dcrutil.NewAmount(tx.Total)
			if err != nil {
				continue
			}
			total += amt
		}
		return
	}
	block.TotalSent = (getTotalSent(block.Tx) + getTotalSent(block.Revs) +
		getTotalSent(block.Tickets) + getTotalSent(block.Votes)).ToCoin()
	block.MiningFee = getTotalFee(block.Tx) + getTotalFee(block.Revs) +
		getTotalFee(block.Tickets)

	return block
}

func (db *wiredDB) GetExplorerTx(txid string) *explorer.TxInfo {
	txhash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		log.Errorf("Invalid transaction hash %s", txid)
		return nil
	}
	txraw, err := db.client.GetRawTransactionVerbose(txhash)
	if err != nil {
		log.Errorf("GetRawTransactionVerbose failed for: %v", txhash)
		return nil
	}
	msgTx := txhelpers.MsgTxFromHex(txraw.Hex)
	if msgTx == nil {
		log.Errorf("Cannot create MsgTx for tx %v", txhash)
		return nil
	}
	txBasic := makeExplorerTxBasic(*txraw, msgTx, db.params)
	tx := &explorer.TxInfo{
		TxBasic: txBasic,
	}
	tx.Type = txhelpers.DetermineTxTypeString(msgTx)
	tx.BlockHeight = txraw.BlockHeight
	tx.BlockIndex = txraw.BlockIndex
	tx.Confirmations = txraw.Confirmations
	tx.Time = txraw.Time
	t := time.Unix(tx.Time, 0)
	tx.FormattedTime = t.Format("1/_2/06 15:04:05")

	inputs := make([]explorer.Vin, 0, len(txraw.Vin))
	for i, vin := range txraw.Vin {
		var addresses []string
		if !(vin.IsCoinBase() || (vin.IsStakeBase() && i == 0)) {
			addrs, err := txhelpers.OutPointAddresses(&msgTx.TxIn[i].PreviousOutPoint, db.client, db.params)
			if err != nil {
				log.Warnf("Failed to get outpoint address from txid: %v", err)
				continue
			}
			addresses = addrs
		}
		inputs = append(inputs, explorer.Vin{
			Vin: &dcrjson.Vin{
				Txid:        vin.Txid,
				Coinbase:    vin.Coinbase,
				Stakebase:   vin.Stakebase,
				Vout:        vin.Vout,
				AmountIn:    vin.AmountIn,
				BlockHeight: vin.BlockHeight,
			},
			Addresses:       addresses,
			FormattedAmount: humanize.Commaf(vin.AmountIn),
		})
	}
	tx.Vin = inputs
	if tx.Type == "Vote" || tx.Type == "Ticket" || tx.Vin[0].IsCoinBase() {
		if db.GetBestBlockHeight() >= (int64(db.params.TicketMaturity) + tx.BlockHeight) {
			tx.Mature = "True"
		} else {
			tx.Mature = "False"
		}
	}
	if tx.Type == "Vote" {
		if tx.Confirmations < int64(db.params.CoinbaseMaturity) {
			tx.VoteFundsLocked = "True"
		} else {
			tx.VoteFundsLocked = "False"
		}
	}
	outputs := make([]explorer.Vout, 0, len(txraw.Vout))
	for i, vout := range txraw.Vout {
		txout, err := db.client.GetTxOut(txhash, uint32(i), true)
		if err != nil {
			log.Warnf("Failed to determine if tx out is spent for ouput %d of tx %s", i, txid)
		}
		var opReturn string
		if strings.Contains(vout.ScriptPubKey.Asm, "OP_RETURN") {
			opReturn = vout.ScriptPubKey.Asm
		}
		outputs = append(outputs, explorer.Vout{
			Addresses:       vout.ScriptPubKey.Addresses,
			Amount:          vout.Value,
			FormattedAmount: humanize.Commaf(vout.Value),
			OP_RETURN:       opReturn,
			Type:            vout.ScriptPubKey.Type,
			Spent:           txout == nil,
		})
	}
	tx.Vout = outputs
	return tx
}

func (db *wiredDB) GetExplorerAddress(address string, count int) *explorer.AddressInfo {
	addr, err := dcrutil.DecodeAddress(address)
	if err != nil {
		log.Infof("Invalid address %s: %v", address, err)
		return nil
	}

	txs, err := db.client.SearchRawTransactionsVerbose(addr, 0, count, true, true, nil)
	if err != nil {
		log.Warnf("GetAddressTransactionsRaw failed for address %s: %v", addr, err)
		return nil
	}

	addressTxs := make([]*explorer.AddressTx, 0, len(txs))
	for _, tx := range txs {
		addressTxs = append(addressTxs, makeExplorerAddressTx(tx, address))
	}

	NumberOfTx := len(txs)
	NoOfUnconfirmed := 0
	var totalreceived, totalsent dcrutil.Amount
	if NumberOfTx < explorer.AddressRows {
		for _, tx := range txs {
			if tx.Confirmations == 0 {
				NoOfUnconfirmed++
			}
			for _, y := range tx.Vout {
				if len(y.ScriptPubKey.Addresses) != 0 {
					if address == y.ScriptPubKey.Addresses[0] {
						t, _ := dcrutil.NewAmount(y.Value)
						totalreceived += t
					}
				}
			}
			for _, u := range tx.Vin {
				if u.PrevOut != nil && len(u.PrevOut.Addresses) != 0 {
					if address == u.PrevOut.Addresses[0] {
						t, _ := dcrutil.NewAmount(*u.AmountIn)
						totalsent += t
					}
				}
			}
		}
	}
	unspentfunds := totalreceived - totalsent
	return &explorer.AddressInfo{
		Address:          address,
		Transactions:     addressTxs,
		NumTransactions:  NumberOfTx,
		TotalUnconfirmed: NoOfUnconfirmed,
		Received:         totalreceived,
		AddressRow:       explorer.AddressRows,
		TotalSent:        totalsent,
		UnSpent:          unspentfunds,
	}
}
