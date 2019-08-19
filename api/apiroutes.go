// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types"
	"github.com/decred/dcrd/rpcclient/v4"
	"github.com/decred/dcrd/wire"
	apitypes "github.com/decred/dcrdata/api/types/v4"
	"github.com/decred/dcrdata/db/cache/v2"
	"github.com/decred/dcrdata/db/dbtypes/v2"
	"github.com/decred/dcrdata/exchanges/v2"
	"github.com/decred/dcrdata/gov/v2/agendas"
	m "github.com/decred/dcrdata/middleware/v3"
	"github.com/decred/dcrdata/txhelpers/v3"
	appver "github.com/decred/dcrdata/v5/version"
)

// DataSource specifies an interface for advanced data collection using the
// auxiliary DB (e.g. PostgreSQL).
type DataSource interface {
	GetHeight() (int64, error)
	GetBestBlockHash() (string, error)
	GetBlockHash(idx int64) (string, error)
	GetBlockHeight(hash string) (int64, error)
	GetBlockByHash(string) (*wire.MsgBlock, error)
	SpendingTransaction(fundingTx string, vout uint32) (string, uint32, int8, error)
	SpendingTransactions(fundingTxID string) ([]string, []uint32, []uint32, error)
	AddressHistory(address string, N, offset int64, txnType dbtypes.AddrTxnViewType) ([]*dbtypes.AddressRow, *dbtypes.AddressBalance, error)
	FillAddressTransactions(addrInfo *dbtypes.AddressInfo) error
	AddressTransactionDetails(addr string, count, skip int64,
		txnType dbtypes.AddrTxnViewType) (*apitypes.Address, error)
	AddressTotals(address string) (*apitypes.AddressTotals, error)
	VotesInBlock(hash string) (int16, error)
	TxHistoryData(address string, addrChart dbtypes.HistoryChart,
		chartGroupings dbtypes.TimeBasedGrouping) (*dbtypes.ChartsData, error)
	TicketPoolVisualization(interval dbtypes.TimeBasedGrouping) (
		*dbtypes.PoolTicketsData, *dbtypes.PoolTicketsData, *dbtypes.PoolTicketsData, int64, error)
	AgendaVotes(agendaID string, chartType int) (*dbtypes.AgendaVoteChoices, error)
	AddressTxIoCsv(address string) ([][]string, error)
	Height() int64
	AllAgendas() (map[string]dbtypes.MileStone, error)
	GetTicketInfo(txid string) (*apitypes.TicketInfo, error)
	ProposalVotes(proposalToken string) (*dbtypes.ProposalChartsData, error)
	PowerlessTickets() (*apitypes.PowerlessTickets, error)
	GetStakeInfoExtendedByHash(hash string) *apitypes.StakeInfoExtended
	GetStakeInfoExtendedByHeight(idx int) *apitypes.StakeInfoExtended
	GetPoolInfo(idx int) *apitypes.TicketPoolInfo
	GetPoolInfoByHash(hash string) *apitypes.TicketPoolInfo
	GetPoolInfoRange(idx0, idx1 int) []apitypes.TicketPoolInfo
	GetPoolValAndSizeRange(idx0, idx1 int) ([]float64, []uint32)
	GetPool(idx int64) ([]string, error)
	CurrentCoinSupply() *apitypes.CoinSupply
	GetHeader(idx int) *chainjson.GetBlockHeaderVerboseResult
	GetBlockHeaderByHash(hash string) (*wire.BlockHeader, error)
	GetBlockVerboseByHash(hash string, verboseTx bool) *chainjson.GetBlockVerboseResult
	GetRawAPITransaction(txid *chainhash.Hash) *apitypes.Tx
	GetTransactionHex(txid *chainhash.Hash) string
	GetTrimmedTransaction(txid *chainhash.Hash) *apitypes.TrimmedTx
	GetVoteInfo(txid *chainhash.Hash) (*apitypes.VoteInfo, error)
	GetVoteVersionInfo(ver uint32) (*chainjson.GetVoteInfoResult, error)
	GetStakeVersionsLatest() (*chainjson.StakeVersions, error)
	GetAllTxIn(txid *chainhash.Hash) []*apitypes.TxIn
	GetAllTxOut(txid *chainhash.Hash) []*apitypes.TxOut
	GetTransactionsForBlockByHash(hash string) *apitypes.BlockTransactions
	GetStakeDiffEstimates() *apitypes.StakeDiff
	GetSummary(idx int) *apitypes.BlockDataBasic
	GetSummaryByHash(hash string, withTxTotals bool) *apitypes.BlockDataBasic
	GetBestBlockSummary() *apitypes.BlockDataBasic
	GetBlockSize(idx int) (int32, error)
	GetBlockSizeRange(idx0, idx1 int) ([]int32, error)
	GetSDiff(idx int) float64
	GetSDiffRange(idx0, idx1 int) []float64
	GetMempoolSSTxSummary() *apitypes.MempoolTicketFeeInfo
	GetMempoolSSTxFeeRates(N int) *apitypes.MempoolTicketFees
	GetMempoolSSTxDetails(N int) *apitypes.MempoolTicketDetails
	GetAddressTransactionsRawWithSkip(addr string, count, skip int) []*apitypes.AddressTxRaw
	GetMempoolPriceCountTime() *apitypes.PriceCountTime
}

// dcrdata application context used by all route handlers
type appContext struct {
	nodeClient   *rpcclient.Client
	Params       *chaincfg.Params
	DataSource   DataSource
	Status       *apitypes.Status
	JSONIndent   string
	xcBot        *exchanges.ExchangeBot
	AgendaDB     *agendas.AgendaDB
	maxCSVAddrs  int
	charts       *cache.ChartData
	isPiDisabled bool // is piparser disabled
}

// AppContextConfig is the configuration for the appContext and the only
// argument to its constructor.
type AppContextConfig struct {
	Client             *rpcclient.Client
	Params             *chaincfg.Params
	DataSource         DataSource
	JsonIndent         string
	XcBot              *exchanges.ExchangeBot
	AgendasDBInstance  *agendas.AgendaDB
	MaxAddrs           int
	Charts             *cache.ChartData
	IsPiparserDisabled bool
}

