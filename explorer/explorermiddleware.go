// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi"
)

type contextKey int

const (
	ctxBlockIndex contextKey = iota
	ctxBlockHash
	ctxTxHash
	ctxTxInOut
	ctxTxInOutId
	ctxAddress
	ctxAgendaId
	ctxProposalRefID
)

const (
	darkModeCoookie   = "dcrdataDarkBG"
	darkModeFormKey   = "darkmode"
	requestURIFormKey = "requestURI"
)

func (exp *explorerUI) BlockHashPathOrIndexCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		height, err := strconv.ParseInt(chi.URLParam(r, "blockhash"), 10, 0)
		var hash string
		if err != nil {
			// Not a height, try it as a hash.
			hash = chi.URLParam(r, "blockhash")
			height, err = exp.dataSource.BlockHeight(hash)
			if exp.timeoutErrorPage(w, err, "BlockHashPathOrIndexCtx>BlockHeight") {
				return
			}
			if err != nil {
				if err != sql.ErrNoRows {
					log.Warnf("BlockHeight(%s) failed: %v", hash, err)
				}
				exp.StatusPage(w, defaultErrorCode, "could not find that block", hash, ExpStatusNotFound)
				return
			}
		} else {
			// Check best DB block to recognize future blocks.
			maxHeight := exp.dataSource.Height()

			if height > maxHeight {
				expectedTime := time.Duration(height-maxHeight) * exp.ChainParams.TargetTimePerBlock
				message := fmt.Sprintf("This block is expected to arrive in approximately in %v. ", expectedTime)
				exp.StatusPage(w, defaultErrorCode, message,
					expectedTime.String(), ExpStatusFutureBlock)
				return
			}

			hash, err = exp.dataSource.GetBlockHash(height)
			if err != nil {
				hash, err = exp.dataSource.BlockHash(height)
				if err != nil {
					log.Errorf("(*ChainDB).BlockHash(%d) failed: %v", height, err)
					exp.StatusPage(w, defaultErrorCode, "could not find that block",
						fmt.Sprintf("height: %d", height), ExpStatusNotFound)
					return
				}
			}
		}

		ctx := context.WithValue(r.Context(), ctxBlockHash, hash)
		ctx = context.WithValue(ctx, ctxBlockIndex, int(height)) // Must be int!
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SyncStatusPageIntercept serves only the syncing status page until it is
// deactivated when ShowingSyncStatusPage is set to false. This page is served
// for all the possible routes supported until the background syncing is done.
func (exp *explorerUI) SyncStatusPageIntercept(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exp.ShowingSyncStatusPage() {
			exp.StatusPage(w, "Database Update Running. Please Wait.",
				"Blockchain sync is running. Please wait.", "", ExpStatusSyncing)
			return
		}
		// Otherwise, proceed to the next http handler.
		next.ServeHTTP(w, r)
	})
}

// SyncStatusAPIIntercept returns a json response back instead of a web page
// when display sync status is active for the api endpoints supported.
func (exp *explorerUI) SyncStatusAPIIntercept(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exp.ShowingSyncStatusPage() {
			exp.HandleApiRequestsOnSync(w, r)
			return
		}
		// Otherwise, proceed to the next http handler.
		next.ServeHTTP(w, r)
	})
}

// SyncStatusFileIntercept triggers an HTTP error if a file is requested for
// download before the DB is synced.
func (exp *explorerUI) SyncStatusFileIntercept(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exp.ShowingSyncStatusPage() {
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		// Otherwise, proceed to the next http handler.
		next.ServeHTTP(w, r)
	})
}

func getBlockHashCtx(r *http.Request) string {
	hash, ok := r.Context().Value(ctxBlockHash).(string)
	if !ok {
		log.Trace("Block Hash not set")
		return ""
	}
	return hash
}

func getAgendaIDCtx(r *http.Request) string {
	hash, ok := r.Context().Value(ctxAgendaId).(string)
	if !ok {
		log.Trace("Agendaid not set")
		return ""
	}
	return hash
}

func getProposalTokenCtx(r *http.Request) string {
	hash, ok := r.Context().Value(ctxProposalRefID).(string)
	if !ok {
		log.Trace("Proposal ref ID not set")
		return ""
	}
	return hash
}

// TransactionHashCtx embeds "txid" into the request context
func TransactionHashCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		txid := chi.URLParam(r, "txid")
		ctx := context.WithValue(r.Context(), ctxTxHash, txid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TransactionIoIndexCtx embeds "inout" and "inoutid" into the request context
func TransactionIoIndexCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inout := chi.URLParam(r, "inout")
		inoutid := chi.URLParam(r, "inoutid")
		ctx := context.WithValue(r.Context(), ctxTxInOut, inout)
		ctx = context.WithValue(ctx, ctxTxInOutId, inoutid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AddressPathCtx embeds "address" into the request context
func AddressPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := chi.URLParam(r, "address")
		ctx := context.WithValue(r.Context(), ctxAddress, address)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AgendaPathCtx embeds "agendaid" into the request context
func AgendaPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agendaid := chi.URLParam(r, "agendaid")
		ctx := context.WithValue(r.Context(), ctxAgendaId, agendaid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ProposalPathCtx embeds "proposalrefID" into the request context
func ProposalPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proposalRefID := chi.URLParam(r, "proposalrefid")
		ctx := context.WithValue(r.Context(), ctxProposalRefID, proposalRefID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// MenuFormParser parses a form submission from the navigation menu.
func MenuFormParser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue(darkModeFormKey) != "" {
			cookie, err := r.Cookie(darkModeCoookie)
			if err != nil && err != http.ErrNoCookie {
				log.Errorf("Cookie dcrdataDarkBG retrieval error: %v", err)
			} else {
				if err == http.ErrNoCookie {
					cookie = &http.Cookie{
						Name:   darkModeCoookie,
						Value:  "1",
						MaxAge: 0,
					}
				} else {
					cookie.Value = "0"
					cookie.MaxAge = -1
				}

				// Redirect to the specified relative path.
				requestURI := r.FormValue(requestURIFormKey)
				if requestURI == "" {
					requestURI = "/"
				}
				URL, err := url.Parse(requestURI)
				if err != nil {
					http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
					return
				}
				http.SetCookie(w, cookie)
				http.Redirect(w, r, URL.EscapedPath(), http.StatusFound)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
