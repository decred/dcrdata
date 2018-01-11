// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package mempool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	apitypes "github.com/decred/dcrdata/api/types"
)

// NewTx models data for a new transaction
type NewTx struct {
	Hash *chainhash.Hash
	T    time.Time
}

// MempoolInfo models basic data about the node's mempool
type MempoolInfo struct {
	CurrentHeight               uint32
	NumTicketPurchasesInMempool uint32
	NumTicketsSinceStatsReport  int32
	LastCollectTime             time.Time
}

type mempoolMonitor struct {
	mpoolInfo      MempoolInfo
	newTicketLimit int32
	minInterval    time.Duration
	maxInterval    time.Duration
	collector      *mempoolDataCollector
	dataSavers     []MempoolDataSaver
	newTxHash      chan *NewTx
	quit           chan struct{}
	wg             *sync.WaitGroup
}

// NewMempoolMonitor creates a new mempoolMonitor
func NewMempoolMonitor(collector *mempoolDataCollector,
	savers []MempoolDataSaver, newTxChan chan *NewTx,
	quit chan struct{}, wg *sync.WaitGroup, newTicketLimit int32,
	mini time.Duration, maxi time.Duration, mpi *MempoolInfo) *mempoolMonitor {
	return &mempoolMonitor{
		mpoolInfo:      *mpi,
		newTicketLimit: newTicketLimit,
		minInterval:    mini,
		maxInterval:    maxi,
		collector:      collector,
		dataSavers:     savers,
		newTxHash:      newTxChan,
		quit:           quit,
		wg:             wg,
	}
}

// TicketsDetails localizes apitypes.TicketsDetails
type TicketsDetails apitypes.TicketsDetails

// Below is the implementation of sort.Interface
// { Len(), Swap(i, j int), Less(i, j int) bool }. This implementation sorts
// the structure in acending order

// Len returns the length of TicketsDetails
func (tix TicketsDetails) Len() int {
	return len(tix)
}

// Swap swaps TicketsDetails elements at i and j
func (tix TicketsDetails) Swap(i, j int) {
	tix[i], tix[j] = tix[j], tix[i]
}

// ByFeeRate models TicketsDetails sorted by fee rates
type ByFeeRate struct {
	TicketsDetails
}

// Less compares fee rates by rate_i < rate_j
func (tix ByFeeRate) Less(i, j int) bool {
	return tix.TicketsDetails[i].FeeRate < tix.TicketsDetails[j].FeeRate
}

// ByAbsoluteFee models TicketDetails sorted by fee
type ByAbsoluteFee struct {
	TicketsDetails
}

// Less compares fee rates by fee_i < fee_j
func (tix ByAbsoluteFee) Less(i, j int) bool {
	return tix.TicketsDetails[i].Fee < tix.TicketsDetails[j].Fee
}