// NewContext constructs a new appContext from the RPC client, primary and
// auxiliary data sources, and JSON indentation string.
func NewContext(cfg *AppContextConfig) *appContext {
	conns, _ := cfg.Client.GetConnectionCount()
	nodeHeight, _ := cfg.Client.GetBlockCount()

	// DataSource is an interface that could have a value of pointer type.
	if cfg.DataSource == nil || reflect.ValueOf(cfg.DataSource).IsNil() {
		log.Errorf("NewContext: a DataSource is required.")
		return nil
	}

	return &appContext{
		nodeClient:   cfg.Client,
		Params:       cfg.Params,
		DataSource:   cfg.DataSource,
		xcBot:        cfg.XcBot,
		AgendaDB:     cfg.AgendasDBInstance,
		Status:       apitypes.NewStatus(uint32(nodeHeight), conns, APIVersion, appver.Version(), cfg.Params.Name),
		JSONIndent:   cfg.JsonIndent,
		maxCSVAddrs:  cfg.MaxAddrs,
		charts:       cfg.Charts,
		isPiDisabled: cfg.IsPiparserDisabled,
	}
}

func (c *appContext) updateNodeConnections() error {
	nodeConnections, err := c.nodeClient.GetConnectionCount()
	if err != nil {
		// Assume there arr no connections if RPC had an error.
		c.Status.SetConnections(0)
		return fmt.Errorf("failed to get connection count: %v", err)
	}

	// Before updating connections, get the previous connection count.
	prevConnections := c.Status.NodeConnections()

	c.Status.SetConnections(nodeConnections)
	if nodeConnections == 0 {
		return nil
	}

	// Detect if the node's peer connections were just restored.
	if prevConnections != 0 {
		// Status.ready may be false, but since connections were not lost and
		// then recovered, it is not our job to check other readiness factors.
		return nil
	}

	// Check the reconnected node's best block, and update Status.height.
	_, nodeHeight, err := c.nodeClient.GetBestBlock()
	if err != nil {
		c.Status.SetReady(false)
		return fmt.Errorf("node: getbestblock failed: %v", err)
	}

	// Update Status.height with current node height. This also sets
	// Status.ready according to the previously-set Status.dbHeight.
	c.Status.SetHeight(uint32(nodeHeight))

	return nil
}

// UpdateNodeHeight updates the Status height. This method satisfies
// notification.BlockHandlerLite.
func (c *appContext) UpdateNodeHeight(height uint32, _ string) error {
	c.Status.SetHeight(height)
	return nil
}

// StatusNtfnHandler keeps the appContext's Status up-to-date with changes in
// node and DB status.
func (c *appContext) StatusNtfnHandler(ctx context.Context, wg *sync.WaitGroup, wireHeightChan chan uint32) {
	defer wg.Done()
	// Check the node connection count periodically.
	rpcCheckTicker := time.NewTicker(5 * time.Second)
out:
	for {
	keepon:
		select {
		case <-rpcCheckTicker.C:
			if err := c.updateNodeConnections(); err != nil {
				log.Warn("updateNodeConnections: ", err)
				break keepon
			}

		case height, ok := <-wireHeightChan:
			if !ok {
				log.Warnf("Block connected channel closed.")
				break out
			}

			if c.DataSource == nil {
				panic("BlockData DataSourceLite is nil")
			}

			summary := c.DataSource.GetBestBlockSummary()
			if summary == nil {
				log.Errorf("BlockData summary is nil for height %d.", height)
				break keepon
			}

			c.Status.DBUpdate(height, summary.Time.UNIX())

			bdHeight, err := c.DataSource.GetHeight()
			// Catch certain pathological conditions.
			switch {
			case err != nil:
				log.Errorf("GetHeight failed: %v", err)
			case (height != uint32(bdHeight)) || (height != summary.Height):
				log.Errorf("New DB height (%d) and stored block data (%d, %d) are not consistent.",
					height, bdHeight, summary.Height)
			case bdHeight < 0:
				log.Warnf("DB empty (height = %d)", bdHeight)
			default:
				// If DB height agrees with node height, then we're ready.
				break keepon
			}

			c.Status.SetReady(false)

		case <-ctx.Done():
			log.Debugf("Got quit signal. Exiting block connected handler for STATUS monitor.")
			rpcCheckTicker.Stop()
			break out
		}
	}
}

// root is a http.Handler intended for the API root path. This essentially
// provides a heartbeat, and no information about the application status.
func (c *appContext) root(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, "dcrdata api running")
}

func (c *appContext) writeJSONHandlerFunc(thing interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, thing, c.JSONIndent)
	}
}

func writeJSON(w http.ResponseWriter, thing interface{}, indent string) {
	writeJSONWithStatus(w, thing, http.StatusOK, indent)
}

func writeJSONWithStatus(w http.ResponseWriter, thing interface{}, code int, indent string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", indent)
	if err := encoder.Encode(thing); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}
}

// writeJSONBytes prepares the headers for pre-encoded JSON and writes the JSON
// bytes.
func writeJSONBytes(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, err := w.Write(data)
	if err != nil {
		apiLog.Warnf("ResponseWriter.Write error: %v", err)
	}
}

// Measures length, sets common headers, formats, and sends CSV data.
func writeCSV(w http.ResponseWriter, rows [][]string, filename string, useCRLF bool) {
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment;filename=%s", filename))
	w.Header().Set("Content-Type", "text/csv")

	// To set the Content-Length response header, it is necessary to write the
	// CSV data into a buffer rather than streaming the response directly to the
	// http.ResponseWriter.
	buffer := new(bytes.Buffer)
	writer := csv.NewWriter(buffer)
	writer.UseCRLF = useCRLF
	err := writer.WriteAll(rows)
	if err != nil {
		log.Errorf("Failed to write address rows to buffer: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError)
		return
	}

	bytesToSend := int64(buffer.Len())
	w.Header().Set("Content-Length", strconv.FormatInt(bytesToSend, 10))

	bytesWritten, err := buffer.WriteTo(w)
	if err != nil {
		log.Errorf("Failed to transfer address rows from buffer. "+
			"%d bytes written. %v", bytesWritten, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError)
		return
	}

	// Warn if the number of bytes sent differs from buffer length.
	if bytesWritten != bytesToSend {
		log.Warnf("Failed to send the entire file. Sent %d of %d bytes.",
			bytesWritten, bytesToSend)
	}
}

func (c *appContext) getIndentQuery(r *http.Request) (indent string) {
	useIndentation := r.URL.Query().Get("indent")
	if useIndentation == "1" || useIndentation == "true" {
		indent = c.JSONIndent
	}
	return
}

func getVoteVersionQuery(r *http.Request) (int32, string, error) {
	verLatest := int64(m.GetLatestVoteVersionCtx(r))
	voteVersion := r.URL.Query().Get("version")
	if voteVersion == "" {
		return int32(verLatest), voteVersion, nil
	}

	ver, err := strconv.ParseInt(voteVersion, 10, 0)
	if err != nil {
		return -1, voteVersion, err
	}
	if ver > verLatest {
		ver = verLatest
	}

	return int32(ver), voteVersion, nil
}

func (c *appContext) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, c.Status.API(), c.getIndentQuery(r))
}

