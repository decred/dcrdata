// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package agendas

import (
	"fmt"
	"sync"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/wire"
)

const (
	statusDefined = "defined"
	statusStarted = "started"
	statusLocked  = "lockedin"
	statusActive  = "active"
	statusFailed  = "failed"
	choiceYes     = "yes"
	choiceNo      = "no"
	choiceAbstain = "abstain"
)

// VoteDataSource is satisfied by rpcclient.Client.
type VoteDataSource interface {
	GetStakeVersionInfo(int32) (*dcrjson.GetStakeVersionInfoResult, error)
	GetVoteInfo(uint32) (*dcrjson.GetVoteInfoResult, error)
	GetStakeVersions(string, int32) (*dcrjson.GetStakeVersionsResult, error)
}

// dcrd does not supply vote counts for completed votes, so the tracker will
// need a means to get the counts from a database somewhere.
type voteCounter func(string) (uint32, uint32, uint32, error)

// AgendaSummary summarizes the current state of voting on a particular agenda.
type AgendaSummary struct {
	Description     string  `json:"description"`
	ID              string  `json:"id"`
	Quorum          uint32  `json:"quorum"`
	QuorumProgress  float32 `json:"quorum_progress"`
	QuorumAchieved  bool    `json:"quorum_achieved"`
	Aye             uint32  `json:"aye"`
	Nay             uint32  `json:"nay"`
	Abstain         uint32  `json:"abstain"`
	AbstainRate     float32 `json:"abstain_rate"`
	VoteCount       uint32  `json:"vote_count"` // non-abstaining
	PassThreshold   float32 `json:"pass_threshold"`
	FailThreshold   float32 `json:"fail_threshold"`
	Approval        float32 `json:"approval"`
	LockCount       uint32  `json:"lock_count"`
	IsWinning       bool    `json:"is_winning"`
	IsLosing        bool    `json:"is_losing"`
	IsVoting        bool    `json:"is_voting"`
	IsDefined       bool    `json:"is_defined"`
	VotingTriggered bool    `json:"voting_triggered"`
	IsLocked        bool    `json:"is_locked"`
	IsFailed        bool    `json:"is_failed"`
	IsActive        bool    `json:"is_active"`
}

// VoteSummary summarizes the current state of consensus voting. VoteSummary is
// the primary exported type produced by VoteTracker.
type VoteSummary struct {
	Version         uint32          `json:"version"`
	Height          int64           `json:"height"`
	Hash            string          `json:"hash"`
	VoteVersion     uint32          `json:"vote_version"`
	Agendas         []AgendaSummary `json:"agendas"`
	OldVoters       uint32          `json:"old_voters"`
	NewVoters       uint32          `json:"new_voters"`
	VoterCount      uint32          `json:"voter_count"`
	VoterThreshold  float32         `json:"voter_threshold"`
	VoterProgress   float32         `json:"voter_progress"`
	OldMiners       uint32          `json:"old_miners"`
	NewMiners       uint32          `json:"new_miners"`
	MinerCount      uint32          `json:"miner_count"`
	MinerThreshold  float32         `json:"miner_threshold"`
	MinerProgress   float32         `json:"miner_progress"`
	RCIBlocks       uint32          `json:"rci_blocks"`
	RCIMined        uint32          `json:"rci_mined"`
	RCIProgress     float32         `json:"rci_progress"`
	SVIBlocks       uint32          `json:"svi_blocks"`
	SVIMined        uint32          `json:"svi_mined"`
	SVIProgress     float32         `json:"svi_progress"`
	TilNextRCI      int64           `json:"til_next_rci"`
	NextRCIHeight   uint32          `json:"next_rci_height"`
	NetworkUpgraded bool            `json:"network_upgrading"`
	VotingTriggered bool            `json:"voting_triggered"`
}

// Store counts fetched usin the voteCounter to prevent extra database calls.
// The counts are only required for votes that have complete, so the numbers
// will not change.
type voteCount struct {
	yes     uint32
	no      uint32
	abstain uint32
}