// TxHandler receives signals from OnTxAccepted via the newTxChan, indicating
// that a new transaction has entered mempool. This function should be launched
// as a goroutine, and stopped by closing the quit channel, the broadcasting
// mechanism used by main. The newTxChan contains a chain hash for the
// transaction from the notificiation, or a zero value hash indicating it was
// from a Ticker or manually triggered.
func (p *mempoolMonitor) TxHandler(client *rpcclient.Client) {
	defer p.wg.Done()
	for {
		select {
		case s, ok := <-p.newTxHash:
			if !ok {
				log.Infof("New Tx channel closed")
				return
			}

			// A nil hash is our signal of a new block
			if s.Hash == nil {
				_ = p.CollectAndStore()
				continue
			}

			// OnTxAccepted probably sent on newTxChan
			tx, err := client.GetRawTransaction(s.Hash)
			if err != nil {
				log.Errorf("Failed to get transaction (do you have --txindex with dcrd?) %v: %v",
					s.Hash.String(), err)
				continue
			}

			// See if the transaction is a ticket purchase.  If not, just
			// make a note of it and go back to the loop.
			txType := stake.DetermineTxType(tx.MsgTx())
			//s.Tree() == dcrutil.TxTreeRegular
			// See dcrd/blockchain/stake/staketx.go for information about
			// specifications for different transaction types.

			switch txType {
			case stake.TxTypeRegular:
				// Regular Tx
				log.Tracef("Received regular transaction: %v", tx.Hash())
				continue
			case stake.TxTypeSStx:
				// Ticket purchase
				price := tx.MsgTx().TxOut[0].Value
				log.Tracef("Received ticket purchase %v, price %v",
					tx.Hash(), dcrutil.Amount(price).ToCoin())
				// txHeight = tx.MsgTx().TxIn[0].BlockHeight // uh, no
			case stake.TxTypeSSGen:
				// Vote
				ticketHash := &tx.MsgTx().TxIn[1].PreviousOutPoint.Hash
				log.Tracef("Received vote %v for ticket %v", tx.Hash(), ticketHash)
				// TODO: Show subsidy for this vote (Vout[2] - Vin[1] ?)
				continue
			case stake.TxTypeSSRtx:
				// Revoke
				log.Tracef("Received revoke transaction: %v", tx.Hash())
				continue
			default:
				// Unknown
				log.Warnf("Received other transaction: %v", tx.Hash())
				continue
			}

			// TODO: Get fee for this ticket (Vin[0] - Vout[0])

			// Decide if it is time to collect and record new data
			// 1. Get block height
			// 2. Record num new and total tickets in mp
			// 3. Collect mempool info (fee info), IF:
			//	 a. block is new (height of Ticket-Tx > currentHeight)
			//   OR
			//   b. time since last > maxInterval
			//	 OR
			//   c. (num new tickets >= newTicketLimit
			//       AND
			//       time since lastCollectTime >= minInterval)

			// Get best block height at time of transaction broadcast
			bestBlock, err := client.GetBlockCount()
			if err != nil {
				log.Error("Unable to get block count")
				continue
			}
			txHeight := uint32(bestBlock)

			newBlock := txHeight > p.mpoolInfo.CurrentHeight
			if newBlock {
				p.mpoolInfo.CurrentHeight = txHeight
			}

			// Increment new ticket count (assuming we only get here via a sstx)
			p.mpoolInfo.NumTicketsSinceStatsReport++
			enoughNewTickets := p.mpoolInfo.NumTicketsSinceStatsReport >= p.newTicketLimit

			// See how long it has been since we last collected mempool data
			timeSinceLast := time.Since(p.mpoolInfo.LastCollectTime)
			quiteLong := timeSinceLast > p.maxInterval
			longEnough := timeSinceLast >= p.minInterval

			if newBlock || quiteLong || (enoughNewTickets && longEnough) {
				// Do not bother collecting if this transaction came in before
				// last collection, in which case we'd have it already.
				if time.Since(s.T) > timeSinceLast {
					log.Debugf("A Tx was queued before last mempool collection, which would have included it. SKIPPING.")
					continue
				}

				if err = p.CollectAndStore(); err != nil {
					continue
				}
			}

		case <-p.quit:
			log.Debugf("Quitting OnTxAccepted (new tx in mempool) handler.")
			return
		}
	}
}

// CollectAndStore collects mempool data, resets counters ticket counters and
// the timer, and dispatches the storers.
func (p *mempoolMonitor) CollectAndStore() error {
	// reset counter for tickets since last report
	newTickets := p.mpoolInfo.NumTicketsSinceStatsReport
	p.mpoolInfo.NumTicketsSinceStatsReport = 0
	// and timer
	p.mpoolInfo.LastCollectTime = time.Now()
	// Collect mempool data (currently ticket fees)
	log.Trace("Gathering new mempool data.")
	data, err := p.collector.Collect()
	if err != nil {
		log.Errorf("mempool data collection failed: %v", err.Error())
		// data is nil when err != nil
		return err
	}

	// Insert new ticket counter into data structure
	data.NewTickets = uint32(newTickets)

	p.mpoolInfo.NumTicketPurchasesInMempool = data.Ticketfees.FeeInfoMempool.Number

	// Store mempool data with each saver
	timestamp := time.Now()
	for _, s := range p.dataSavers {
		if s != nil {
			log.Trace("Saving MP data.")
			// save data to wherever the saver wants to put it
			go s.StoreMPData(data, timestamp)
		}
	}

	return nil
}

// COLLECTOR

