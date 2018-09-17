// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"database/sql"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrdata/v3/db/agendadb"
	"github.com/decred/dcrdata/v3/db/dbtypes"
	"github.com/decred/dcrdata/v3/txhelpers"
	humanize "github.com/dustin/go-humanize"
)

const (
	defaultErrorCode    = "Something went wrong..."
	defaultErrorMessage = "Try refreshing this page... it usually fixes things"
	fullModeRequired    = "full-functionality mode required for this page"
)

// netName returns the name used when referring to a decred network.
func netName(chainParams *chaincfg.Params) string {
	if strings.HasPrefix(strings.ToLower(chainParams.Name), "testnet") {
		return "Testnet"
	}
	return strings.Title(chainParams.Name)
}

// Home is the page handler for the "/" path
func (exp *explorerUI) Home(w http.ResponseWriter, r *http.Request) {
	height := exp.blockData.GetHeight()

	blocks := exp.blockData.GetExplorerBlocks(height, height-5)

	// Lock for both MempoolData and ExtraInfo
	exp.MempoolData.RLock()
	exp.NewBlockDataMtx.RLock()

	str, err := exp.templates.execTemplateToString("home", struct {
		Info    *HomeInfo
		Mempool *MempoolInfo
		Blocks  []*BlockBasic
		Version string
		NetName string
	}{
		exp.ExtraInfo,
		exp.MempoolData,
		blocks,
		exp.Version,
		exp.NetName,
	})

	exp.MempoolData.RUnlock()
	exp.NewBlockDataMtx.RUnlock()

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// SideChains is the page handler for the "/side" path
func (exp *explorerUI) SideChains(w http.ResponseWriter, r *http.Request) {
	sideBlocks, err := exp.explorerSource.SideChainBlocks()
	if err != nil {
		log.Errorf("Unable to get side chain blocks: %v", err)
		exp.StatusPage(w, defaultErrorCode, "failed to retrieve side chain blocks", ErrorStatusType)
		return
	}

	str, err := exp.templates.execTemplateToString("sidechains", struct {
		Data    []*dbtypes.BlockStatus
		Version string
		NetName string
	}{
		sideBlocks,
		exp.Version,
		exp.NetName,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// NextHome is the page handler for the "/nexthome" path
func (exp *explorerUI) NextHome(w http.ResponseWriter, r *http.Request) {
	height := exp.blockData.GetHeight()

	blocks := exp.blockData.GetExplorerFullBlocks(height, height-11)

	exp.NewBlockDataMtx.RLock()
	exp.MempoolData.RLock()

	str, err := exp.templates.execTemplateToString("nexthome", struct {
		Info    *HomeInfo
		Mempool *MempoolInfo
		Blocks  []*BlockInfo
		Version string
		NetName string
	}{
		exp.ExtraInfo,
		exp.MempoolData,
		blocks,
		exp.Version,
		exp.NetName,
	})
	exp.NewBlockDataMtx.RUnlock()
	exp.MempoolData.RUnlock()

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Blocks is the page handler for the "/blocks" path
func (exp *explorerUI) Blocks(w http.ResponseWriter, r *http.Request) {
	idx := exp.blockData.GetHeight()

	height, err := strconv.Atoi(r.URL.Query().Get("height"))
	if err != nil || height > idx {
		height = idx
	}

	rows, err := strconv.Atoi(r.URL.Query().Get("rows"))

	if err != nil || rows > maxExplorerRows || rows < minExplorerRows {
		rows = minExplorerRows
	}

	oldestBlock := height - rows + 1
	if oldestBlock < 0 {
		height = rows - 1
	}

	summaries := exp.blockData.GetExplorerBlocks(height, height-rows)
	if summaries == nil {
		log.Errorf("Unable to get blocks: height=%d&rows=%d", height, rows)
		exp.StatusPage(w, defaultErrorCode, "could not find those blocks", NotFoundStatusType)
		return
	}

	if !exp.liteMode {
		for _, s := range summaries {
			blockStatus, err := exp.explorerSource.BlockStatus(s.Hash)
			if err != nil && err != sql.ErrNoRows {
				log.Warnf("Unable to retrieve chain status for block %s: %v", s.Hash, err)
			}
			s.Valid = blockStatus.IsValid
			s.MainChain = blockStatus.IsMainchain
		}
	}

	str, err := exp.templates.execTemplateToString("explorer", struct {
		Data      []*BlockBasic
		BestBlock int
		Rows      int
		Version   string
		NetName   string
	}{
		summaries,
		idx,
		rows,
		exp.Version,
		exp.NetName,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Block is the page handler for the "/block" path
func (exp *explorerUI) Block(w http.ResponseWriter, r *http.Request) {
	hash := getBlockHashCtx(r)

	data := exp.blockData.GetExplorerBlock(hash)
	if data == nil {
		log.Errorf("Unable to get block %s", hash)
		exp.StatusPage(w, defaultErrorCode, "could not find that block", NotFoundStatusType)
		return
	}
	// Checking if there exists any regular non-Coinbase transactions in the block.
	var count int
	data.TxAvailable = true
	for _, i := range data.Tx {
		if i.Coinbase {
			count++
		}
	}
	if count == len(data.Tx) {
		data.TxAvailable = false
	}

	if !exp.liteMode {
		var err error
		data.Misses, err = exp.explorerSource.BlockMissedVotes(hash)
		if err != nil && err != sql.ErrNoRows {
			log.Warnf("Unable to retrieve missed votes for block %s: %v", hash, err)
		}

		var blockStatus dbtypes.BlockStatus
		blockStatus, err = exp.explorerSource.BlockStatus(hash)
		if err != nil && err != sql.ErrNoRows {
			log.Warnf("Unable to retrieve chain status for block %s: %v", hash, err)
		}
		data.Valid = blockStatus.IsValid
		data.MainChain = blockStatus.IsMainchain
	}

	pageData := struct {
		Data          *BlockInfo
		ConfirmHeight int64
		Version       string
		NetName       string
	}{
		data,
		exp.Height() - data.Confirmations,
		exp.Version,
		exp.NetName,
	}
	str, err := exp.templates.execTemplateToString("block", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Turbolinks-Location", r.URL.RequestURI())
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Mempool is the page handler for the "/mempool" path
func (exp *explorerUI) Mempool(w http.ResponseWriter, r *http.Request) {
	exp.MempoolData.RLock()
	str, err := exp.templates.execTemplateToString("mempool", struct {
		Mempool *MempoolInfo
		Version string
		NetName string
	}{
		exp.MempoolData,
		exp.Version,
		exp.NetName,
	})
	exp.MempoolData.RUnlock()

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Ticketpool is the page handler for the "/ticketpool" path
func (exp *explorerUI) Ticketpool(w http.ResponseWriter, r *http.Request) {
	if exp.liteMode {
		exp.StatusPage(w, fullModeRequired,
			"Ticketpool page cannot run in lite mode", NotSupportedStatusType)
		return
	}
	interval := dbtypes.AllChartGrouping

	barGraphs, donutChart, height, err := exp.explorerSource.TicketPoolVisualization(interval)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}

	var mp = dbtypes.PoolTicketsData{}
	exp.MempoolData.RLock()
	var mpData = exp.MempoolData

	if len(mpData.Tickets) > 0 {
		mp.Time = append(mp.Time, uint64(mpData.Tickets[0].Time))
		mp.Price = append(mp.Price, mpData.Tickets[0].TotalOut)
		mp.Mempool = append(mp.Mempool, uint64(len(mpData.Tickets)))
	} else {
		log.Debug("No tickets exist in the mempool")
	}
	exp.MempoolData.RUnlock()

	str, err := exp.templates.execTemplateToString("ticketpool", struct {
		Version      string
		NetName      string
		ChartsHeight uint64
		ChartData    []*dbtypes.PoolTicketsData
		GroupedData  *dbtypes.PoolTicketsData
		Mempool      *dbtypes.PoolTicketsData
	}{
		exp.Version,
		exp.NetName,
		height,
		barGraphs,
		donutChart,
		&mp,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// TxPage is the page handler for the "/tx" path
func (exp *explorerUI) TxPage(w http.ResponseWriter, r *http.Request) {
	// attempt to get tx hash string from URL path
	hash, ok := r.Context().Value(ctxTxHash).(string)
	if !ok {
		log.Trace("txid not set")
		exp.StatusPage(w, defaultErrorCode, "there was no transaction requested", NotFoundStatusType)
		return
	}
	tx := exp.blockData.GetExplorerTx(hash)
	if tx == nil {
		log.Errorf("Unable to get transaction %s", hash)
		exp.StatusPage(w, defaultErrorCode, "could not find that transaction", NotFoundStatusType)
		return
	}

	// Set ticket-related parameters for both full and lite mode
	if tx.IsTicket() {
		blocksLive := tx.Confirmations - int64(exp.ChainParams.TicketMaturity)
		tx.TicketInfo.TicketPoolSize = int64(exp.ChainParams.TicketPoolSize) *
			int64(exp.ChainParams.TicketsPerBlock)
		tx.TicketInfo.TicketExpiry = int64(exp.ChainParams.TicketExpiry)
		expirationInDays := (exp.ChainParams.TargetTimePerBlock.Hours() *
			float64(exp.ChainParams.TicketExpiry)) / 24
		maturityInHours := (exp.ChainParams.TargetTimePerBlock.Hours() *
			float64(tx.TicketInfo.TicketMaturity))
		tx.TicketInfo.TimeTillMaturity = ((float64(exp.ChainParams.TicketMaturity) -
			float64(tx.Confirmations)) / float64(exp.ChainParams.TicketMaturity)) * maturityInHours
		ticketExpiryBlocksLeft := int64(exp.ChainParams.TicketExpiry) - blocksLive
		tx.TicketInfo.TicketExpiryDaysLeft = (float64(ticketExpiryBlocksLeft) /
			float64(exp.ChainParams.TicketExpiry)) * expirationInDays
	}

	var blocks []*dbtypes.BlockStatus
	var blockInds []uint32
	if exp.liteMode {
		blocks = append(blocks, &dbtypes.BlockStatus{
			Hash:        tx.BlockHash,
			Height:      uint32(tx.BlockHeight),
			IsMainchain: true,
			IsValid:     true,
		})
		blockInds = []uint32{tx.BlockIndex}
	} else {
		// For any coinbase transactions look up the total block fees to include
		// as part of the inputs.
		if tx.Type == "Coinbase" {
			data := exp.blockData.GetExplorerBlock(tx.BlockHash)
			if data == nil {
				log.Errorf("Unable to get block %s", tx.BlockHash)
			} else {
				tx.BlockMiningFee = int64(data.MiningFee)
			}
		}

		// Details on all the blocks containing this transaction
		var err error
		blocks, blockInds, err = exp.explorerSource.TransactionBlocks(tx.TxID)
		if err != nil {
			log.Errorf("Unable to retrieve blocks for transaction %s: %v", hash, err)
			exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
			return
		}

		// For each output of this transaction, look up any spending transactions,
		// and the index of the spending transaction input.
		spendingTxHashes, spendingTxVinInds, voutInds, err := exp.explorerSource.SpendingTransactions(hash)
		if err != nil {
			log.Errorf("Unable to retrieve spending transactions for %s: %v", hash, err)
			exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
			return
		}
		for i, vout := range voutInds {
			if int(vout) >= len(tx.SpendingTxns) {
				log.Errorf("Invalid spending transaction data (%s:%d)", hash, vout)
				continue
			}
			tx.SpendingTxns[vout] = TxInID{
				Hash:  spendingTxHashes[i],
				Index: spendingTxVinInds[i],
			}
		}
		if tx.IsTicket() {
			spendStatus, poolStatus, err := exp.explorerSource.PoolStatusForTicket(hash)
			if err != nil {
				log.Errorf("Unable to retrieve ticket spend and pool status for %s: %v", hash, err)
			} else {
				if tx.Mature == "False" {
					tx.TicketInfo.PoolStatus = "immature"
				} else {
					tx.TicketInfo.PoolStatus = poolStatus.String()
				}
				tx.TicketInfo.SpendStatus = spendStatus.String()

				// Ticket luck and probability of voting
				blocksLive := tx.Confirmations - int64(exp.ChainParams.TicketMaturity)
				if tx.TicketInfo.SpendStatus == "Voted" {
					// Blocks from eligible until voted (actual luck)
					tx.TicketInfo.TicketLiveBlocks = exp.blockData.TxHeight(tx.SpendingTxns[0].Hash) -
						tx.BlockHeight - int64(exp.ChainParams.TicketMaturity) - 1
				} else if tx.Confirmations >= int64(exp.ChainParams.TicketExpiry+
					uint32(exp.ChainParams.TicketMaturity)) { // Expired
					// Blocks ticket was active before expiring (actual no luck)
					tx.TicketInfo.TicketLiveBlocks = int64(exp.ChainParams.TicketExpiry)
				} else { // Active
					// Blocks ticket has been active and eligible to vote
					tx.TicketInfo.TicketLiveBlocks = blocksLive
				}
				tx.TicketInfo.BestLuck = tx.TicketInfo.TicketExpiry / int64(exp.ChainParams.TicketPoolSize)
				tx.TicketInfo.AvgLuck = tx.TicketInfo.BestLuck - 1
				if tx.TicketInfo.TicketLiveBlocks == int64(exp.ChainParams.TicketExpiry) {
					tx.TicketInfo.VoteLuck = 0
				} else {
					tx.TicketInfo.VoteLuck = float64(tx.TicketInfo.BestLuck) -
						(float64(tx.TicketInfo.TicketLiveBlocks) / float64(exp.ChainParams.TicketPoolSize))
				}
				if tx.TicketInfo.VoteLuck >= float64(tx.TicketInfo.BestLuck-(1/int64(exp.ChainParams.TicketPoolSize))) {
					tx.TicketInfo.LuckStatus = "Perfection"
				} else if tx.TicketInfo.VoteLuck > (float64(tx.TicketInfo.BestLuck) - 0.25) {
					tx.TicketInfo.LuckStatus = "Very Lucky!"
				} else if tx.TicketInfo.VoteLuck > (float64(tx.TicketInfo.BestLuck) - 0.75) {
					tx.TicketInfo.LuckStatus = "Good Luck"
				} else if tx.TicketInfo.VoteLuck > (float64(tx.TicketInfo.BestLuck) - 1.25) {
					tx.TicketInfo.LuckStatus = "Normal"
				} else if tx.TicketInfo.VoteLuck > (float64(tx.TicketInfo.BestLuck) * 0.50) {
					tx.TicketInfo.LuckStatus = "Bad Luck"
				} else if tx.TicketInfo.VoteLuck > 0 {
					tx.TicketInfo.LuckStatus = "Horrible Luck!"
				} else if tx.TicketInfo.VoteLuck == 0 {
					tx.TicketInfo.LuckStatus = "No Luck"
				}

				// Chance for a ticket to NOT be voted in a given time frame:
				// C = (1 - P)^N
				// Where: P is the probability of a vote in one block. (votes
				// per block / current ticket pool size)
				// N is the number of blocks before ticket expiry. (ticket
				// expiry in blocks - (number of blocks since ticket purchase -
				// ticket maturity))
				// C is the probability (chance)
				exp.NewBlockDataMtx.RLock()
				pVote := float64(exp.ChainParams.TicketsPerBlock) / float64(exp.ExtraInfo.PoolInfo.Size)
				exp.NewBlockDataMtx.RUnlock()
				tx.TicketInfo.Probability = 100 * (math.Pow(1-pVote,
					float64(exp.ChainParams.TicketExpiry)-float64(blocksLive)))
			}
		} // tx.IsTicket()
	} // !exp.liteMode

	pageData := struct {
		Data          *TxInfo
		Blocks        []*dbtypes.BlockStatus
		BlockInds     []uint32
		ConfirmHeight int64
		Version       string
		NetName       string
	}{
		tx,
		blocks,
		blockInds,
		exp.Height() - tx.Confirmations,
		exp.Version,
		exp.NetName,
	}

	str, err := exp.templates.execTemplateToString("tx", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Turbolinks-Location", r.URL.RequestURI())
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// AddressPage is the page handler for the "/address" path
func (exp *explorerUI) AddressPage(w http.ResponseWriter, r *http.Request) {
	// AddressPageData is the data structure passed to the HTML template
	type AddressPageData struct {
		Data          *AddressInfo
		ConfirmHeight []int64
		Version       string
		NetName       string
		OldestTxTime  int64
		IsLiteMode    bool
		ChartData     *dbtypes.ChartsData
	}

	// Get the address URL parameter, which should be set in the request context
	// by the addressPathCtx middleware.
	address, ok := r.Context().Value(ctxAddress).(string)
	if !ok {
		log.Trace("address not set")
		exp.StatusPage(w, defaultErrorCode, "there seems to not be an address in this request", NotFoundStatusType)
		return
	}

	// Number of outputs for the address to query the database for. The URL
	// query parameter "n" is used to specify the limit (e.g. "?n=20").
	limitN, err := strconv.ParseInt(r.URL.Query().Get("n"), 10, 64)
	if err != nil || limitN < 0 {
		limitN = defaultAddressRows
	} else if limitN > MaxAddressRows {
		log.Warnf("addressPage: requested up to %d address rows, "+
			"limiting to %d", limitN, MaxAddressRows)
		limitN = MaxAddressRows
	}

	// Number of outputs to skip (OFFSET in database query). For UX reasons, the
	// "start" URL query parameter is used.
	offsetAddrOuts, err := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
	if err != nil || offsetAddrOuts < 0 {
		offsetAddrOuts = 0
	}

	// Transaction types to show.
	txntype := r.URL.Query().Get("txntype")
	if txntype == "" {
		txntype = "all"
	}
	txnType := dbtypes.AddrTxnTypeFromStr(txntype)
	if txnType == dbtypes.AddrTxnUnknown {
		exp.StatusPage(w, defaultErrorCode, "unknown txntype query value", ErrorStatusType)
		return
	}
	log.Debugf("Showing transaction types: %s (%d)", txntype, txnType)

	var oldestTxBlockTime int64

	// Retrieve address information from the DB and/or RPC
	var addrData *AddressInfo
	if exp.liteMode {
		addrData = exp.blockData.GetExplorerAddress(address, limitN, offsetAddrOuts)
		if addrData == nil {
			log.Errorf("Unable to get address %s", address)
			exp.StatusPage(w, defaultErrorCode, "could not find that address", NotFoundStatusType)
			return
		}
	} else {
		// Get addresses table rows for the address
		addrHist, balance, errH := exp.explorerSource.AddressHistory(
			address, limitN, offsetAddrOuts, txnType)

		if errH == nil {
			// Generate AddressInfo skeleton from the address table rows
			addrData = ReduceAddressHistory(addrHist)
			if addrData == nil {
				// Empty history is not expected for credit txnType with any txns.
				if txnType != dbtypes.AddrTxnDebit && (balance.NumSpent+balance.NumUnspent) > 0 {
					log.Debugf("empty address history (%s): n=%d&start=%d", address, limitN, offsetAddrOuts)
					exp.StatusPage(w, defaultErrorCode, "that address has no history", NotFoundStatusType)
					return
				}
				// No mined transactions
				addrData = new(AddressInfo)
				addrData.Address = address
			}
			addrData.Fullmode = true

			// Balances and txn counts (partial unless in full mode)
			addrData.Balance = balance
			addrData.KnownTransactions = (balance.NumSpent * 2) + balance.NumUnspent
			addrData.KnownFundingTxns = balance.NumSpent + balance.NumUnspent
			addrData.KnownSpendingTxns = balance.NumSpent
			addrData.KnownMergedSpendingTxns = balance.NumMergedSpent

			// Transactions to fetch with FillAddressTransactions. This should be a
			// noop if ReduceAddressHistory is working right.
			switch txnType {
			case dbtypes.AddrTxnAll, dbtypes.AddrMergedTxnDebit:
			case dbtypes.AddrTxnCredit:
				addrData.Transactions = addrData.TxnsFunding
			case dbtypes.AddrTxnDebit:
				addrData.Transactions = addrData.TxnsSpending
			default:
				log.Warnf("Unknown address transaction type: %v", txnType)
			}

			// Transactions on current page
			addrData.NumTransactions = int64(len(addrData.Transactions))
			if addrData.NumTransactions > limitN {
				addrData.NumTransactions = limitN
			}

			// Query database for transaction details
			err = exp.explorerSource.FillAddressTransactions(addrData)
			if err != nil {
				log.Errorf("Unable to fill address %s transactions: %v", address, err)
				exp.StatusPage(w, defaultErrorCode, "could not find transactions for that address", NotFoundStatusType)
				return
			}
		} else {
			// We do not have any confirmed transactions.  Prep to display ONLY
			// unconfirmed transactions (or none at all)
			addrData = new(AddressInfo)
			addrData.Address = address
			addrData.Fullmode = true
			addrData.Balance = &AddressBalance{}
		}

		// Check for unconfirmed transactions
		addressOuts, numUnconfirmed, err := exp.blockData.UnconfirmedTxnsForAddress(address)
		if err != nil {
			log.Warnf("UnconfirmedTxnsForAddress failed for address %s: %v", address, err)
		}
		addrData.NumUnconfirmed = numUnconfirmed
		if addrData.UnconfirmedTxns == nil {
			addrData.UnconfirmedTxns = new(AddressTransactions)
		}
		// Funding transactions (unconfirmed)
		var received, sent, numReceived, numSent int64
	FUNDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.Outpoints {
			//Mempool transactions stick around for 2 blocks.  The first block
			//incorporates the transaction and mines it.  The second block
			//validates it by the stake.  However, transactions move into our
			//database as soon as they are mined and thus we need to be careful
			//to not include those transactions in our list.
			for _, b := range addrData.Transactions {
				if f.Hash.String() == b.TxID && f.Index == b.InOutID {
					continue FUNDING_TX_DUPLICATE_CHECK
				}
			}
			fundingTx, ok := addressOuts.TxnsStore[f.Hash]
			if !ok {
				log.Errorf("An outpoint's transaction is not available in TxnStore.")
				continue
			}
			if fundingTx.Confirmed() {
				log.Errorf("An outpoint's transaction is unexpectedly confirmed.")
				continue
			}
			if txnType == dbtypes.AddrTxnAll || txnType == dbtypes.AddrTxnCredit {
				addrTx := &AddressTx{
					TxID:          fundingTx.Hash().String(),
					InOutID:       f.Index,
					Time:          fundingTx.MemPoolTime,
					FormattedSize: humanize.Bytes(uint64(fundingTx.Tx.SerializeSize())),
					Total:         txhelpers.TotalOutFromMsgTx(fundingTx.Tx).ToCoin(),
					ReceivedTotal: dcrutil.Amount(fundingTx.Tx.TxOut[f.Index].Value).ToCoin(),
				}
				addrData.Transactions = append(addrData.Transactions, addrTx)
			}
			received += fundingTx.Tx.TxOut[f.Index].Value
			numReceived++

		}
		// Spending transactions (unconfirmed)
	SPENDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.PrevOuts {
			//Mempool transactions stick around for 2 blocks.  The first block
			//incorporates the transaction and mines it.  The second block
			//validates it by the stake.  However, transactions move into our
			//database as soon as they are mined and thus we need to be careful
			//to not include those transactions in our list.
			for _, b := range addrData.Transactions {
				if f.TxSpending.String() == b.TxID && f.InputIndex == int(b.InOutID) {
					continue SPENDING_TX_DUPLICATE_CHECK
				}
			}
			spendingTx, ok := addressOuts.TxnsStore[f.TxSpending]
			if !ok {
				log.Errorf("An outpoint's transaction is not available in TxnStore.")
				continue
			}
			if spendingTx.Confirmed() {
				log.Errorf("An outpoint's transaction is unexpectedly confirmed.")
				continue
			}

			// sent total sats has to be a lookup of the vout:i prevout value
			// because vin:i valuein is not reliable from dcrd at present
			prevhash := spendingTx.Tx.TxIn[f.InputIndex].PreviousOutPoint.Hash
			previndex := spendingTx.Tx.TxIn[f.InputIndex].PreviousOutPoint.Index
			valuein := addressOuts.TxnsStore[prevhash].Tx.TxOut[previndex].Value

			if txnType == dbtypes.AddrTxnAll || txnType == dbtypes.AddrTxnDebit {
				addrTx := &AddressTx{
					TxID:          spendingTx.Hash().String(),
					InOutID:       uint32(f.InputIndex),
					Time:          spendingTx.MemPoolTime,
					FormattedSize: humanize.Bytes(uint64(spendingTx.Tx.SerializeSize())),
					Total:         txhelpers.TotalOutFromMsgTx(spendingTx.Tx).ToCoin(),
					SentTotal:     dcrutil.Amount(valuein).ToCoin(),
				}
				addrData.Transactions = append(addrData.Transactions, addrTx)
			}

			sent += valuein
			numSent++
		}
		addrData.Balance.NumSpent += numSent
		addrData.Balance.NumUnspent += (numReceived - numSent)
		addrData.Balance.TotalSpent += sent
		addrData.Balance.TotalUnspent += (received - sent)

		if err != nil {
			log.Errorf("Unable to fetch transactions for the address %s: %v", address, err)
			exp.StatusPage(w, defaultErrorCode, "transactions for that address not found",
				NotFoundStatusType)
			return
		}

		// If there are transactions, check the oldest transaction's time.
		if len(addrData.Transactions) > 0 {
			oldestTxBlockTime, err = exp.explorerSource.GetOldestTxBlockTime(address)
			if err != nil {
				log.Errorf("Unable to fetch oldest transactions block time %s: %v", address, err)
				exp.StatusPage(w, defaultErrorCode, "oldest block time not found",
					NotFoundStatusType)
				return
			}
		}
	}

	// Set page parameters
	addrData.Path = r.URL.Path
	addrData.Limit, addrData.Offset = limitN, offsetAddrOuts
	addrData.TxnType = txnType.String()

	confirmHeights := make([]int64, len(addrData.Transactions))
	bdHeight := exp.Height()
	for i, v := range addrData.Transactions {
		confirmHeights[i] = bdHeight - int64(v.Confirmations)
	}

	sort.Slice(addrData.Transactions, func(i, j int) bool {
		if addrData.Transactions[i].Time == addrData.Transactions[j].Time {
			return addrData.Transactions[i].InOutID > addrData.Transactions[j].InOutID
		}
		return addrData.Transactions[i].Time > addrData.Transactions[j].Time
	})

	// addresscount := len(addressRows)
	// if addresscount > 0 {
	// 	calcoffset := int(math.Min(float64(addresscount), float64(offset)))
	// 	calcN := int(math.Min(float64(offset+N), float64(addresscount)))
	// 	log.Infof("Slicing result set which is %d addresses long to offset: %d and N: %d", addresscount, calcoffset, calcN)
	// 	addressRows = addressRows[calcoffset:calcN]
	// }

	pageData := AddressPageData{
		Data:          addrData,
		ConfirmHeight: confirmHeights,
		IsLiteMode:    exp.liteMode,
		OldestTxTime:  oldestTxBlockTime,
		Version:       exp.Version,
		NetName:       exp.NetName,
	}
	str, err := exp.templates.execTemplateToString("address", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Turbolinks-Location", r.URL.RequestURI())
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// DecodeTxPage handles the "decode/broadcast transaction" page. The actual
// decoding or broadcasting is handled by the websocket hub.
func (exp *explorerUI) DecodeTxPage(w http.ResponseWriter, r *http.Request) {
	str, err := exp.templates.execTemplateToString("rawtx", struct {
		Version string
		NetName string
	}{
		exp.Version,
		exp.NetName,
	})
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Charts handles the charts displays showing the various charts plotted.
func (exp *explorerUI) Charts(w http.ResponseWriter, r *http.Request) {
	if exp.liteMode {
		exp.StatusPage(w, fullModeRequired,
			"Charts page cannot run in lite mode", NotSupportedStatusType)
		return
	}
	tickets, err := exp.explorerSource.GetTicketsPriceByHeight()
	if err != nil {
		log.Errorf("Loading the Ticket Price By Height chart data failed %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}

	str, err := exp.templates.execTemplateToString("charts", struct {
		Version string
		NetName string
		Data    *dbtypes.ChartsData
	}{
		exp.Version,
		exp.NetName,
		tickets,
	})
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Search implements a primitive search algorithm by checking if the value in
// question is a block index, block hash, address hash or transaction hash and
// redirects to the appropriate page or displays an error
func (exp *explorerUI) Search(w http.ResponseWriter, r *http.Request) {
	searchStr := r.URL.Query().Get("search")
	if searchStr == "" {
		exp.StatusPage(w, "search failed", "Nothing was searched for", NotFoundStatusType)
		return
	}

	// Attempt to get a block hash by calling GetBlockHash to see if the value
	// is a block index and then redirect to the block page if it is
	idx, err := strconv.ParseInt(searchStr, 10, 0)
	if err == nil {
		_, err = exp.blockData.GetBlockHash(idx)
		if err == nil {
			http.Redirect(w, r, "/block/"+searchStr, http.StatusPermanentRedirect)
			return
		}
		exp.StatusPage(w, "search failed", "Block "+searchStr+" has not yet been mined", NotFoundStatusType)
		return
	}

	// Call GetExplorerAddress to see if the value is an address hash and
	// then redirect to the address page if it is
	address := exp.blockData.GetExplorerAddress(searchStr, 1, 0)
	if address != nil {
		http.Redirect(w, r, "/address/"+searchStr, http.StatusPermanentRedirect)
		return
	}

	// Check if the value is a valid hash
	if _, err = chainhash.NewHashFromStr(searchStr); err != nil {
		exp.StatusPage(w, "search failed", "Couldn't find any address "+searchStr, NotFoundStatusType)
		return
	}

	// Attempt to get a block index by calling GetBlockHeight to see if the
	// value is a block hash and then redirect to the block page if it is
	_, err = exp.blockData.GetBlockHeight(searchStr)
	if err == nil {
		http.Redirect(w, r, "/block/"+searchStr, http.StatusPermanentRedirect)
		return
	}

	// Call GetExplorerTx to see if the value is a transaction hash and then
	// redirect to the tx page if it is
	tx := exp.blockData.GetExplorerTx(searchStr)
	if tx != nil {
		http.Redirect(w, r, "/tx/"+searchStr, http.StatusPermanentRedirect)
		return
	}
	exp.StatusPage(w, "search failed", "Could not find any transaction or block "+searchStr, NotFoundStatusType)
}

// StatusPage provides a page for displaying status messages and exception
// handling without redirecting.
func (exp *explorerUI) StatusPage(w http.ResponseWriter, code string, message string, sType statusType) {
	str, err := exp.templates.execTemplateToString("status", struct {
		StatusType statusType
		Code       string
		Message    string
		Version    string
		NetName    string
	}{
		sType,
		code,
		message,
		exp.Version,
		exp.NetName,
	})
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		str = "Something went very wrong if you can see this, try refreshing"
	}

	w.Header().Set("Content-Type", "text/html")
	switch sType {
	case NotFoundStatusType:
		w.WriteHeader(http.StatusNotFound)
	case ErrorStatusType:
		w.WriteHeader(http.StatusInternalServerError)
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	io.WriteString(w, str)
}

// NotFound wraps StatusPage to display a 404 page
func (exp *explorerUI) NotFound(w http.ResponseWriter, r *http.Request) {
	exp.StatusPage(w, "Page not found.", "Cannot find page: "+r.URL.Path, NotFoundStatusType)
}

// ParametersPage is the page handler for the "/parameters" path
func (exp *explorerUI) ParametersPage(w http.ResponseWriter, r *http.Request) {
	cp := exp.ChainParams
	addrPrefix := AddressPrefixes(cp)
	actualTicketPoolSize := int64(cp.TicketPoolSize * cp.TicketsPerBlock)
	ecp := ExtendedChainParams{
		Params:               cp,
		AddressPrefix:        addrPrefix,
		ActualTicketPoolSize: actualTicketPoolSize,
	}

	str, err := exp.templates.execTemplateToString("parameters", struct {
		Cp      ExtendedChainParams
		Version string
		NetName string
	}{
		ecp,
		exp.Version,
		exp.NetName,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// AgendaPage is the page handler for the "/agenda" path
func (exp *explorerUI) AgendaPage(w http.ResponseWriter, r *http.Request) {
	if exp.liteMode {
		exp.StatusPage(w, fullModeRequired,
			"Agenda page cannot run in lite mode.", NotSupportedStatusType)
		return
	}
	errPageInvalidAgenda := func(err error) {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode,
			"the agenda ID given seems to not exist", NotFoundStatusType)
	}

	// Attempt to get agendaid string from URL path
	agendaid := getAgendaIDCtx(r)
	agendaInfo, err := GetAgendaInfo(agendaid)
	if err != nil {
		errPageInvalidAgenda(err)
		return
	}

	chartDataByTime, err := exp.explorerSource.AgendaVotes(agendaid, 0)
	if err != nil {
		errPageInvalidAgenda(err)
		return
	}

	chartDataByHeight, err := exp.explorerSource.AgendaVotes(agendaid, 1)
	if err != nil {
		errPageInvalidAgenda(err)
		return
	}

	str, err := exp.templates.execTemplateToString("agenda", struct {
		Ai               *agendadb.AgendaTagged
		Version          string
		NetName          string
		ChartDataByTime  *dbtypes.AgendaVoteChoices
		ChartDataByBlock *dbtypes.AgendaVoteChoices
	}{
		agendaInfo,
		exp.Version,
		exp.NetName,
		chartDataByTime,
		chartDataByHeight,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// AgendasPage is the page handler for the "/agendas" path
func (exp *explorerUI) AgendasPage(w http.ResponseWriter, r *http.Request) {
	agendas, err := agendadb.GetAllAgendas()
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}

	str, err := exp.templates.execTemplateToString("agendas", struct {
		Agendas []*agendadb.AgendaTagged
		Version string
		NetName string
	}{
		agendas,
		exp.Version,
		exp.NetName,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, ErrorStatusType)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}
