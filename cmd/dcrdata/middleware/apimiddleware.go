// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package middleware

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/wire"
	apitypes "github.com/decred/dcrdata/v6/api/types"
	"github.com/didip/tollbooth/v6"
	"github.com/didip/tollbooth/v6/limiter"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/docgen"
)

type contextKey int

// These are the keys for different types of values stored in a request context.
const (
	ctxAPIDocs contextKey = iota
	CtxAddress
	ctxBlockIndex0
	ctxBlockIndex
	ctxBlockStep
	ctxBlockHash
	ctxTxHash
	ctxTxns
	ctxTxInOutIndex
	ctxN
	ctxCount
	ctxOffset
	ctxPageNum
	CtxBlockDate
	CtxLimit
	ctxGetStatus
	ctxStakeVersionLatest
	ctxRawHexTx
	ctxM
	ctxChartType
	ctxChartGrouping
	ctxTp
	ctxAgendaId
	ctxProposalToken
	ctxXcToken
	ctxStickWidth
	ctxIndent
)

type DataSource interface {
	GetHeight() (int64, error)
	GetBlockHeight(hash string) (int64, error)
	GetBlockHash(idx int64) (string, error)
}

type StakeVersionsLatest func() (*chainjson.StakeVersions, error)

// writeHTMLBadRequest is used for the Insight API error response for a BAD REQUEST.
// This means the request was malformed in some way or the request HASH,
// ADDRESS, BLOCK was not valid.
func writeHTMLBadRequest(w http.ResponseWriter, str string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	io.WriteString(w, html.EscapeString(str))
}

// Limiter wraps the tollbooth limiter. Use NewLimiter to create a new Limiter.
type Limiter struct {
	*limiter.Limiter
}

// NewLimiter creates a new Limiter for the given maximum allowed request rate
// in requests per second (may be fractional).
func NewLimiter(max float64) *Limiter {
	return &Limiter{tollbooth.NewLimiter(max, nil)}
}

// Tollbooth creates a new rate limiter middleware using the provided Limiter.
func Tollbooth(l *Limiter) func(http.Handler) http.Handler {
	// Create a middleware, capturing the Limiter.
	return func(next http.Handler) http.Handler {
		hf := func(w http.ResponseWriter, r *http.Request) {
			// Rate limit using request header.
			httpError := tollbooth.LimitByRequest(l.Limiter, w, r)
			if httpError != nil {
				// Bad client.
				l.ExecOnLimitReached(w, r)
				w.Header().Add("Content-Type", l.GetMessageContentType())
				w.WriteHeader(httpError.StatusCode)
				// The client may be gone, so just ignore any error on Write.
				_, _ = w.Write([]byte(httpError.Message))
				return
			}

			// Nice client.
			next.ServeHTTP(w, r)
		}
		return http.HandlerFunc(hf)
	}
}

// GetBlockStepCtx retrieves the ctxBlockStep data from the request context. If
// not set, the return value is -1.
func GetBlockStepCtx(r *http.Request) int {
	step, ok := r.Context().Value(ctxBlockStep).(int)
	if !ok {
		apiLog.Error("block step is not set or is not an int")
		return -1
	}
	return step
}

// GetBlockIndex0Ctx retrieves the ctxBlockIndex0 data from the request context.
// If not set, the return value is -1.
func GetBlockIndex0Ctx(r *http.Request) int {
	idx, ok := r.Context().Value(ctxBlockIndex0).(int)
	if !ok {
		apiLog.Error("block index0 is not set or is not an int")
		return -1
	}
	return idx
}

// GetTxIOIndexCtx retrieves the ctxTxInOutIndex data from the request context.
// If not set, the return value is -1.
func GetTxIOIndexCtx(r *http.Request) int {
	index, ok := r.Context().Value(ctxTxInOutIndex).(int)
	if !ok {
		apiLog.Warn("txinoutindex is not set or is not an int")
		return -1
	}
	return index
}