// MinableFeeInfo describes the ticket fees
type MinableFeeInfo struct {
	// All fees in mempool
	allFees     []float64
	allFeeRates []float64
	// The index of the 20th largest fee, or largest if number in mempool < 20
	lowestMineableIdx int
	// The corresponding fee (i.e. all[lowestMineableIdx])
	lowestMineableFee float64
	// A window of fees about lowestMineableIdx
	targetFeeWindow []float64
}

// Stakelimitfeeinfo JSON output
type Stakelimitfeeinfo struct {
	Stakelimitfee float64 `json:"stakelimitfee"`
	// others...
}

// MempoolData models info about ticket purchases in mempool
type MempoolData struct {
	Height            uint32
	NumTickets        uint32
	NumVotes          uint32
	NewTickets        uint32
	Ticketfees        *dcrjson.TicketFeeInfoResult
	MinableFees       *MinableFeeInfo
	AllTicketsDetails TicketsDetails
}

// GetHeight returns the mempool height
func (m *MempoolData) GetHeight() uint32 {
	return m.Height
}

// GetNumTickets returns number of tickets
func (m *MempoolData) GetNumTickets() uint32 {
	return m.NumTickets
}

type mempoolDataCollector struct {
	mtx          sync.Mutex
	dcrdChainSvr *rpcclient.Client
	activeChain  *chaincfg.Params
}

// NewMempoolDataCollector creates a new mempoolDataCollector.
func NewMempoolDataCollector(dcrdChainSvr *rpcclient.Client, params *chaincfg.Params) *mempoolDataCollector {
	return &mempoolDataCollector{
		mtx:          sync.Mutex{},
		dcrdChainSvr: dcrdChainSvr,
		activeChain:  params,
	}
}

// Collect is the main handler for collecting chain data
func (t *mempoolDataCollector) Collect() (*MempoolData, error) {
	// In case of a very fast block, make sure previous call to collect is not
	// still running, or dcrd may be mad.
	t.mtx.Lock()
	defer t.mtx.Unlock()

	// Time this function
	defer func(start time.Time) {
		log.Debugf("mempoolDataCollector.Collect() completed in %v",
			time.Since(start))
	}(time.Now())

	// client
	c := t.dcrdChainSvr

	// Get a map of ticket hashes to getrawmempool results
	// mempoolTickets[ticketHashes[0].String()].Fee
	mempoolTickets, err := c.GetRawMempoolVerbose(dcrjson.GRMTickets)
	if err != nil {
		return nil, err
	}

	mempoolVotes, err := c.GetRawMempoolVerbose(dcrjson.GRMVotes)
	if err != nil {
		return nil, err
	}
	numVotes := len(mempoolVotes)

	// Fee info
	var numFeeWindows, numFeeBlocks uint32 = 0, 0
	feeInfo, err := c.TicketFeeInfo(&numFeeBlocks, &numFeeWindows)
	if err != nil {
		return nil, err
	}

	// Make slice of TicketDetails
	N := len(mempoolTickets)
	allTicketsDetails := make(TicketsDetails, 0, N)
	for hash, t := range mempoolTickets {
		//ageSec := time.Since(time.Unix(t.Time, 0)).Seconds()
		// Compute fee in DCR / kB
		feeRate := t.Fee / float64(t.Size) * 1000
		allTicketsDetails = append(allTicketsDetails, &apitypes.TicketDetails{
			Hash:    hash,
			Fee:     t.Fee,
			FeeRate: feeRate,
			Size:    t.Size,
			Height:  t.Height,
		})
	}
	// Verify we get the correct median result
	//medianFee := MedianCoin(allFeeRates)
	//log.Infof("Median fee computed: %v (%v)", medianFee, N)

	sort.Sort(ByAbsoluteFee{allTicketsDetails})
	allFees := make([]float64, 0, N)
	for _, td := range allTicketsDetails {
		allFees = append(allFees, td.Fee)
	}
	sort.Sort(ByFeeRate{allTicketsDetails})
	allFeeRates := make([]float64, 0, N)
	for _, td := range allTicketsDetails {
		allFeeRates = append(allFeeRates, td.FeeRate)
	}

	// 20 tickets purchases may be mined per block
	Nmax := int(t.activeChain.MaxFreshStakePerBlock)
	//sort.Float64s(allFeeRates)
	var lowestMineableFee float64
	// If no tickets, no valid index
	var lowestMineableIdx = -1
	if N >= Nmax {
		lowestMineableIdx = N - Nmax
		lowestMineableFee = allFeeRates[lowestMineableIdx]
	} else if N != 0 {
		lowestMineableIdx = 0
		lowestMineableFee = allFeeRates[0]
	}

	// Extract the fees for a window about the mileability threshold
	var targetFeeWindow []float64
	if N > 0 {
		// Summary output has it's own radius, but here we hard-code
		const feeRad int = 5

		lowEnd := lowestMineableIdx - feeRad
		if lowEnd < 0 {
			lowEnd = 0
		}

		// highEnd is the exclusive end of the half-open range (+1)
		highEnd := lowestMineableIdx + feeRad + 1
		if highEnd > N {
			highEnd = N
		}

		targetFeeWindow = allFeeRates[lowEnd:highEnd]
	}

	mineables := &MinableFeeInfo{
		allFees,
		allFeeRates,
		lowestMineableIdx,
		lowestMineableFee,
		targetFeeWindow,
	}

	height, err := c.GetBlockCount()

	mpoolData := &MempoolData{
		Height:            uint32(height),
		NumTickets:        feeInfo.FeeInfoMempool.Number,
		Ticketfees:        feeInfo,
		MinableFees:       mineables,
		NumVotes:          uint32(numVotes),
		AllTicketsDetails: allTicketsDetails,
	}

	return mpoolData, err
}