func (c *appContext) statusHappy(w http.ResponseWriter, r *http.Request) {
	happy := c.Status.Happy()
	statusCode := http.StatusOK
	if !happy.Happy {
		// For very simple health checks, set the status code.
		statusCode = http.StatusServiceUnavailable
	}
	writeJSONWithStatus(w, happy, statusCode, c.getIndentQuery(r))
}

func (c *appContext) coinSupply(w http.ResponseWriter, r *http.Request) {
	supply := c.DataSource.CurrentCoinSupply()
	if supply == nil {
		apiLog.Error("Unable to get coin supply.")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, supply, c.getIndentQuery(r))
}

func (c *appContext) currentHeight(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := io.WriteString(w, strconv.Itoa(int(c.Status.Height()))); err != nil {
		apiLog.Infof("failed to write height response: %v", err)
	}
}

func (c *appContext) getBlockHeight(w http.ResponseWriter, r *http.Request) {
	idx, err := c.getBlockHeightCtx(r)
	if err != nil {
		apiLog.Infof("getBlockHeight: getBlockHeightCtx failed: %v", err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := io.WriteString(w, strconv.Itoa(int(idx))); err != nil {
		apiLog.Infof("failed to write height response: %v", err)
	}
}

func (c *appContext) getBlockHash(w http.ResponseWriter, r *http.Request) {
	hash, err := c.getBlockHashCtx(r)
	if err != nil {
		apiLog.Debugf("getBlockHash: %v", err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := io.WriteString(w, hash); err != nil {
		apiLog.Infof("failed to write height response: %v", err)
	}
}

func (c *appContext) getBlockSummary(w http.ResponseWriter, r *http.Request) {
	// attempt to get hash of block set by hash or (fallback) height set on path
	hash, err := c.getBlockHashCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	txTotalsParam := r.URL.Query().Get("txtotals")
	withTxTotals := txTotalsParam == "1" || strings.EqualFold(txTotalsParam, "true")

	blockSummary := c.DataSource.GetSummaryByHash(hash, withTxTotals)
	if blockSummary == nil {
		apiLog.Errorf("Unable to get block %s summary", hash)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, blockSummary, c.getIndentQuery(r))
}

func (c *appContext) getBlockTransactions(w http.ResponseWriter, r *http.Request) {
	hash, err := c.getBlockHashCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockTransactions := c.DataSource.GetTransactionsForBlockByHash(hash)
	if blockTransactions == nil {
		apiLog.Errorf("Unable to get block %s transactions", hash)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, blockTransactions, c.getIndentQuery(r))
}

func (c *appContext) getBlockTransactionsCount(w http.ResponseWriter, r *http.Request) {
	hash, err := c.getBlockHashCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockTransactions := c.DataSource.GetTransactionsForBlockByHash(hash)
	if blockTransactions == nil {
		apiLog.Errorf("Unable to get block %s transactions", hash)
		return
	}

	counts := &apitypes.BlockTransactionCounts{
		Tx:  len(blockTransactions.Tx),
		STx: len(blockTransactions.STx),
	}
	writeJSON(w, counts, c.getIndentQuery(r))
}

func (c *appContext) getBlockHeader(w http.ResponseWriter, r *http.Request) {
	idx, err := c.getBlockHeightCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockHeader := c.DataSource.GetHeader(int(idx))
	if blockHeader == nil {
		apiLog.Errorf("Unable to get block %d header", idx)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, blockHeader, c.getIndentQuery(r))
}

func (c *appContext) getBlockRaw(w http.ResponseWriter, r *http.Request) {
	hash, err := c.getBlockHashCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	msgBlock, err := c.DataSource.GetBlockByHash(hash)
	if err != nil {
		apiLog.Errorf("Unable to get block %s: %v", hash, err)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	var hexString strings.Builder
	hexString.Grow(msgBlock.SerializeSize())
	err = msgBlock.Serialize(hex.NewEncoder(&hexString))
	if err != nil {
		apiLog.Errorf("Unable to serialize block %s: %v", hash, err)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockRaw := &apitypes.BlockRaw{
		Height: msgBlock.Header.Height,
		Hash:   hash,
		Hex:    hexString.String(),
	}

	writeJSON(w, blockRaw, c.getIndentQuery(r))
}

func (c *appContext) getBlockHeaderRaw(w http.ResponseWriter, r *http.Request) {
	hash, err := c.getBlockHashCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockHeader, err := c.DataSource.GetBlockHeaderByHash(hash)
	if err != nil {
		apiLog.Errorf("Unable to get block %s: %v", hash, err)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	var hexString strings.Builder
	err = blockHeader.Serialize(hex.NewEncoder(&hexString))
	if err != nil {
		apiLog.Errorf("Unable to serialize block %s: %v", hash, err)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockRaw := &apitypes.BlockRaw{
		Height: blockHeader.Height,
		Hash:   hash,
		Hex:    hexString.String(),
	}

	writeJSON(w, blockRaw, c.getIndentQuery(r))
}

func (c *appContext) getBlockVerbose(w http.ResponseWriter, r *http.Request) {
	hash, err := c.getBlockHashCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockVerbose := c.DataSource.GetBlockVerboseByHash(hash, false)
	if blockVerbose == nil {
		apiLog.Errorf("Unable to get block %s", hash)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, blockVerbose, c.getIndentQuery(r))
}

func (c *appContext) getVoteInfo(w http.ResponseWriter, r *http.Request) {
	ver, verStr, err := getVoteVersionQuery(r)
	if err != nil || ver < 0 {
		apiLog.Errorf("Unable to get vote info for stake version %s", verStr)
		http.Error(w, "Unable to get vote info for stake version "+verStr, 422)
		return
	}
	voteVersionInfo, err := c.DataSource.GetVoteVersionInfo(uint32(ver))
	if err != nil || voteVersionInfo == nil {
		apiLog.Errorf("Unable to get vote version %d info: %v", ver, err)
		http.Error(w, "Unable to get vote info for stake version "+verStr, 422)
		return
	}
	writeJSON(w, voteVersionInfo, c.getIndentQuery(r))
}

// setOutputSpends retrieves spending transaction information for each output of
// the specified transaction. This sets the vouts[i].Spend fields for each
// output that is spent. For unspent outputs, the Spend field remains a nil
// pointer.
func (c *appContext) setOutputSpends(txid string, vouts []apitypes.Vout) error {
	// For each output of this transaction, look up any spending transactions,
	// and the index of the spending transaction input.
	spendHashes, spendVinInds, voutInds, err := c.DataSource.SpendingTransactions(txid)
	if dbtypes.IsTimeoutErr(err) {
		return fmt.Errorf("SpendingTransactions: %v", err)
	}
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("unable to get spending transaction info for outputs of %s", txid)
	}
	if len(voutInds) > len(vouts) {
		return fmt.Errorf("invalid spending transaction data for %s", txid)
	}
	for i, vout := range voutInds {
		if int(vout) >= len(vouts) {
			return fmt.Errorf("invalid spending transaction data (%s:%d)", txid, vout)
		}
		vouts[vout].Spend = &apitypes.TxInputID{
			Hash:  spendHashes[i],
			Index: spendVinInds[i],
		}
	}
	return nil
}

// setTxSpends retrieves spending transaction information for each output of the
// given transaction. This sets the tx.Vout[i].Spend fields for each output that
// is spent. For unspent outputs, the Spend field remains a nil pointer.
func (c *appContext) setTxSpends(tx *apitypes.Tx) error {
	return c.setOutputSpends(tx.TxID, tx.Vout)
}

// setTrimmedTxSpends is like setTxSpends except that it operates on a TrimmedTx
// instead of a Tx.
func (c *appContext) setTrimmedTxSpends(tx *apitypes.TrimmedTx) error {
	return c.setOutputSpends(tx.TxID, tx.Vout)
}

func (c *appContext) getTransaction(w http.ResponseWriter, r *http.Request) {
	txid, err := m.GetTxIDCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	tx := c.DataSource.GetRawAPITransaction(txid)
	if tx == nil {
		apiLog.Errorf("Unable to get transaction %s", txid)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	// Look up any spending transactions for each output of this transaction
	// when the client requests spends with the URL query ?spends=true.
	spendParam := r.URL.Query().Get("spends")
	withSpends := spendParam == "1" || strings.EqualFold(spendParam, "true")

	if withSpends {
		if err := c.setTxSpends(tx); err != nil {
			apiLog.Errorf("Unable to get spending transaction info for outputs of %s: %v", txid, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError),
				http.StatusInternalServerError)
			return
		}
	}

	writeJSON(w, tx, c.getIndentQuery(r))
}

func (c *appContext) getTransactionHex(w http.ResponseWriter, r *http.Request) {
	txid, err := m.GetTxIDCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	hex := c.DataSource.GetTransactionHex(txid)

	fmt.Fprint(w, hex)
}

func (c *appContext) getDecodedTx(w http.ResponseWriter, r *http.Request) {
	txid, err := m.GetTxIDCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	tx := c.DataSource.GetTrimmedTransaction(txid)
	if tx == nil {
		apiLog.Errorf("Unable to get transaction %s", txid)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	// Look up any spending transactions for each output of this transaction
	// when the client requests spends with the URL query ?spends=true.
	spendParam := r.URL.Query().Get("spends")
	withSpends := spendParam == "1" || strings.EqualFold(spendParam, "true")

	if withSpends {
		if err := c.setTrimmedTxSpends(tx); err != nil {
			apiLog.Errorf("Unable to get spending transaction info for outputs of %s: %v", txid, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError),
				http.StatusInternalServerError)
			return
		}
	}

	writeJSON(w, tx, c.getIndentQuery(r))
}

func (c *appContext) getTransactions(w http.ResponseWriter, r *http.Request) {
	txids, err := m.GetTxnsCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	// Look up any spending transactions for each output of this transaction
	// when the client requests spends with the URL query ?spends=true.
	spendParam := r.URL.Query().Get("spends")
	withSpends := spendParam == "1" || strings.EqualFold(spendParam, "true")

	txns := make([]*apitypes.Tx, 0, len(txids))
	for i := range txids {
		tx := c.DataSource.GetRawAPITransaction(txids[i])
		if tx == nil {
			apiLog.Errorf("Unable to get transaction %s", txids[i])
			http.Error(w, http.StatusText(422), 422)
			return
		}

		if withSpends {
			if err := c.setTxSpends(tx); err != nil {
				apiLog.Errorf("Unable to get spending transaction info for outputs of %s: %v",
					txids[i], err)
				http.Error(w, http.StatusText(http.StatusInternalServerError),
					http.StatusInternalServerError)
				return
			}
		}

		txns = append(txns, tx)
	}

	writeJSON(w, txns, c.getIndentQuery(r))
}

func (c *appContext) getDecodedTransactions(w http.ResponseWriter, r *http.Request) {
	txids, err := m.GetTxnsCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	txns := make([]*apitypes.TrimmedTx, 0, len(txids))
	for i := range txids {
		tx := c.DataSource.GetTrimmedTransaction(txids[i])
		if tx == nil {
			apiLog.Errorf("Unable to get transaction %v", tx)
			http.Error(w, http.StatusText(422), 422)
			return
		}
		txns = append(txns, tx)
	}

	writeJSON(w, txns, c.getIndentQuery(r))
}

func (c *appContext) getTxVoteInfo(w http.ResponseWriter, r *http.Request) {
	txid, err := m.GetTxIDCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	vinfo, err := c.DataSource.GetVoteInfo(txid)
	if err != nil {
		err = fmt.Errorf("unable to get vote info for tx %v: %v",
			txid, err)
		apiLog.Error(err)
		http.Error(w, err.Error(), 422)
		return
	}
	writeJSON(w, vinfo, c.getIndentQuery(r))
}

// For /tx/{txid}/tinfo
func (c *appContext) getTxTicketInfo(w http.ResponseWriter, r *http.Request) {
	txid, err := m.GetTxIDCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	tinfo, err := c.DataSource.GetTicketInfo(txid.String())
	if err != nil {
		err = fmt.Errorf("unable to get ticket info for tx %v: %v",
			txid, err)
		apiLog.Error(err)
		http.Error(w, err.Error(), 422)
		return
	}
	writeJSON(w, tinfo, c.getIndentQuery(r))
}

// getTransactionInputs serves []TxIn
func (c *appContext) getTransactionInputs(w http.ResponseWriter, r *http.Request) {
	txid, err := m.GetTxIDCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	allTxIn := c.DataSource.GetAllTxIn(txid)
	// allTxIn may be empty, but not a nil slice
	if allTxIn == nil {
		apiLog.Errorf("Unable to get all TxIn for transaction %s", txid)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, allTxIn, c.getIndentQuery(r))
}

// getTransactionInput serves TxIn[i]
func (c *appContext) getTransactionInput(w http.ResponseWriter, r *http.Request) {
	txid, err := m.GetTxIDCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	index := m.GetTxIOIndexCtx(r)
	if index < 0 {
		http.NotFound(w, r)
		//http.Error(w, http.StatusText(422), 422)
		return
	}

	allTxIn := c.DataSource.GetAllTxIn(txid)
	// allTxIn may be empty, but not a nil slice
	if allTxIn == nil {
		apiLog.Warnf("Unable to get all TxIn for transaction %s", txid)
		http.NotFound(w, r)
		return
	}

	if len(allTxIn) <= index {
		apiLog.Debugf("Index %d larger than []TxIn length %d", index, len(allTxIn))
		http.NotFound(w, r)
		return
	}

	writeJSON(w, *allTxIn[index], c.getIndentQuery(r))
}

// getTransactionOutputs serves []TxOut
func (c *appContext) getTransactionOutputs(w http.ResponseWriter, r *http.Request) {
	txid, err := m.GetTxIDCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	allTxOut := c.DataSource.GetAllTxOut(txid)
	// allTxOut may be empty, but not a nil slice
	if allTxOut == nil {
		apiLog.Errorf("Unable to get all TxOut for transaction %s", txid)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, allTxOut, c.getIndentQuery(r))
}

// getTransactionOutput serves TxOut[i]
func (c *appContext) getTransactionOutput(w http.ResponseWriter, r *http.Request) {
	txid, err := m.GetTxIDCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	index := m.GetTxIOIndexCtx(r)
	if index < 0 {
		http.NotFound(w, r)
		return
	}

	allTxOut := c.DataSource.GetAllTxOut(txid)
	// allTxOut may be empty, but not a nil slice
	if allTxOut == nil {
		apiLog.Errorf("Unable to get all TxOut for transaction %s", txid)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	if len(allTxOut) <= index {
		apiLog.Debugf("Index %d larger than []TxOut length %d", index, len(allTxOut))
		http.NotFound(w, r)
		return
	}

	writeJSON(w, *allTxOut[index], c.getIndentQuery(r))
}

// getBlockStakeInfoExtendedByHash retrieves the apitype.StakeInfoExtended
// for the given blockhash
func (c *appContext) getBlockStakeInfoExtendedByHash(w http.ResponseWriter, r *http.Request) {
	hash, err := c.getBlockHashCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	stakeinfo := c.DataSource.GetStakeInfoExtendedByHash(hash)
	if stakeinfo == nil {
		apiLog.Errorf("Unable to get block fee info for %s", hash)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, stakeinfo, c.getIndentQuery(r))
}

// getBlockStakeInfoExtendedByHeight retrieves the apitype.StakeInfoExtended
// for the given blockheight on mainchain
func (c *appContext) getBlockStakeInfoExtendedByHeight(w http.ResponseWriter, r *http.Request) {
	idx, err := c.getBlockHeightCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	stakeinfo := c.DataSource.GetStakeInfoExtendedByHeight(int(idx))
	if stakeinfo == nil {
		apiLog.Errorf("Unable to get stake info for height %d", idx)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, stakeinfo, c.getIndentQuery(r))
}

func (c *appContext) getStakeDiffSummary(w http.ResponseWriter, r *http.Request) {
	stakeDiff := c.DataSource.GetStakeDiffEstimates()
	if stakeDiff == nil {
		apiLog.Errorf("Unable to get stake diff info")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, stakeDiff, c.getIndentQuery(r))
}

// Encodes apitypes.PowerlessTickets, which is missed or expired tickets sorted
// by revocation status.
func (c *appContext) getPowerlessTickets(w http.ResponseWriter, r *http.Request) {
	tickets, err := c.DataSource.PowerlessTickets()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, tickets, c.getIndentQuery(r))
}

func (c *appContext) getStakeDiffCurrent(w http.ResponseWriter, r *http.Request) {
	stakeDiff := c.DataSource.GetStakeDiffEstimates()
	if stakeDiff == nil {
		apiLog.Errorf("Unable to get stake diff info")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	stakeDiffCurrent := chainjson.GetStakeDifficultyResult{
		CurrentStakeDifficulty: stakeDiff.CurrentStakeDifficulty,
		NextStakeDifficulty:    stakeDiff.NextStakeDifficulty,
	}

	writeJSON(w, stakeDiffCurrent, c.getIndentQuery(r))
}

func (c *appContext) getStakeDiffEstimates(w http.ResponseWriter, r *http.Request) {
	stakeDiff := c.DataSource.GetStakeDiffEstimates()
	if stakeDiff == nil {
		apiLog.Errorf("Unable to get stake diff info")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, stakeDiff.Estimates, c.getIndentQuery(r))
}

func (c *appContext) getSSTxSummary(w http.ResponseWriter, r *http.Request) {
	sstxSummary := c.DataSource.GetMempoolSSTxSummary()
	if sstxSummary == nil {
		apiLog.Errorf("Unable to get SSTx info from mempool")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, sstxSummary, c.getIndentQuery(r))
}

func (c *appContext) getSSTxFees(w http.ResponseWriter, r *http.Request) {
	N := m.GetNCtx(r)
	sstxFees := c.DataSource.GetMempoolSSTxFeeRates(N)
	if sstxFees == nil {
		apiLog.Errorf("Unable to get SSTx fees from mempool")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, sstxFees, c.getIndentQuery(r))
}

func (c *appContext) getSSTxDetails(w http.ResponseWriter, r *http.Request) {
	N := m.GetNCtx(r)
	sstxDetails := c.DataSource.GetMempoolSSTxDetails(N)
	if sstxDetails == nil {
		apiLog.Errorf("Unable to get SSTx details from mempool")
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, sstxDetails, c.getIndentQuery(r))
}

// getTicketPoolCharts pulls the initial data to populate the /ticketpool page
// charts.
func (c *appContext) getTicketPoolCharts(w http.ResponseWriter, r *http.Request) {
	timeChart, priceChart, outputsChart, height, err := c.DataSource.TicketPoolVisualization(dbtypes.AllGrouping)
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("TicketPoolVisualization: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		apiLog.Errorf("Unable to get ticket pool charts: %v", err)
		http.Error(w, http.StatusText(http.StatusUnprocessableEntity), http.StatusUnprocessableEntity)
		return
	}

	mp := c.DataSource.GetMempoolPriceCountTime()

	response := &apitypes.TicketPoolChartsData{
		ChartHeight:  uint64(height),
		TimeChart:    timeChart,
		PriceChart:   priceChart,
		OutputsChart: outputsChart,
		Mempool:      mp,
	}

	writeJSON(w, response, c.getIndentQuery(r))

}

func (c *appContext) getTicketPoolByDate(w http.ResponseWriter, r *http.Request) {
	tp := m.GetTpCtx(r)
	// default to day if no grouping was sent
	if tp == "" {
		tp = "day"
	}

	// The db queries are fast enough that it makes sense to call
	// TicketPoolVisualization here even though it returns a lot of data not
	// needed by this request.
	interval := dbtypes.TimeGroupingFromStr(tp)
	timeChart, _, _, height, err := c.DataSource.TicketPoolVisualization(interval)
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("TicketPoolVisualization: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		apiLog.Errorf("Unable to get ticket pool by date: %v", err)
		http.Error(w, http.StatusText(http.StatusUnprocessableEntity), http.StatusUnprocessableEntity)
		return
	}

	tpResponse := struct {
		Height    int64                    `json:"height"`
		TimeChart *dbtypes.PoolTicketsData `json:"time_chart"`
	}{
		height,
		timeChart, // purchase time distribution
	}

	writeJSON(w, tpResponse, c.getIndentQuery(r))
}

func (c *appContext) getProposalChartData(w http.ResponseWriter, r *http.Request) {
	if c.isPiDisabled {
		errMsg := "piparser is disabled."
		apiLog.Errorf("%s. Remove the disable-piparser flag to activate it.", errMsg)
		http.Error(w, errMsg, http.StatusServiceUnavailable)
		return
	}

	token := m.GetProposalTokenCtx(r)
	votesData, err := c.DataSource.ProposalVotes(token)
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("ProposalVotes: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		apiLog.Errorf("Unable to get proposals votes for token %s : %v", token, err)
		http.Error(w, http.StatusText(http.StatusUnprocessableEntity),
			http.StatusUnprocessableEntity)
		return
	}

	writeJSON(w, votesData, c.getIndentQuery(r))
}

func (c *appContext) getBlockSize(w http.ResponseWriter, r *http.Request) {
	idx, err := c.getBlockHeightCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockSize, err := c.DataSource.GetBlockSize(int(idx))
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, blockSize, "")
}

func (c *appContext) blockSubsidies(w http.ResponseWriter, r *http.Request) {
	idx, err := c.getBlockHeightCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	hash, err := c.getBlockHashCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	// Unless this is a mined block, assume all votes.
	numVotes := int16(c.Params.TicketsPerBlock)
	if hash != "" {
		var err error
		numVotes, err = c.DataSource.VotesInBlock(hash)
		if dbtypes.IsTimeoutErr(err) {
			apiLog.Errorf("VotesInBlock: %v", err)
			http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}

	work, stake, tax := txhelpers.RewardsAtBlock(idx, uint16(numVotes), c.Params)
	rewards := apitypes.BlockSubsidies{
		BlockNum:   idx,
		BlockHash:  hash,
		Work:       work,
		Stake:      stake,
		NumVotes:   numVotes,
		TotalStake: stake * int64(numVotes),
		Tax:        tax,
		Total:      work + stake*int64(numVotes) + tax,
	}

	writeJSON(w, rewards, c.getIndentQuery(r))
}

func (c *appContext) getBlockRangeSize(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 || idx < idx0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockSizes, err := c.DataSource.GetBlockSizeRange(idx0, idx)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, blockSizes, "")
}

func (c *appContext) getBlockRangeSteppedSize(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 || idx < idx0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	step := m.GetBlockStepCtx(r)
	if step <= 0 {
		http.Error(w, "Yeaaah, that step's not gonna work with me.", 422)
		return
	}

	blockSizesFull, err := c.DataSource.GetBlockSizeRange(idx0, idx)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	var blockSizes []int32
	if step == 1 {
		blockSizes = blockSizesFull
	} else {
		numValues := (idx - idx0 + 1) / step
		blockSizes = make([]int32, 0, numValues)
		for i := idx0; i <= idx; i += step {
			blockSizes = append(blockSizes, blockSizesFull[i-idx0])
		}
		// it's the client's problem if i doesn't go all the way to idx
	}

	writeJSON(w, blockSizes, "")
}

func (c *appContext) getBlockRangeSummary(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	// w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// N := idx - idx0 + 1
	// summaries := make([]*apitypes.BlockDataBasic, 0, N)
	// for i := idx0; i <= idx; i++ {
	// 	summaries = append(summaries, c.BlockData.GetSummary(i))
	// }
	// writeJSON(w, summaries, c.getIndentQuery(r))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	indent := c.getIndentQuery(r)
	prefix, newline := indent, ""
	encoder.SetIndent(prefix, indent)
	if indent != "" {
		newline = "\n"
	}
	fmt.Fprintf(w, "[%s%s", newline, prefix)
	for i := idx0; i <= idx; i++ {
		summary := c.DataSource.GetSummary(i)
		if summary == nil {
			apiLog.Debugf("Unknown block %d", i)
			http.Error(w, fmt.Sprintf("I don't know block %d", i), http.StatusNotFound)
			return
		}
		// TODO: deal with the extra newline from Encode, if needed
		if err := encoder.Encode(summary); err != nil {
			apiLog.Infof("JSON encode error: %v", err)
			http.Error(w, http.StatusText(422), 422)
			return
		}
		if i != idx {
			fmt.Fprintf(w, ",%s%s", newline, prefix)
		}
	}
	fmt.Fprintf(w, "]")
}

func (c *appContext) getBlockRangeSteppedSummary(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	step := m.GetBlockStepCtx(r)
	if step <= 0 {
		http.Error(w, "Yeaaah, that step's not gonna work with me.", 422)
		return
	}

	// Compute the last block in the range
	numSteps := (idx - idx0) / step
	last := idx0 + step*numSteps
	// Support reverse list (e.g. 10/0/5 counts down from 10 to 0 in steps of 5)
	if idx0 > idx {
		step = -step
		// TODO: support reverse in other endpoints
	}

	// Prepare JSON encode for streaming response
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	indent := c.getIndentQuery(r)
	prefix, newline := indent, ""
	encoder.SetIndent(prefix, indent)
	if indent != "" {
		newline = "\n"
	}

	// Manually structure outer JSON array
	fmt.Fprintf(w, "[%s%s", newline, prefix)
	// Go through blocks in list, stop after last (i.e. on last+step)
	for i := idx0; i != last+step; i += step {
		summary := c.DataSource.GetSummary(i)
		if summary == nil {
			apiLog.Debugf("Unknown block %d", i)
			http.Error(w, fmt.Sprintf("I don't know block %d", i), http.StatusNotFound)
			return
		}
		// TODO: deal with the extra newline from Encode, if needed
		if err := encoder.Encode(summary); err != nil {
			apiLog.Infof("JSON encode error: %v", err)
			http.Error(w, http.StatusText(422), 422)
			return
		}
		// After last block, do not print comma+newline+prefix
		if i != last {
			fmt.Fprintf(w, ",%s%s", newline, prefix)
		}
	}
	fmt.Fprintf(w, "]")
}

func (c *appContext) getTicketPool(w http.ResponseWriter, r *http.Request) {
	// getBlockHeightCtx falls back to try hash if height fails
	idx, err := c.getBlockHeightCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	tp, err := c.DataSource.GetPool(idx)
	if err != nil {
		apiLog.Errorf("Unable to fetch ticket pool: %v", err)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	sortPool := r.URL.Query().Get("sort")
	if sortPool == "1" || sortPool == "true" {
		sort.Strings(tp)
	}

	writeJSON(w, tp, c.getIndentQuery(r))
}

func (c *appContext) getTicketPoolInfo(w http.ResponseWriter, r *http.Request) {
	idx, err := c.getBlockHeightCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	tpi := c.DataSource.GetPoolInfo(int(idx))
	writeJSON(w, tpi, c.getIndentQuery(r))
}

func (c *appContext) getTicketPoolInfoRange(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	useArray := r.URL.Query().Get("arrays")
	if useArray == "1" || useArray == "true" {
		c.getTicketPoolValAndSizeRange(w, r)
		return
	}

	tpis := c.DataSource.GetPoolInfoRange(idx0, idx)
	if tpis == nil {
		http.Error(w, "invalid range", http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, tpis, c.getIndentQuery(r))
}

func (c *appContext) getTicketPoolValAndSizeRange(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	pvs, pss := c.DataSource.GetPoolValAndSizeRange(idx0, idx)
	if pvs == nil || pss == nil {
		http.Error(w, "invalid range", http.StatusUnprocessableEntity)
		return
	}

	tPVS := apitypes.TicketPoolValsAndSizes{
		StartHeight: uint32(idx0),
		EndHeight:   uint32(idx),
		Value:       pvs,
		Size:        pss,
	}
	writeJSON(w, tPVS, c.getIndentQuery(r))
}

func (c *appContext) getStakeDiff(w http.ResponseWriter, r *http.Request) {
	idx, err := c.getBlockHeightCtx(r)
	if err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	sdiff := c.DataSource.GetSDiff(int(idx))
	writeJSON(w, []float64{sdiff}, c.getIndentQuery(r))
}

func (c *appContext) getStakeDiffRange(w http.ResponseWriter, r *http.Request) {
	idx0 := m.GetBlockIndex0Ctx(r)
	if idx0 < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	idx := m.GetBlockIndexCtx(r)
	if idx < 0 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	sdiffs := c.DataSource.GetSDiffRange(idx0, idx)
	writeJSON(w, sdiffs, c.getIndentQuery(r))
}

func (c *appContext) addressTotals(w http.ResponseWriter, r *http.Request) {
	addresses, err := m.GetAddressCtx(r, c.Params, 1)
	if err != nil || len(addresses) > 1 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	address := addresses[0]
	totals, err := c.DataSource.AddressTotals(address)
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("AddressTotals: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		log.Warnf("failed to get address totals (%s): %v", address, err)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, totals, c.getIndentQuery(r))
}

// Handler for address activity CSV file download.
// /download/address/io/{address}?cr=[true|false]
func (c *appContext) addressIoCsv(w http.ResponseWriter, r *http.Request) {
	addresses, err := m.GetAddressCtx(r, c.Params, 1)
	if err != nil || len(addresses) > 1 {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	address := addresses[0]

	_, _, addrErr := txhelpers.AddressValidation(address, c.Params)
	if addrErr != nil {
		log.Debugf("Error validating address %s: %v", address, addrErr)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	rows, err := c.DataSource.AddressTxIoCsv(address)
	if err != nil {
		log.Errorf("Failed to fetch AddressTxIoCsv: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("address-io-%s-%d-%s.csv", address,
		c.Status.Height(), strconv.FormatInt(time.Now().Unix(), 10))

	// Check if ?cr=true was specified.
	crlfParam := r.URL.Query().Get("cr")
	useCRLF := crlfParam == "1" || strings.EqualFold(crlfParam, "true")

	writeCSV(w, rows, filename, useCRLF)
}

func (c *appContext) getAddressTxTypesData(w http.ResponseWriter, r *http.Request) {
	addresses, err := m.GetAddressCtx(r, c.Params, 1)
	if err != nil || len(addresses) > 1 {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	address := addresses[0]

	chartGrouping := m.GetChartGroupingCtx(r)
	if chartGrouping == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	data, err := c.DataSource.TxHistoryData(address, dbtypes.TxsType,
		dbtypes.TimeGroupingFromStr(chartGrouping))
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("TxHistoryData: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		log.Warnf("failed to get address (%s) history by tx type : %v", address, err)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, data, c.getIndentQuery(r))
}

func (c *appContext) getAddressTxAmountFlowData(w http.ResponseWriter, r *http.Request) {
	addresses, err := m.GetAddressCtx(r, c.Params, 1)
	if err != nil || len(addresses) > 1 {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	address := addresses[0]

	chartGrouping := m.GetChartGroupingCtx(r)
	if chartGrouping == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	data, err := c.DataSource.TxHistoryData(address, dbtypes.AmountFlow,
		dbtypes.TimeGroupingFromStr(chartGrouping))
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("TxHistoryData: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		log.Warnf("failed to get address (%s) history by amount flow: %v", address, err)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, data, c.getIndentQuery(r))
}

func (c *appContext) ChartTypeData(w http.ResponseWriter, r *http.Request) {
	chartType := m.GetChartTypeCtx(r)
	bin := r.URL.Query().Get("bin")
	// Support the deprecated URL parameter "zoom".
	if bin == "" {
		bin = r.URL.Query().Get("zoom")
	}
	axis := r.URL.Query().Get("axis")
	chartData, err := c.charts.Chart(chartType, bin, axis)
	if err != nil {
		http.NotFound(w, r)
		log.Warnf(`Error fetching chart %s at bin level '%s': %v`, chartType, bin, err)
		return
	}
	writeJSONBytes(w, chartData)
}

// route: /market/{token}/candlestick/{bin}
func (c *appContext) getCandlestickChart(w http.ResponseWriter, r *http.Request) {
	if c.xcBot == nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	token := m.RetrieveExchangeTokenCtx(r)
	bin := m.RetrieveStickWidthCtx(r)
	if token == "" || bin == "" {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	chart, err := c.xcBot.QuickSticks(token, bin)
	if err != nil {
		apiLog.Infof("QuickSticks error: %v", err)
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	writeJSONBytes(w, chart)
}

// route: /market/{token}/depth
func (c *appContext) getDepthChart(w http.ResponseWriter, r *http.Request) {
	if c.xcBot == nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	token := m.RetrieveExchangeTokenCtx(r)
	if token == "" {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	chart, err := c.xcBot.QuickDepth(token)
	if err != nil {
		apiLog.Infof("QuickDepth error: %v", err)
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	writeJSONBytes(w, chart)
}

func (c *appContext) getAddressTransactions(w http.ResponseWriter, r *http.Request) {
	addresses, err := m.GetAddressCtx(r, c.Params, 1)
	if err != nil || len(addresses) > 1 {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	address := addresses[0]

	count := int64(m.GetNCtx(r))
	skip := int64(m.GetMCtx(r))
	if count <= 0 {
		count = 10
	} else if count > 8000 {
		count = 8000
	}
	if skip <= 0 {
		skip = 0
	}

	txs, err := c.DataSource.AddressTransactionDetails(address, count, skip, dbtypes.AddrTxnAll)
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("AddressTransactionDetails: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}

	if txs == nil || err != nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	writeJSON(w, txs, c.getIndentQuery(r))
}

func (c *appContext) getAddressTransactionsRaw(w http.ResponseWriter, r *http.Request) {
	addresses, err := m.GetAddressCtx(r, c.Params, 1)
	if err != nil || len(addresses) > 1 {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	address := addresses[0]

	count := int64(m.GetNCtx(r))
	skip := int64(m.GetMCtx(r))
	if count <= 0 {
		count = 10
	} else if count > 8000 {
		count = 8000
	}
	if skip <= 0 {
		skip = 0
	}

	// TODO: add postgresql powered method
	txs := c.DataSource.GetAddressTransactionsRawWithSkip(address, int(count), int(skip))
	if txs == nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	writeJSON(w, txs, c.getIndentQuery(r))
}

// getAgendaData processes a request for agenda chart data from /agenda/{agendaId}.
func (c *appContext) getAgendaData(w http.ResponseWriter, r *http.Request) {
	agendaId := m.GetAgendaIdCtx(r)
	if agendaId == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	chartDataByTime, err := c.DataSource.AgendaVotes(agendaId, 0)
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("AgendaVotes timeout error %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}

	chartDataByHeight, err := c.DataSource.AgendaVotes(agendaId, 1)
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("AgendaVotes timeout error: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}

	data := &apitypes.AgendaAPIResponse{
		ByHeight: chartDataByHeight,
		ByTime:   chartDataByTime,
	}

	writeJSON(w, data, "")

}

func (c *appContext) getExchanges(w http.ResponseWriter, r *http.Request) {
	if c.xcBot == nil {
		http.Error(w, "Exchange monitoring disabled.", http.StatusServiceUnavailable)
		return
	}
	// Don't provide any info if the bot is in the failed state.
	if c.xcBot.IsFailed() {
		http.Error(w, fmt.Sprintf("No exchange data available"), http.StatusNotFound)
		return
	}
	code := r.URL.Query().Get("code")
	var state *exchanges.ExchangeBotState
	var err error
	if code != "" && code != c.xcBot.BtcIndex {
		state, err = c.xcBot.ConvertedState(code)
		if err != nil {
			http.Error(w, fmt.Sprintf("No exchange data for code %s", code), http.StatusNotFound)
			return
		}
	} else {
		state = c.xcBot.State()
	}
	writeJSON(w, state, c.getIndentQuery(r))
}

func (c *appContext) getCurrencyCodes(w http.ResponseWriter, r *http.Request) {
	if c.xcBot == nil {
		http.Error(w, "Exchange monitoring disabled.", http.StatusServiceUnavailable)
		return
	}
	codes := c.xcBot.AvailableIndices()
	if len(codes) == 0 {
		http.Error(w, fmt.Sprintf("No codes found."), http.StatusNotFound)
		return
	}
	writeJSON(w, codes, c.getIndentQuery(r))
}

// getAgendasData returns high level agendas details that includes Name,
// Description, Vote Version, VotingDone height, Activated, HardForked,
// StartTime and ExpireTime.
func (c *appContext) getAgendasData(w http.ResponseWriter, _ *http.Request) {
	agendas, err := c.AgendaDB.AllAgendas()
	if err != nil {
		apiLog.Errorf("agendadb AllAgendas error: %v", err)
		http.Error(w, "agendadb.AllAgendas failed.", http.StatusServiceUnavailable)
		return
	}

	voteMilestones, err := c.DataSource.AllAgendas()
	if err != nil {
		apiLog.Errorf("AllAgendas timeout error: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
	}

	data := make([]apitypes.AgendasInfo, 0, len(agendas))
	for index := range agendas {
		val := agendas[index]
		agendaMilestone := voteMilestones[val.ID]
		agendaMilestone.StartTime = time.Unix(int64(val.StartTime), 0).UTC()
		agendaMilestone.ExpireTime = time.Unix(int64(val.ExpireTime), 0).UTC()

		data = append(data, apitypes.AgendasInfo{
			Name:        val.ID,
			Description: val.Description,
			VoteVersion: val.VoteVersion,
			MileStone:   &agendaMilestone,
			Mask:        val.Mask,
		})
	}
	writeJSON(w, data, "")
}

func (c *appContext) StakeVersionLatestCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := m.StakeVersionLatestCtx(r, c.DataSource.GetStakeVersionsLatest)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (c *appContext) BlockHashPathAndIndexCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := m.BlockHashPathAndIndexCtx(r, c.DataSource)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (c *appContext) BlockIndexLatestCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := m.BlockIndexLatestCtx(r, c.DataSource)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (c *appContext) getBlockHeightCtx(r *http.Request) (int64, error) {
	return m.GetBlockHeightCtx(r, c.DataSource)
}

func (c *appContext) getBlockHashCtx(r *http.Request) (string, error) {
	hash, err := m.GetBlockHashCtx(r)
	if err != nil {
		idx := int64(m.GetBlockIndexCtx(r))
		hash, err = c.DataSource.GetBlockHash(idx)
		if err != nil {
			apiLog.Errorf("Unable to GetBlockHash: %v", err)
			return "", err
		}
	}
	return hash, nil
}