// GetNCtx retrieves the ctxN data from the request context. If not set, the
// return value is -1.
func GetNCtx(r *http.Request) int {
	N, ok := r.Context().Value(ctxN).(int)
	if !ok {
		apiLog.Trace("N is not set or is not an int")
		return -1
	}
	return N
}

// GetMCtx retrieves the ctxM data from the request context. If not set, the
// return value is -1.
func GetMCtx(r *http.Request) int {
	M, ok := r.Context().Value(ctxM).(int)
	if !ok {
		apiLog.Trace("M is not set or is not an int")
		return -1
	}
	return M
}

// GetTpCtx retrieves the ctxTp data from the request context.
// If the value is not set, an empty string is returned.
func GetTpCtx(r *http.Request) string {
	tp, ok := r.Context().Value(ctxTp).(string)
	if !ok {
		apiLog.Trace("ticket pool interval not set")
		return ""
	}
	return tp
}

// GetProposalTokenCtx retrieves the ctxProposalToken data from the request context.
// If the value is not set, an empty string is returned.
func GetProposalTokenCtx(r *http.Request) string {
	tp, ok := r.Context().Value(ctxProposalToken).(string)
	if !ok {
		apiLog.Trace("proposal token hash not set")
		return ""
	}
	return tp
}

// GetRawHexTx retrieves the ctxRawHexTx data from the request context. If not
// set, the return value is an empty string.
func GetRawHexTx(r *http.Request) (string, error) {
	rawHexTx, ok := r.Context().Value(ctxRawHexTx).(string)
	if !ok {
		apiLog.Trace("hex transaction id not set")
		return "", fmt.Errorf("hex transaction id not set")
	}

	msgtx := wire.NewMsgTx()
	err := msgtx.Deserialize(hex.NewDecoder(strings.NewReader(rawHexTx)))
	if err != nil {
		return "", fmt.Errorf("failed to deserialize tx: %w", err)
	}
	return rawHexTx, nil
}

// NoOrigin removes any Origin from the request header.
func NoOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Del("Origin")
		next.ServeHTTP(w, r)
	})
}

// OriginalRequestURI checks the X-Original-Request-URI HTTP request header for
// a valid URI, and patches the request URL's Path and RawPath. This may be
// useful in the event that a reverse proxy maps requests on path A to path B,
// but the request handler requires the original path A (e.g. generating links
// relative to path A).
func OriginalRequestURI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origRequestURI := r.Header.Get("X-Original-Request-URI")
		if origRequestURI != "" {
			// Create a temporary URL to parse and (un)escape the URI provided in
			// the X-Original-Request-URI requests header.
			newURL, err := url.Parse(origRequestURI)
			if err != nil {
				apiLog.Debugf("X-Original-Request-URI (%s) is not a valid URI: %v",
					origRequestURI, err)
				next.ServeHTTP(w, r)
				return
			}
			// Patch the the Requests URL's Path and RawPath so that
			// (*http.Request).URL.RequestURI() may assemble escaped path?query.
			r.URL.Path = newURL.Path
			r.URL.RawPath = newURL.RawPath
		}
		next.ServeHTTP(w, r)
	})
}