// VoteTracker manages the current state of node version data and vote data on
// the blockchain. VoteTracker refreshes its data when it is signaled by
// a call to Refresh. A VoteSummary is created and stored for requests with the
// Summary method.
type VoteTracker struct {
	mtx            sync.RWMutex
	node           VoteDataSource
	voteCounter    voteCounter
	countCache     map[string]*voteCount
	params         *chaincfg.Params
	version        uint32
	blockVersion   int32
	stakeVersion   uint32
	stakeIntervals *dcrjson.GetStakeVersionInfoResult
	voteInfo       *dcrjson.GetVoteInfoResult
	summary        *VoteSummary
	ringIndex      int
	ringHeight     int64
	blockRing      []int32
	minerThreshold float32
	voterThreshold float32
	sviBlocks      uint32
	rciBlocks      uint32
	blockTime      int64
	passThreshold  float32
	lockCount      uint32
}

// NewVoteTracker is a constructor for a VoteTracker.
func NewVoteTracker(params *chaincfg.Params, node VoteDataSource, counter voteCounter) (*VoteTracker, error) {
	tracker := &VoteTracker{
		mtx:            sync.RWMutex{},
		node:           node,
		voteCounter:    counter,
		countCache:     make(map[string]*voteCount),
		params:         params,
		version:        wire.ProtocolVersion,
		ringIndex:      -1,
		blockRing:      make([]int32, params.BlockUpgradeNumToCheck),
		minerThreshold: float32(params.BlockRejectNumRequired) / float32(params.BlockUpgradeNumToCheck),
		voterThreshold: float32(params.RuleChangeActivationMultiplier) / float32(params.RuleChangeActivationDivisor),
		sviBlocks:      uint32(params.StakeVersionInterval),
		rciBlocks:      params.RuleChangeActivationInterval,
		blockTime:      int64(params.TargetTimePerBlock.Seconds()),
		passThreshold:  float32(params.RuleChangeActivationMultiplier) / float32(params.RuleChangeActivationDivisor),
		lockCount:      params.RuleChangeActivationInterval * uint32(params.TicketsPerBlock) * params.RuleChangeActivationMultiplier / params.RuleChangeActivationDivisor,
	}

	voteInfo, err := tracker.refreshRCI()
	if err != nil {
		return nil, err
	}
	blocksToAdd, stakeVersion, err := tracker.fetchBlocks(voteInfo)
	if err != nil {
		return nil, err
	}
	stakeInfo, err := tracker.refreshSVIs(voteInfo)
	if err != nil {
		return nil, err
	}
	tracker.update(voteInfo, blocksToAdd, stakeInfo, stakeVersion)
	return tracker, nil
}

// Refresh refreshes node version and vote data. It can be called as a
// goroutine. All VoteTracker updating and mutex locking is handled within
// VoteTracker.update.
func (tracker *VoteTracker) Refresh() {
	voteInfo, err := tracker.refreshRCI()
	if err != nil {
		log.Errorf("VoteTracker.Refresh -> refreshRCI: %v")
		return
	}
	blocksToAdd, stakeVersion, err := tracker.fetchBlocks(voteInfo)
	if err != nil {
		log.Errorf("VoteTracker.Refresh -> fetchBlocks: %v")
		return
	}
	stakeInfo, err := tracker.refreshSVIs(voteInfo)
	if err != nil {
		log.Errorf("VoteTracker.Refresh -> refreshSVIs: %v")
		return
	}
	tracker.update(voteInfo, blocksToAdd, stakeInfo, stakeVersion)
}

// Since versoion could technically be updated without turning off dcrdata,
// the field must be protected.
func (tracker *VoteTracker) Version() uint32 {
	tracker.mtx.RLock()
	defer tracker.mtx.RUnlock()
	return tracker.version
}