// SAVER

// MempoolDataSaver is an interface for saving/storing MempoolData
type MempoolDataSaver interface {
	StoreMPData(data *MempoolData, timestamp time.Time) error
}

// MempoolDataToJSONStdOut implements MempoolDataSaver interface for JSON output to
// stdout
type MempoolDataToJSONStdOut struct {
	mtx *sync.Mutex
}

// MempoolDataToSummaryStdOut implements MempoolDataSaver interface for plain text
// summary to stdout
type MempoolDataToSummaryStdOut struct {
	mtx             *sync.Mutex
	feeWindowRadius int
}

type fileSaver struct {
	folder   string
	nameBase string
	file     os.File
	mtx      *sync.Mutex
}

// MempoolDataToJSONFiles implements MempoolDataSaver interface for JSON output to
// the file system
type MempoolDataToJSONFiles struct {
	fileSaver
}

// MempoolFeeDumper implements MempoolDataSaver interface for a complete file
// dump of all ticket fees to the file system
type MempoolFeeDumper struct {
	fileSaver
}

// MempoolDataToMySQL implements MempoolDataSaver interface for output to a
// MySQL database
// type MempoolDataToMySQL struct {
// 	mtx *sync.Mutex
// }

// NewMempoolDataToJSONStdOut creates a new MempoolDataToJSONStdOut with optional
// existing mutex
func NewMempoolDataToJSONStdOut(m ...*sync.Mutex) *MempoolDataToJSONStdOut {
	if len(m) > 1 {
		panic("Too many inputs.")
	}
	if len(m) > 0 {
		return &MempoolDataToJSONStdOut{m[0]}
	}
	return &MempoolDataToJSONStdOut{}
}

// NewMempoolDataToSummaryStdOut creates a new MempoolDataToSummaryStdOut with optional
// existing mutex
func NewMempoolDataToSummaryStdOut(feeWindowRadius int, m ...*sync.Mutex) *MempoolDataToSummaryStdOut {
	if len(m) > 1 {
		panic("Too many inputs.")
	}
	if len(m) > 0 {
		return &MempoolDataToSummaryStdOut{m[0], feeWindowRadius}
	}
	return &MempoolDataToSummaryStdOut{nil, feeWindowRadius}
}