// PostBroadcastTxCtx is middleware that checks for parameters given in POST
// request body of the broadcast transaction endpoint.
func PostBroadcastTxCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.InsightRawTx
		body, err := ioutil.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			writeHTMLBadRequest(w, fmt.Sprintf("Error reading JSON message: %v", err))
			return
		}

		err = json.Unmarshal(body, &req)
		if err != nil {
			writeHTMLBadRequest(w, fmt.Sprintf("Failed to parse request: %v", err))
			return
		}

		// Successful extraction of Body JSON as long as the rawtx is not empty
		// string we should return it.
		if req.Rawtx == "" {
			writeHTMLBadRequest(w, "rawtx cannot be an empty string.")
			return
		}

		ctx := context.WithValue(r.Context(), ctxRawHexTx, req.Rawtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetTxIDCtx retrieves the ctxTxHash data from the request context. If not set,
// the return value is an empty string.
func GetTxIDCtx(r *http.Request) (*chainhash.Hash, error) {
	hashStr, ok := r.Context().Value(ctxTxHash).(string)
	if !ok {
		apiLog.Trace("txid not set")
		return nil, fmt.Errorf("txid not set")
	}
	if len(hashStr) != chainhash.MaxHashStringSize {
		apiLog.Tracef("invalid hash: %v", hashStr)
		return nil, fmt.Errorf("invalid hash")
	}
	hash, err := chainhash.NewHashFromStr(hashStr)
	if err != nil {
		apiLog.Trace("invalid hash '%s': %v", hashStr, err)
		return nil, fmt.Errorf("invalid hash: %w", err)
	}
	return hash, nil
}

// GetTxnsCtx retrieves the ctxTxns data from the request context. If not set,
// the return value is an empty string slice.
func GetTxnsCtx(r *http.Request) ([]*chainhash.Hash, error) {
	hashStrs, ok := r.Context().Value(ctxTxns).([]string)
	if !ok || len(hashStrs) == 0 {
		apiLog.Trace("ctxTxns not set")
		return nil, fmt.Errorf("ctxTxns not set")
	}

	hashes := make([]*chainhash.Hash, 0, len(hashStrs))
	for _, hashStr := range hashStrs {
		if len(hashStr) != chainhash.MaxHashStringSize {
			apiLog.Tracef("invalid hash: %v", hashStr)
			return nil, fmt.Errorf("invalid hash")
		}
		hash, err := chainhash.NewHashFromStr(hashStr)
		if err != nil {
			apiLog.Trace("invalid hash '%s': %v", hashStr, err)
			return nil, fmt.Errorf("invalid hash: %w", err)
		}
		hashes = append(hashes, hash)
	}

	return hashes, nil
}

// Next is a dummy middleware that just continues with the next http.Handler.
func Next(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

// PostTxnsCtx extract transaction IDs from the POST body
func PostTxnsCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := apitypes.Txns{}
		body, err := ioutil.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			apiLog.Debugf("No/invalid txns: %v", err)
			http.Error(w, "error reading JSON message", http.StatusBadRequest)
			return
		}
		err = json.Unmarshal(body, &req)
		if err != nil {
			apiLog.Debugf("failed to unmarshal JSON request to apitypes.Txns: %v", err)
			http.Error(w, "failed to unmarshal JSON request", http.StatusBadRequest)
			return
		}
		// Successful extraction of body JSON
		ctx := context.WithValue(r.Context(), ctxTxns, req.Transactions)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ValidateTxnsPostCtx will confirm Post content length is valid.
func ValidateTxnsPostCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentLengthString := r.Header.Get("Content-Length")
		contentLength, err := strconv.Atoi(contentLengthString)
		if err != nil {
			http.Error(w, "Unable to parse Content-Length", http.StatusBadRequest)
			return
		}
		// Broadcast Tx has the largest possible body.
		maxPayload := 1 << 22
		if contentLength > maxPayload {
			http.Error(w, fmt.Sprintf("Maximum Content-Length is %d", maxPayload), http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetBlockHashCtx retrieves the ctxBlockHash data from the request context. If
// not set, the return value is an empty string.
func GetBlockHashCtx(r *http.Request) (string, error) {
	hashStr, ok := r.Context().Value(ctxBlockHash).(string)
	if !ok {
		apiLog.Trace("block hash not set")
		return "", fmt.Errorf("block hash not set")
	}
	if len(hashStr) != chainhash.MaxHashStringSize {
		apiLog.Tracef("invalid hash: %v", hashStr)
		return "", fmt.Errorf("invalid hash")
	}
	if _, err := chainhash.NewHashFromStr(hashStr); err != nil {
		apiLog.Trace("invalid hash '%s': %v", hashStr, err)
		return "", fmt.Errorf("invalid hash: %w", err)
	}

	return hashStr, nil
}

// GetAddressCtx returns a slice of base-58 encoded addresses parsed from the
// {address} URL parameter. Duplicate addresses are removed. Multiple
// comma-delimited address can be specified.
func GetAddressCtx(r *http.Request, activeNetParams *chaincfg.Params) ([]string, error) {
	addressStrs, ok := r.Context().Value(CtxAddress).([]string)
	if !ok {
		return nil, fmt.Errorf("type assertion failed")
	}

	strInSlice := func(sl []string, s string) bool {
		for i := range sl {
			if sl[i] == s {
				return true
			}
		}
		return false
	}

	// Allocate as if all addresses are unique.
	addrStrs := make([]string, 0, len(addressStrs))
	for _, addrStr := range addressStrs {
		if strInSlice(addrStrs, addrStr) {
			continue
		}
		addrStrs = append(addrStrs, addrStr)
	}

	for _, addrStr := range addressStrs {
		_, err := dcrutil.DecodeAddress(addrStr, activeNetParams)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q for this network: %w",
				addrStr, err)
		}
	}
	return addrStrs, nil
}

// GetAddressRawCtx returns a slice of addresses parsed from the {address} URL
// parameter. Multiple comma-delimited address strings can be specified.
func GetAddressRawCtx(r *http.Request, activeNetParams *chaincfg.Params) ([]dcrutil.Address, error) {
	addressStrs, ok := r.Context().Value(CtxAddress).([]string)
	if !ok {
		return nil, fmt.Errorf("type assertion failed")
	}
	addresses := make([]dcrutil.Address, 0, len(addressStrs))
	for _, addrStr := range addressStrs {
		addr, err := dcrutil.DecodeAddress(addrStr, activeNetParams)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q for this network: %w",
				addrStr, err)
		}
		addresses = append(addresses, addr)
	}
	return addresses, nil
}

