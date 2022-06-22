package explorer

import (
	"math"
	"sync"
	"time"

	"github.com/decred/dcrdata/v8/db/dbtypes"
	pstypes "github.com/decred/dcrdata/v8/pubsub/types"
)

// SyncStatusInfo defines information for a single progress bar.
type SyncStatusInfo struct {
	// PercentComplete is the percentage of sync complete for a given progress bar.
	PercentComplete float64 `json:"percentage_complete"`
	// BarMsg holds the main bar message about the currect sync.
	BarMsg string `json:"bar_msg"`
	// BarSubtitle holds any other information about the current main sync. This
	// value may include but not limited to; db indexing, deleting duplicates etc.
	BarSubtitle string `json:"subtitle"`
	// Time is the estimated time in seconds to the sync should be complete.
	Time int64 `json:"seconds_to_complete"`
	// ProgressBarID is the given entry progress bar id needed on the UI page.
	ProgressBarID string `json:"progress_bar_id"`
}

// syncStatus makes it possible to update the user on the progress of the
// blockchain db syncing that is running after new blocks were detected on
// system startup. ProgressBars is an array whose every entry is one of the
// progress bars data that will be displayed on the sync status page.
type syncStatus struct {
	sync.RWMutex
	ProgressBars []SyncStatusInfo
}

// blockchainSyncStatus defines the status update displayed on the syncing
// status page when new blocks are being appended into the db.
var blockchainSyncStatus = new(syncStatus)

// SyncStatus defines a thread-safe way to read the sync status updates
func SyncStatus() []SyncStatusInfo {
	blockchainSyncStatus.RLock()
	defer blockchainSyncStatus.RUnlock()

	return blockchainSyncStatus.ProgressBars
}

// ShowingSyncStatusPage is a thread-safe way to fetch the
// displaySyncStatusPage.
func (exp *explorerUI) ShowingSyncStatusPage() bool {
	display, ok := exp.displaySyncStatusPage.Load().(bool)
	return ok && display
}

// EnableSyncStatusPage enables or disables updates to the sync status page.
func (exp *explorerUI) EnableSyncStatusPage(displayStatus bool) {
	exp.displaySyncStatusPage.Store(displayStatus)
}

// BeginSyncStatusUpdates receives the progress updates and and updates the
// blockchainSyncStatus.ProgressBars.
func (exp *explorerUI) BeginSyncStatusUpdates(barLoad chan *dbtypes.ProgressBarLoad) {
	// Do not start listening for updates if channel is nil.
	if barLoad == nil {
		log.Warnf("Not updating sync status page.")
		return
	}

	// stopTimer allows safe exit of the goroutine that triggers periodic
	// websocket progress update.
	stopTimer := make(chan struct{})

	exp.EnableSyncStatusPage(true)

	// Periodically trigger websocket hub to signal a progress update.
	go func() {
		timer := time.NewTicker(syncStatusInterval)

	timerLoop:
		for {
			select {
			case <-timer.C:
				log.Trace("Sending progress bar signal.")
				exp.wsHub.HubRelay <- pstypes.HubMessage{Signal: sigSyncStatus}

			case <-stopTimer:
				log.Debug("Stopping progress bar signals.")
				timer.Stop()
				break timerLoop
			}
		}
	}()

	// Update the progress bar data when progress updates are received from the
	// sync routine of a database backend.
	go func() {
		// The receive loop quits when barLoad is closed or a nil *Hash is sent.
		// In either case, the barLoad channel is set to nil when the goroutine
		// returns. As a result, the websocket trigger goroutine will return.
		defer func() {
			log.Debug("Finished with sync status updates.")
			barLoad = nil
			// Send the one last signal so that the websocket can send the final
			// confirmation that syncing is done and home page auto reload should
			// happen.
			exp.wsHub.HubRelay <- pstypes.HubMessage{Signal: sigSyncStatus}
			exp.EnableSyncStatusPage(false)
		}()

	barloop:
		for bar := range barLoad {
			if bar == nil {
				stopTimer <- struct{}{}
				return
			}

			var percentage float64
			if bar.To > 0 {
				percentage = math.Floor(float64(bar.From)/float64(bar.To)*10000) / 100
			}

			val := SyncStatusInfo{
				PercentComplete: percentage,
				BarMsg:          bar.Msg,
				Time:            bar.Timestamp,
				ProgressBarID:   bar.BarID,
				BarSubtitle:     bar.Subtitle,
			}

			// Update existing progress bar if one is found with this ID.
			blockchainSyncStatus.Lock()
			for i, v := range blockchainSyncStatus.ProgressBars {
				if v.ProgressBarID == bar.BarID {
					// Existing progress bar data.
					if len(bar.Subtitle) > 0 && bar.Timestamp == 0 {
						// Handle case scenario when only subtitle should be updated.
						blockchainSyncStatus.ProgressBars[i].BarSubtitle = bar.Subtitle
					} else {
						blockchainSyncStatus.ProgressBars[i] = val
					}
					// Go back to waiting for updates.
					blockchainSyncStatus.Unlock()
					continue barloop
				}
			}

			// Existing bar with this ID not found, append new.
			blockchainSyncStatus.ProgressBars = append(blockchainSyncStatus.ProgressBars, val)
			blockchainSyncStatus.Unlock()
		}
	}()
}