// Grab the getvoteinfo data. Instead of updating the tracker's voteInfo field,
// return, return the data. voteInfo will be updated in VoteTracker.update
// along with other fileds under mutex lock.
func (tracker *VoteTracker) refreshRCI() (*dcrjson.GetVoteInfoResult, error) {
	oldVersion := tracker.Version()
	v := oldVersion
	var err error
	var voteInfo *dcrjson.GetVoteInfoResult

	for {
		vinfo, err := tracker.node.GetVoteInfo(v)
		if err != nil {
			break
		}
		voteInfo = vinfo
		v++
	}

	if voteInfo == nil {
		return nil, fmt.Errorf("Vote information not found: %v", err)
	}
	if v > oldVersion+1 {
		tracker.mtx.Lock()
		tracker.version = v
		tracker.mtx.Unlock()
	}
	return voteInfo, nil
}

// The number of blocks that have been mined in the rule change interval.
func rciBlocks(voteInfo *dcrjson.GetVoteInfoResult) int64 {
	return voteInfo.CurrentHeight - voteInfo.StartHeight + 1
}

// Grab the block versions for up to the last BlockUpgradeNumToCheck blocks.
// If the current block builds upon the last block, only request a single
// block's data. Otherwise, request all BlockUpgradeNumToCheck.
func (tracker *VoteTracker) fetchBlocks(voteInfo *dcrjson.GetVoteInfoResult) ([]int32, uint32, error) {
	blocksToRequest := 1
	// If this isn't the next block, request them all again
	if voteInfo.CurrentHeight < 0 || voteInfo.CurrentHeight != tracker.ringHeight+1 {
		blocksToRequest = int(tracker.params.BlockUpgradeNumToCheck)
	}
	r, err := tracker.node.GetStakeVersions(voteInfo.Hash, int32(blocksToRequest))
	if err != nil {
		return nil, 0, err
	}
	blockCount := len(r.StakeVersions)
	if blockCount != blocksToRequest {
		return nil, 0, fmt.Errorf("Unexpected number of blocks returns from GetStakeVersions. Asked for %d, received %d", blockCount, blockCount)
	}
	blocks := make([]int32, blockCount)
	var block dcrjson.StakeVersions
	for i := 0; i < blockCount; i++ {
		block = r.StakeVersions[blockCount-i-1] // iterate backwards
		tracker.ringIndex = (tracker.ringIndex + 1) % blockCount
		blocks[i] = block.BlockVersion
	}
	return blocks, block.StakeVersion, nil
}

// Get the info for the stake versions in the current rule change interval.
func (tracker *VoteTracker) refreshSVIs(voteInfo *dcrjson.GetVoteInfoResult) (*dcrjson.GetStakeVersionInfoResult, error) {
	blocksInCurrentRCI := rciBlocks(voteInfo)
	svis := int32(blocksInCurrentRCI / tracker.params.StakeVersionInterval)
	// blocksInCurrentSVI := int32(blocksInCurrentRCI % params.StakeVersionInterval)
	if blocksInCurrentRCI%tracker.params.StakeVersionInterval > 0 {
		svis++
	}
	si, err := tracker.node.GetStakeVersionInfo(svis)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving stake version info: %v", err)
	}
	return si, nil
}

// The cached voteCount for the given agenda, or nil if not found.
func (tracker *VoteTracker) cachedCounts(agendaId string) *voteCount {
	tracker.mtx.RLock()
	defer tracker.mtx.RUnlock()
	counts := tracker.countCache[agendaId]
	return counts
}

// Cache the voteCount for the given agenda.
func (tracker *VoteTracker) cacheVoteCounts(agendaId string, counts *voteCount) {
	tracker.mtx.Lock()
	defer tracker.mtx.Unlock()
	tracker.countCache[agendaId] = counts
}