// GetChartTypeCtx retrieves the ctxChart data from the request context.
// If not set, the return value is an empty string.
func GetChartTypeCtx(r *http.Request) string {
	chartType, ok := r.Context().Value(ctxChartType).(string)
	if !ok {
		apiLog.Trace("chart type not set")
		return ""
	}
	return chartType
}

// GetChartGroupingCtx retrieves the ctxChart data from the request context.
// If not set, the return value is an empty string.
func GetChartGroupingCtx(r *http.Request) string {
	chartType, ok := r.Context().Value(ctxChartGrouping).(string)
	if !ok {
		apiLog.Trace("chart grouping not set")
		return ""
	}
	return chartType
}

// GetCountCtx retrieves the ctxCount data ("to") URL path element from the
// request context. If not set, the return value is 20.
func GetCountCtx(r *http.Request) int {
	count, ok := r.Context().Value(ctxCount).(int)
	if !ok {
		apiLog.Warn("count is not set or is not an int")
		return 20
	}
	return count
}

// GetOffsetCtx retrieves the ctxOffset data ("from") from the request context.
// If not set, the return value is 0.
func GetOffsetCtx(r *http.Request) int {
	offset, ok := r.Context().Value(ctxOffset).(int)
	if !ok {
		apiLog.Warn("offset is not set or is not an int")
		return 0
	}
	return offset
}

// GetPageNumCtx retrieves the ctxPageNum data ("pageNum") URL path element from
// the request context. If not set, the return value is 1. The page number must
// be a postitive integer.
func GetPageNumCtx(r *http.Request) int {
	pageNum, ok := r.Context().Value(ctxPageNum).(int)
	if !ok {
		apiLog.Debug("pageNum is not set or is not an int")
		return 1
	}
	if pageNum < 1 {
		pageNum = 1
	}
	return pageNum
}

// GetStatusInfoCtx retrieves the ctxGetStatus data ("q" POST form data) from
// the request context. If not set, the return value is an empty string.
func GetStatusInfoCtx(r *http.Request) string {
	statusInfo, ok := r.Context().Value(ctxGetStatus).(string)
	if !ok {
		apiLog.Warn("status info is not set or is not a string")
		return ""
	}
	return statusInfo
}

// GetBlockDateCtx retrieves the ctxBlockDate data from the request context. If
// not set, the return value is an empty string.
func GetBlockDateCtx(r *http.Request) string {
	blockDate, _ := r.Context().Value(CtxBlockDate).(string)
	return blockDate
}