// NewMempoolFeeDumper creates a new MempoolFeeDumper with optional
// existing mutex
func NewMempoolFeeDumper(folder string, fileBase string, m ...*sync.Mutex) *MempoolFeeDumper {
	if len(m) > 1 {
		panic("Too many inputs.")
	}

	var mtx *sync.Mutex
	if len(m) > 0 {
		mtx = m[0]
	} else {
		mtx = new(sync.Mutex)
	}

	return &MempoolFeeDumper{
		fileSaver: fileSaver{
			folder:   folder,
			nameBase: fileBase,
			file:     os.File{},
			mtx:      mtx,
		},
	}
}

// NewMempoolDataToJSONFiles creates a new MempoolDataToJSONFiles with optional
// existing mutex
func NewMempoolDataToJSONFiles(folder string, fileBase string,
	m ...*sync.Mutex) *MempoolDataToJSONFiles {
	if len(m) > 1 {
		panic("Too many inputs.")
	}

	var mtx *sync.Mutex
	if len(m) > 0 {
		mtx = m[0]
	} else {
		mtx = new(sync.Mutex)
	}

	return &MempoolDataToJSONFiles{
		fileSaver: fileSaver{
			folder:   folder,
			nameBase: fileBase,
			file:     os.File{},
			mtx:      mtx,
		},
	}
}

// StoreMPData writes MempoolData to stdout in JSON format
func (s *MempoolDataToJSONStdOut) StoreMPData(data *MempoolData) error {
	// Do not write JSON data if there are no new tickets since last report
	if data.NewTickets == 0 {
		return nil
	}

	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}

	// Marshall all the block data results in to a single JSON object, indented
	jsonConcat, err := JSONFormatMempoolData(data)
	if err != nil {
		return err
	}

	// Write JSON to stdout with guards to delimit the object from other text
	fmt.Printf("\n--- BEGIN MempoolData JSON ---\n")
	_, err = writeFormattedJSONMempoolData(jsonConcat, os.Stdout)
	fmt.Printf("--- END MempoolData JSON ---\n\n")
	if err != nil {
		log.Error("Write JSON mempool data to stdout pipe: ", os.Stdout)
	}

	return err
}

// StoreMPData writes MempoolData to stdout as plain text summary
func (s *MempoolDataToSummaryStdOut) StoreMPData(data *MempoolData) error {
	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}

	mempoolTicketFees := data.Ticketfees.FeeInfoMempool

	// time.Now().UTC().Format(time.UnixDate)
	_, err := fmt.Printf("%v - Mempool ticket fees (%v):  %.5f, %.4f, %.4f, %.4f (l/m, mean, median, std), n=%d\n",
		time.Now().Format("2006-01-02 15:04:05.00 -0700 MST"), data.Height,
		data.MinableFees.lowestMineableFee,
		mempoolTicketFees.Mean, mempoolTicketFees.Median,
		mempoolTicketFees.StdDev, mempoolTicketFees.Number)

	// Inspect a range of ticket fees in the sorted list, about the 20th
	// largest or the largest if less than 20 tickets in mempool.
	boundIdx := data.MinableFees.lowestMineableIdx
	N := len(data.MinableFees.allFees)

	if N < 2 {
		return err
	}

	// slices referencing the segments above and below the threshold
	var upperFees, lowerFees []float64
	// distance input from configuration
	w := s.feeWindowRadius

	if w < 1 {
		return err
	}

	lowEnd := boundIdx - w
	if lowEnd < 0 {
		lowEnd = 0
	}
	highEnd := boundIdx + w + 1 // +1 for slice indexing
	if highEnd > N {
		highEnd = N
	}

	// center value not included in upper/lower windows
	lowerFees = data.MinableFees.allFees[lowEnd:boundIdx]
	upperFees = data.MinableFees.allFees[boundIdx+1 : highEnd]

	_, err = fmt.Printf("Mineable tickets, limit -%d/+%d:\t%.5f --> %.5f (threshold) --> %.5f\n",
		len(lowerFees), len(upperFees), lowerFees,
		data.MinableFees.lowestMineableFee, upperFees)

	return err
}

