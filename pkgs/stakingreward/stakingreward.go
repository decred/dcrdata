package stakingreward

import (
	"io"
	"net/http"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/blockdata/v5"
	"github.com/decred/dcrdata/exchanges/v2"
	"github.com/decred/dcrdata/txhelpers/v4"
	"github.com/decred/dcrdata/v5/explorer"
	"github.com/go-chi/chi"
)

type calculator struct {
	RewardPeriod float64
	TicketReward float64
	DCRPrice     float64
	xcBot        *exchanges.ExchangeBot
	templates    *explorer.Templates
	chainParams  *chaincfg.Params
}

func Init(r *chi.Router, chainParams *chaincfg.Params, templates *explorer.Templates, xcBot *exchanges.ExchangeBot) *calculator {
	cal := &calculator{
		xcBot:       xcBot,
		templates:   templates,
		chainParams: chainParams,
	}
	r.Get("/stakingreward", cal.stakingReward)
	return cal
}

// Store implements BlockDataSaver.
func (exp *calculator) Store(blockData *blockdata.BlockData, msgBlock *wire.MsgBlock) error {
	posSubsPerVote := dcrutil.Amount(blockData.ExtraInfo.NextBlockSubsidy.PoS).ToCoin() /
		float64(exp.chainParams.TicketsPerBlock)
	exp.TicketReward = 100 * posSubsPerVote /
		blockData.CurrentStakeDiff.CurrentStakeDifficulty

	return nil
}

func (exp *calculator) stakingReward(w http.ResponseWriter, r *http.Request) {
	price := 24.42
	if exp.xcBot != nil {
		if rate := exp.xcBot.Conversion(1.0); rate != nil {
			price = rate.Value
		}
	}

	meanVotingBlocks := txhelpers.CalcMeanVotingBlocks(exp.chainParams)

	avgSSTxToSSGenMaturity := meanVotingBlocks +
		int64(exp.chainParams.TicketMaturity) +
		int64(exp.chainParams.CoinbaseMaturity)
	rewardPeriodInSecs := float64(avgSSTxToSSGenMaturity) *
		exp.chainParams.TargetTimePerBlock.Seconds()

	str, err := exp.templates.ExecTemplateToString("stakingreward", struct {
		*explorer.CommonPageData
		RewardPeriod float64
		TicketReward float64
		DCRPrice     float64
	}{
		CommonPageData: exp.commonData(r),
		DCRPrice:       price,
		RewardPeriod:   rewardPeriodInSecs,
		TicketReward:   exp.RewardPeriod,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		// exp.StatusPage(w, defaultErrorCode, defaultErrorMessage, "", ExpStatusError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func (exp *calculator) commonData(r *http.Request) *explorer.CommonPageData {
	return nil
}