// GetBlockIndexCtx retrieves the ctxBlockIndex data from the request context.
// If not set, the return -1.
func GetBlockIndexCtx(r *http.Request) int {
	idx, ok := r.Context().Value(ctxBlockIndex).(int)
	if !ok {
		apiLog.Warn("block index not set or is not an int")
		return -1
	}
	return idx
}

// CacheControl creates a new middleware to set the HTTP response header with
// "Cache-Control: max-age=maxAge" where maxAge is in seconds.
func CacheControl(maxAge int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "max-age="+strconv.FormatInt(maxAge, 10))
			next.ServeHTTP(w, r)
		})
	}
}

// Indent creates a middleware for using the specified JSON indentation string
// when the "indent" URL query parameter parses to a true boolean value. Use
// GetIndentCtx with request handlers with the Indent middeware.
func Indent(indent string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			useIndentation := r.URL.Query().Get("indent")
			if useIndentation != "" {
				b, err := strconv.ParseBool(useIndentation)
				if err != nil {
					http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
					return // end request handling
				}
				if b {
					// Use the configured indent string.
					ctx := context.WithValue(r.Context(), ctxIndent, indent)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// GetIndentCtx retrieves the ctxIndent data from the request context. If not
// set, the return value is an empty string.
func GetIndentCtx(r *http.Request) string {
	indent, _ := r.Context().Value(ctxIndent).(string)
	return indent
}

// Server sets the Server header element.
func Server(server string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Server", server)
			next.ServeHTTP(w, r)
		})
	}
}

// BlockStepPathCtx returns a http.HandlerFunc that embeds the value at the url
// part {step} into the request context.
func BlockStepPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stepIdxStr := chi.URLParam(r, "step")
		step, err := strconv.Atoi(stepIdxStr)
		if err != nil {
			apiLog.Infof("No/invalid step value (int64): %v", err)
			http.NotFound(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), ctxBlockStep, step)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// BlockIndexPathCtx returns a http.HandlerFunc that embeds the value at the url