// StoreMPData writes MempoolData to a file in JSON format
// The file name is nameBase+height+".json".
func (s *MempoolDataToJSONFiles) StoreMPData(data *MempoolData) error {
	// Do not write JSON data if there are no new tickets since last report
	if data.NewTickets == 0 {
		return nil
	}

	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}

	// Marshall all the block data results in to a single JSON object, indented
	jsonConcat, err := JSONFormatMempoolData(data)
	if err != nil {
		return err
	}

	// Write JSON to a file with block height in the name
	fname := fmt.Sprintf("%s%d-%d.json", s.nameBase, data.Height,
		data.NumTickets)
	fullfile := filepath.Join(s.folder, fname)
	fp, err := os.Create(fullfile)
	if err != nil {
		log.Errorf("Unable to open file %v for writing.", fullfile)
		return err
	}
	defer fp.Close()

	s.file = *fp
	_, err = writeFormattedJSONMempoolData(jsonConcat, &s.file)
	if err != nil {
		log.Error("Write JSON mempool data to file: ", *fp)
	}

	return err
}

// StoreMPData writes all the ticket fees to a file
// The file name is nameBase+".json".
func (s *MempoolFeeDumper) StoreMPData(data *MempoolData, timestamp time.Time) error {
	// Do not write JSON data if there are no new tickets since last report
	// if data.newTickets == 0 {
	// 	return nil
	// }

	if s.mtx != nil {
		s.mtx.Lock()
		defer s.mtx.Unlock()
	}

	// Write fees to a file with block height in the name
	fname := fmt.Sprintf("%s-%d-%d-%d.json", s.nameBase, data.Height,
		data.NumTickets, time.Now().Unix())
	//fname := fmt.Sprintf("%s.json", s.nameBase)
	fullfile := filepath.Join(s.folder, fname)
	fp, err := os.Create(fullfile)
	if err != nil {
		log.Errorf("Unable to open file %v for writing.", fullfile)
		return err
	}
	defer fp.Close()

	j, err := json.MarshalIndent(struct {
		N        int       `json:"n"`
		Time     int64     `json:"time"`
		AllFees  []float64 `json:"allfees"`
		DateTime string    `json:"datetime"`
	}{
		len(data.MinableFees.allFees),
		timestamp.Unix(),
		data.MinableFees.allFees,
		time.Now().UTC().Format(time.RFC822)},
		"", "    ")

	if err != nil {
		log.Errorf("Failed to Marshal mempool data: %v", err)
		return err
	}

	s.file = *fp
	_, err = fmt.Fprintln(&s.file, string(j))
	if err != nil {
		log.Error("Write mempool ticket fees data to file: ", *fp)
	}
	log.Debugf("All fees written to %s.", fname)

	return err
}

func writeFormattedJSONMempoolData(jsonConcat *bytes.Buffer, w io.Writer) (int, error) {
	n, err := fmt.Fprintln(w, jsonConcat.String())
	// there was once more, perhaps again.
	return n, err
}

// JSONFormatMempoolData concatenates block data results into a single JSON
// object with primary keys for the result type
func JSONFormatMempoolData(data *MempoolData) (*bytes.Buffer, error) {
	var jsonAll bytes.Buffer

	jsonAll.WriteString("{\"ticketfeeinfo_mempool\": ")
	feeInfoMempoolJSON, err := json.Marshal(data.Ticketfees.FeeInfoMempool)
	if err != nil {
		log.Error("Unable to marshall mempool ticketfee info to JSON: ",
			err.Error())
		return nil, err
	}
	jsonAll.Write(feeInfoMempoolJSON)
	//feeInfoMempoolJSON, err := json.MarshalIndent(data.ticketfees.FeeInfoMempool, "", "    ")
	//fmt.Println(string(feeInfoMempoolJSON))

	limitinfo := Stakelimitfeeinfo{data.MinableFees.lowestMineableFee}

	jsonAll.WriteString(",\"stakelimitfee\": ")
	limitInfoJSON, err := json.Marshal(limitinfo)
	if err != nil {
		log.Error("Unable to marshall mempool stake limit info to JSON: ",
			err.Error())
		return nil, err
	}
	jsonAll.Write(limitInfoJSON)

	jsonAll.WriteString("}")

	var jsonAllIndented bytes.Buffer
	err = json.Indent(&jsonAllIndented, jsonAll.Bytes(), "", "    ")
	if err != nil {
		log.Error("Unable to format JSON mempool data: ", err.Error())
		return nil, err
	}

	return &jsonAllIndented, err
}