// Once all resources have been retrieved from dcrd, update VoteTracker fields.
func (tracker *VoteTracker) update(voteInfo *dcrjson.GetVoteInfoResult, blocks []int32,
	stakeInfo *dcrjson.GetStakeVersionInfoResult, stakeVersion uint32) {
	// Check if voteCounts are needed
	for idx := range voteInfo.Agendas {
		agenda := &voteInfo.Agendas[idx]
		status := agenda.Status
		if status != statusDefined && status != statusStarted {
			// check the cache
			counts := tracker.countCache[agenda.ID]
			if counts == nil {
				counts = new(voteCount)
				var err error
				counts.yes, counts.abstain, counts.no, err = tracker.voteCounter(agenda.ID)
				if err != nil {
					log.Errorf("Error counting votes for %s", agenda.ID)
					continue
				}
				tracker.cacheVoteCounts(agenda.ID, counts)
			}
			for idx := range agenda.Choices {
				choice := &agenda.Choices[idx]
				if choice.ID == choiceYes {
					choice.Count = counts.yes
				} else if choice.ID == choiceNo {
					choice.Count = counts.no
				} else {
					choice.Count = counts.abstain
				}
			}
		}
	}
	tracker.mtx.Lock()
	defer tracker.mtx.Unlock()
	tracker.voteInfo = voteInfo
	tracker.stakeIntervals = stakeInfo
	ringLen := int(tracker.params.BlockUpgradeNumToCheck)
	for idx := range blocks {
		tracker.ringIndex = (tracker.ringIndex + 1) % ringLen
		tracker.blockRing[tracker.ringIndex] = blocks[idx]
	}
	tracker.blockVersion = tracker.blockRing[tracker.ringIndex]
	tracker.stakeVersion = stakeVersion
	tracker.ringHeight = voteInfo.CurrentHeight
	tracker.summary = tracker.newVoteSummary()
}

// Create a new VoteSummary from the currently saved info.
func (tracker *VoteTracker) newVoteSummary() *VoteSummary {
	summary := &VoteSummary{
		Version:         tracker.version,
		Height:          tracker.voteInfo.CurrentHeight,
		Hash:            tracker.voteInfo.Hash,
		VoteVersion:     tracker.version,
		MinerThreshold:  tracker.minerThreshold,
		VoterThreshold:  tracker.voterThreshold,
		RCIBlocks:       tracker.rciBlocks,
		SVIBlocks:       tracker.sviBlocks,
		NextRCIHeight:   uint32(tracker.voteInfo.EndHeight + 1),
		NetworkUpgraded: uint32(tracker.blockVersion) == tracker.version && tracker.stakeVersion == tracker.version,
	}
	summary.Agendas = make([]AgendaSummary, len(tracker.voteInfo.Agendas))
	for idx := range tracker.voteInfo.Agendas {
		agenda := &tracker.voteInfo.Agendas[idx]
		agendaSummary := AgendaSummary{
			Description:   agenda.Description,
			ID:            agenda.ID,
			Quorum:        tracker.params.RuleChangeActivationQuorum,
			PassThreshold: tracker.passThreshold,
			LockCount:     tracker.lockCount,
		}
		status := agenda.Status
		agendaSummary.IsLocked = status == statusLocked
		agendaSummary.IsFailed = status == statusFailed
		agendaSummary.IsActive = status == statusActive
		agendaSummary.IsVoting = status == statusStarted
		agendaSummary.IsDefined = status == statusDefined
		for idx := range agenda.Choices {
			choice := &agenda.Choices[idx]
			if choice.IsNo {
				agendaSummary.Nay = choice.Count
			} else if choice.IsAbstain {
				agendaSummary.Abstain = choice.Count
			} else {
				agendaSummary.Aye = choice.Count
			}
		}
		agendaSummary.VoteCount = agendaSummary.Aye + agendaSummary.Nay
		agendaSummary.Approval = float32(agendaSummary.Aye) / float32(agendaSummary.VoteCount)
		agendaSummary.AbstainRate = float32(agendaSummary.Abstain) / float32(agendaSummary.VoteCount+agendaSummary.Abstain)
		agendaSummary.QuorumProgress = float32(agendaSummary.VoteCount) / float32(agendaSummary.Quorum)
		agendaSummary.FailThreshold = 1 - agendaSummary.PassThreshold
		agendaSummary.QuorumAchieved = agendaSummary.VoteCount > agendaSummary.Quorum
		if agendaSummary.QuorumProgress >= 1 {
			agendaSummary.QuorumProgress = 1
			agendaSummary.QuorumAchieved = true
		}
		if agendaSummary.Aye >= agendaSummary.LockCount {
			agendaSummary.IsLocked = true
		}
		if agendaSummary.Approval >= agendaSummary.PassThreshold {
			agendaSummary.IsWinning = true
		}
		if agendaSummary.Approval < agendaSummary.FailThreshold {
			agendaSummary.IsLosing = true
		}
		if agendaSummary.IsDefined && summary.NetworkUpgraded {
			agendaSummary.VotingTriggered = true
			summary.VotingTriggered = true
		}
		summary.Agendas[idx] = agendaSummary
	}
	var sviMined uint32
	for idx := range tracker.stakeIntervals.Intervals {
		interval := tracker.stakeIntervals.Intervals[idx]
		var newVoters, oldVoters uint32
		for idy := range interval.VoteVersions {
			version := &interval.VoteVersions[idy]
			if version.Version == tracker.version {
				newVoters = version.Count
			} else {
				oldVoters += version.Count
			}
		}
		if idx == 0 {
			sviMined = uint32(summary.Height - interval.StartHeight + 1)
			summary.NewVoters = newVoters
			summary.OldVoters = oldVoters
		}
	}
	summary.VoterCount = summary.OldVoters + summary.NewVoters
	summary.VoterProgress = float32(summary.NewVoters) / float32(summary.VoterCount)

	// Count the miners in the rolling window.
	currentBlockVersion := int32(tracker.version)
	for _, blockVersion := range tracker.blockRing {
		if blockVersion == currentBlockVersion {
			summary.NewMiners++
		} else {
			summary.OldMiners++
		}
	}

	summary.MinerCount = summary.NewMiners + summary.OldMiners
	summary.MinerProgress = float32(summary.NewMiners) / float32(summary.MinerCount)

	summary.RCIMined = uint32(tracker.voteInfo.CurrentHeight - tracker.voteInfo.StartHeight + 1)
	summary.RCIProgress = float32(summary.RCIMined) / float32(summary.RCIBlocks)

	summary.SVIMined = sviMined
	summary.SVIProgress = float32(summary.SVIMined) / float32(summary.SVIBlocks)

	summary.TilNextRCI = int64(summary.RCIBlocks-summary.RCIMined) * tracker.blockTime

	return summary
}