// part {idx} into the request context.
func BlockIndexPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathIdxStr := chi.URLParam(r, "idx")
		idx, err := strconv.Atoi(pathIdxStr)
		if err != nil {
			apiLog.Infof("No/invalid idx value (int64): %v", err)
			http.Error(w, "Valid index not provided", http.StatusBadRequest)
			return
		}
		ctx := context.WithValue(r.Context(), ctxBlockIndex, idx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// BlockIndexOrHashPathCtx returns a http.HandlerFunc that embeds the value at
// the url part {idxorhash} into the request context.
func BlockIndexOrHashPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ctx context.Context
		pathIdxOrHashStr := chi.URLParam(r, "idxorhash")
		if len(pathIdxOrHashStr) == 2*chainhash.HashSize {
			ctx = context.WithValue(r.Context(), ctxBlockHash, pathIdxOrHashStr)
		} else {
			idx, err := strconv.Atoi(pathIdxOrHashStr)
			if err != nil {
				apiLog.Infof("No/invalid idx value (int64): %v", err)
				http.Error(w, "Hash or index not provided", http.StatusBadRequest)
				return
			}
			ctx = context.WithValue(r.Context(), ctxBlockIndex, idx)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// BlockIndex0PathCtx returns a http.HandlerFunc that embeds the value at the
// url part {idx0} into the request context.
func BlockIndex0PathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathIdxStr := chi.URLParam(r, "idx0")
		idx, err := strconv.Atoi(pathIdxStr)
		if err != nil {
			apiLog.Infof("No/invalid idx0 value (int64): %v", err)
			http.NotFound(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), ctxBlockIndex0, idx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// NPathCtx returns a http.HandlerFunc that embeds the value at the url part {N}
// into the request context.
func NPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathNStr := chi.URLParam(r, "N")
		N, err := strconv.Atoi(pathNStr)
		if err != nil {
			apiLog.Infof("No/invalid numeric value (uint64): %v", err)
			http.NotFound(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), ctxN, N)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// MPathCtx returns a http.HandlerFunc that embeds the value at the url
// part {M} into the request context
func MPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathMStr := chi.URLParam(r, "M")
		M, err := strconv.Atoi(pathMStr)
		if err != nil {
			apiLog.Infof("No/invalid numeric value (uint64): %v", err)
			http.NotFound(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), ctxM, M)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TicketPoolCtx returns a http.HandlerFunc that embeds the value at the url
// part {tp} into the request context
func TicketPoolCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tp := chi.URLParam(r, "tp")
		ctx := context.WithValue(r.Context(), ctxTp, tp)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ProposalTokenCtx returns a http.HandlerFunc that embeds the value at the url
// part {token} into the request context
func ProposalTokenCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := chi.URLParam(r, "token")
		ctx := context.WithValue(r.Context(), ctxProposalToken, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// BlockHashPathCtx returns a http.HandlerFunc that embeds the value at the url
// part {blockhash} into the request context.
func BlockHashPathCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash := chi.URLParam(r, "blockhash")
		ctx := context.WithValue(r.Context(), ctxBlockHash, hash)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TransactionHashCtx returns a http.HandlerFunc that embeds the value at the
// url part {txid} into the request context.
func TransactionHashCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		txid := chi.URLParam(r, "txid")
		ctx := context.WithValue(r.Context(), ctxTxHash, txid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TransactionIOIndexCtx returns a http.HandlerFunc that embeds the value at the
// url part {txinoutindex} into the request context
func TransactionIOIndexCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idxStr := chi.URLParam(r, "txinoutindex")
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			apiLog.Infof("No/invalid numeric value (%v): %v", idxStr, err)
			http.NotFound(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), ctxTxInOutIndex, idx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

const (
	minAddressLength = 35 // p2pkh and p2sh
	maxAddressLength = 53 // p2pk
)

// AddressPathCtxN constructs a middleware that returns a http.HandlerFunc which
// parses the value at the url part {address} into the a list of addresses not
// longer than n, and embeds the slice into the request context.
func AddressPathCtxN(n int) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addressStr := chi.URLParam(r, "address")
			if len(addressStr) < minAddressLength {
				http.Error(w, "invalid address", http.StatusUnprocessableEntity)
				return
			}
			// string can't be longer than n addresses, plus n - 1 commas.
			if len(addressStr) > n*(maxAddressLength+1)-1 {
				apiLog.Warnf("AddressPathCtxN rejecting address parameter of length %d", len(addressStr))
				http.Error(w, "too many address", http.StatusUnprocessableEntity)
				return
			}
			addrs := strings.Split(addressStr, ",")
			if len(addrs) > n {
				apiLog.Warnf("AddressPathCtxN parsed %d > %d strings", len(addrs), n)
				http.Error(w, "address parse error", http.StatusUnprocessableEntity)
				return
			}
			ctx := context.WithValue(r.Context(), CtxAddress, addrs)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ChartTypeCtx returns a http.HandlerFunc that embeds the value at the url
// part {charttype} into the request context.
func ChartTypeCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxChartType,
			chi.URLParam(r, "charttype"))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ChartGroupingCtx returns a http.HandlerFunc that embeds the value art the url
// part {chartgrouping} into the request context.
func ChartGroupingCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxChartGrouping,
			chi.URLParam(r, "chartgrouping"))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// apiDocs generates a middleware with a "docs" in the context containing a map
// of the routers handlers, etc.
func apiDocs(mux *chi.Mux) func(next http.Handler) http.Handler {
	var buf bytes.Buffer
	err := json.Indent(&buf, []byte(docgen.JSONRoutesDoc(mux)), "", "\t")
	if err != nil {
		apiLog.Errorf("failed to prepare JSON routes docs: %v", err)
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, http.StatusText(http.StatusInternalServerError),
					http.StatusInternalServerError)
			})
		}
	}
	docs := buf.String()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), ctxAPIDocs, docs)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// APIDirectory is the actual handler used with apiDocs
// (e.g. mux.With(apiDocs(mux)).HandleFunc("/help", APIDirectory))
func APIDirectory(w http.ResponseWriter, r *http.Request) {
	docs := r.Context().Value(ctxAPIDocs).(string)
	io.WriteString(w, docs)
}

