package dcrdataapi

import (
	"github.com/decred/dcrd/dcrjson"
)

// import (
// 	"github.com/decred/dcrd/dcrjson"
// )

// much of the time, dcrdata will be using the types in dcrjson, but others are
// defined here

type Status struct {
	Ready  bool   `json:"ready"`
	Height uint32 `json:"height"`
}

// TicketPoolInfo models data about ticket pool
type TicketPoolInfo struct {
	Size   uint32  `json:"size"`
	Value  float64 `json:"value"`
	ValAvg float64 `json:"valavg"`
}

type BlockDataBasic struct {
	Height     uint32  `json:"height"`
	Size       uint32  `json:"size"`
	Hash       string  `json:"hash"`
	Difficulty float64 `json:"diff"`
	StakeDiff  float64 `json:"sdiff"`
	Time       int64   `json:"time"`
	//TicketPoolInfo
	PoolInfo TicketPoolInfo `json:"ticket_pool"`
}

type StakeDiff struct {
	dcrjson.GetStakeDifficultyResult
	Estimates dcrjson.EstimateStakeDiffResult `json:"estimates"`
}

type StakeInfoExtended struct {
	Feeinfo          dcrjson.FeeInfoBlock `json:"feeinfo"`
	StakeDiff        float64              `json:"stakediff"`
	PriceWindowNum   int                  `json:"window_number"`
	IdxBlockInWindow int                  `json:"window_block_index"`
	PoolInfo         TicketPoolInfo       `json:"ticket_pool"`
}

type StakeInfoExtendedEstimates struct {
	Feeinfo          dcrjson.FeeInfoBlock `json:"feeinfo"`
	StakeDiff        StakeDiff            `json:"stakediff"`
	PriceWindowNum   int                  `json:"window_number"`
	IdxBlockInWindow int                  `json:"window_block_index"`
	PoolInfo         TicketPoolInfo       `json:"ticket_pool"`
}

type MempoolTicketFeeInfo struct {
	Height uint32 `json:"height"`
	dcrjson.FeeInfoMempool
	LowestMineable float64 `json:"lowest_mineable"`
}

type MempoolTicketFees struct {
	Height   uint32    `json:"height"`
	Length   uint32    `json:"length"`
	Total    uint32    `json:"total"`
	FeeRates []float64 `json:"top_fees"`
}

type TicketDetails struct {
	Hash    string  `json:"hash"`
	Fee     float64 `json:"abs_fee"`
	FeeRate float64 `json:"fee"`
	Size    int32   `json:"size"`
	Height  int64   `json:"height_received"`
}

type MempoolTicketDetails struct {
	Height  uint32          `json:"height"`
	Length  uint32          `json:"length"`
	Total   uint32          `json:"total"`
	Tickets []TicketDetails `json:"tickets"`
}