// Summary returns VoteTracker's most recent VoteSummary. The summary returned
// will never be modified by VoteTracker, so can be used read-only by any number
// of threads.
func (tracker *VoteTracker) Summary() *VoteSummary {
	tracker.mtx.RLock()
	defer tracker.mtx.RUnlock()
	return tracker.summary
}

// for testing
func spoof(summary *VoteSummary) {
	log.Infof("Spoofing vote data for testing. Don't forget to remove this call.")
	// summary.NetworkUpgraded = false
	// summary.VoterProgress = 0.57
	// summary.MinerProgress = 0.86
	agenda := &summary.Agendas[0]
	summary.VotingTriggered = false
	agenda.VotingTriggered = false
	agenda.IsVoting = false
	agenda.IsDefined = false

	agenda.Aye = agenda.Quorum * 9
	agenda.Nay = agenda.Aye / 7
	agenda.Abstain = agenda.Nay

	// agenda.Aye = 0
	// agenda.Nay = 0
	// agenda.Abstain = 0

	agenda.VoteCount = agenda.Aye + agenda.Nay
	agenda.AbstainRate = float32(agenda.Abstain) / float32(agenda.VoteCount)
	agenda.QuorumProgress = 1
	agenda.QuorumAchieved = true
	agenda.Approval = float32(agenda.Aye) / float32(agenda.VoteCount)
	agenda.IsWinning = false
	agenda.IsLosing = false
	agenda.IsLocked = true
	agenda.IsActive = false
	agenda.IsFailed = false
}