// TransactionsCtx returns a http.Handlerfunc that embeds the {address,
// blockhash} value in the request into the request context.
func TransactionsCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := r.FormValue("address")
		if address != "" {
			ctx := context.WithValue(r.Context(), CtxAddress, []string{address})
			next.ServeHTTP(w, r.WithContext(ctx))
		}

		hash := r.FormValue("block")
		if hash != "" {
			ctx := context.WithValue(r.Context(), ctxBlockHash, hash)
			next.ServeHTTP(w, r.WithContext(ctx))
		}
	})
}

// PaginationCtx returns a http.Handlerfunc that embeds the {to,from} value in
// the request into the request context.
func PaginationCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		to, from := r.FormValue("to"), r.FormValue("from")
		if to == "" {
			to = "20"
		}

		if from == "" {
			from = "0"
		}

		offset, err := strconv.Atoi(from)
		if err != nil {
			http.Error(w, "invalid from value", 422)
			return
		}
		count, err := strconv.Atoi(to)
		if err != nil {
			http.Error(w, "invalid to value", 422)
			return
		}

		ctx := context.WithValue(r.Context(), ctxCount, count)
		ctx = context.WithValue(ctx, ctxOffset, offset)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// PageNumCtx returns a http.Handlerfunc that embeds the {pageNum} URL query
// parameter value in the request into the request context.
func PageNumCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageNumStr := r.FormValue("pageNum")
		if pageNumStr == "" {
			pageNumStr = "1"
		}

		pageNum, err := strconv.Atoi(pageNumStr)
		if err != nil {
			http.Error(w, "invalid from value", 422)
			return
		}

		ctx := context.WithValue(r.Context(), ctxPageNum, pageNum)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AddressPostCtx returns a http.HandlerFunc that embeds the {addrs} value in
// the post request into the request context.
func AddressPostCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := r.PostFormValue("addrs")
		ctx := context.WithValue(r.Context(), CtxAddress, []string{address})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// BlockDateQueryCtx returns a http.Handlerfunc that embeds the {blockdate,
// limit} value in the request into the request context.
func BlockDateQueryCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blockDate := r.FormValue("blockDate")
		limit := r.FormValue("limit")
		if blockDate == "" {
			http.Error(w, "invalid block date", 422)
			return
		}
		fmt.Println("limit in block query ", limit)
		ctx := context.WithValue(r.Context(), CtxBlockDate, blockDate)
		ctx = context.WithValue(ctx, CtxLimit, limit)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AgendaIdCtx returns a http.HandlerFunc that embeds the value at the url part
// {agendaId} into the request context.
func AgendaIdCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agendaId := chi.URLParam(r, "agendaId")
		ctx := context.WithValue(r.Context(), ctxAgendaId, agendaId)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AgendIdCtx returns a http.HandlerFunc that embeds the value at the url part
// {agendaId} into the request context. This is here for backward compatibility.
func AgendIdCtx(next http.Handler) http.Handler {
	return AgendaIdCtx(next)
}

// GetAgendaIdCtx retrieves the ctxAgendaId data from the request context.
// If not set, the return value is an empty string.
func GetAgendaIdCtx(r *http.Request) string {
	agendaId, ok := r.Context().Value(ctxAgendaId).(string)
	if !ok {
		apiLog.Error("agendaId not parsed")
		return ""
	}
	return agendaId
}

// BlockHashPathAndIndexCtx embeds the value at the url part {blockhash}, and
// the corresponding block index, into a request context.
func BlockHashPathAndIndexCtx(r *http.Request, source DataSource) context.Context {
	hash := chi.URLParam(r, "blockhash")
	height, err := source.GetBlockHeight(hash)
	if err != nil {
		apiLog.Errorf("Unable to GetBlockHeight(%d): %v", height, err)
	}
	ctx := context.WithValue(r.Context(), ctxBlockHash, hash)
	return context.WithValue(ctx, ctxBlockIndex, int(height)) // Must be int!
}

// StatusInfoCtx embeds the best block index and the POST form data for
// parameter "q" into a request context.
func StatusInfoCtx(r *http.Request, source DataSource) context.Context {
	idx := int64(-1)
	h, err := source.GetHeight()
	if h >= 0 && err == nil {
		idx = h
	}
	ctx := context.WithValue(r.Context(), ctxBlockIndex, int(idx)) // Must be int!

	q := r.FormValue("q")
	return context.WithValue(ctx, ctxGetStatus, q)
}

// BlockHashLatestCtx embeds the current block height and hash into a request
// context.
func BlockHashLatestCtx(r *http.Request, source DataSource) context.Context {
	var hash string
	// if hash, err = c.BlockData.GetBestBlockHash(int64(idx)); err != nil {
	// 	apiLog.Errorf("Unable to GetBestBlockHash: %v", idx, err)
	// }
	idx, err := source.GetHeight()
	if idx >= 0 && err == nil {
		var err error
		if hash, err = source.GetBlockHash(idx); err != nil {
			apiLog.Errorf("Unable to GetBlockHash(%d): %v", idx, err)
		}
	}

	ctx := context.WithValue(r.Context(), ctxBlockIndex, int(idx)) // Must be int!
	return context.WithValue(ctx, ctxBlockHash, hash)
}

// StakeVersionLatestCtx embeds the specified StakeVersionsLatest function into
// a request context.
func StakeVersionLatestCtx(r *http.Request, stakeVerFun StakeVersionsLatest) context.Context {
	ver := -1
	stkVers, err := stakeVerFun()
	if err == nil && stkVers != nil {
		ver = int(stkVers.StakeVersion)
	}

	return context.WithValue(r.Context(), ctxStakeVersionLatest, ver)
}

// BlockIndexLatestCtx embeds the current block height into a request context.
func BlockIndexLatestCtx(r *http.Request, source DataSource) context.Context {
	idx := int64(-1)
	h, err := source.GetHeight()
	if h >= 0 && err == nil {
		idx = h
	}

	return context.WithValue(r.Context(), ctxBlockIndex, int(idx)) // Must be int!
}

// GetBlockHeightCtx returns the block height for the block index or hash
// specified on the URL path.
func GetBlockHeightCtx(r *http.Request, source DataSource) (int64, error) {
	idxI, ok := r.Context().Value(ctxBlockIndex).(int)
	idx := int64(idxI)
	if !ok || idx < 0 {
		hash, err := GetBlockHashCtx(r)
		if err != nil {
			return 0, err
		}
		idx, err = source.GetBlockHeight(hash)
		if err != nil {
			return 0, err
		}
	}
	return idx, nil
}

// GetLatestVoteVersionCtx attempts to retrieve the latest stake version
// embedded in the request context.
func GetLatestVoteVersionCtx(r *http.Request) int {
	ver, ok := r.Context().Value(ctxStakeVersionLatest).(int)
	if !ok {
		apiLog.Error("latest stake version is not set or is not an int")
		return -1
	}
	return ver
}

// ExchangeTokenContext pulls the exchange token from the URL.
func ExchangeTokenContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := chi.URLParam(r, "token")
		ctx := context.WithValue(r.Context(), ctxXcToken, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RetrieveExchangeTokenCtx tries to fetch the exchange token from the request
// context.
func RetrieveExchangeTokenCtx(r *http.Request) string {
	token, ok := r.Context().Value(ctxXcToken).(string)
	if !ok {
		apiLog.Error("non-string encountered in exchange token context")
		return ""
	}
	return token
}

// StickWidthContext pulls the bin width from the URL.
func StickWidthContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bin := chi.URLParam(r, "bin")
		ctx := context.WithValue(r.Context(), ctxStickWidth, bin)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RetrieveStickWidthCtx tries to fetch the candlestick width from the request
// context.
func RetrieveStickWidthCtx(r *http.Request) string {
	bin, ok := r.Context().Value(ctxStickWidth).(string)
	if !ok {
		apiLog.Error("non-string encountered in candlestick width context")
		return ""
	}
	return bin
}
