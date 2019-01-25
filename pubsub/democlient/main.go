package main

import (
	"log"
	"strings"
	"time"

	exptypes "github.com/decred/dcrdata/v4/explorer/types"
	client "github.com/decred/dcrdata/v4/pubsub/psclient"
	pstypes "github.com/decred/dcrdata/v4/pubsub/types"
	"golang.org/x/net/websocket"
)

var cfg *config

func main() {
	var err error
	cfg, err = loadConfig()
	if err != nil {
		log.Fatalf("%v", err)
		return
	}

	// Create the websocket connection.
	origin := "/"
	ws, err := websocket.Dial(cfg.URL, "", origin)
	if err != nil {
		log.Fatalf("%v", err)
		return
	}
	defer ws.Close()

	// Create the pubsub client.
	cl := client.New(ws)
	cl.ReadTimeout = 30 * time.Second
	cl.WriteTimeout = 10 * time.Second

	// Subscribe to several events.
	subs := []string{"ping", "newtx", "newblock", "mempool"}
	for _, sub := range subs {
		resp, err := cl.Subscribe(sub)
		if err != nil {
			log.Fatalf("Failed to subscribe: %v", err)
			return
		}
		log.Printf(resp.Message)
	}

	// Begin receiving messages.
	for {
		resp, err := cl.ReceiveMsg()
		if err != nil {
			if strings.Contains(err.Error(), "i/o timeout") {
				continue
			}
			return
		}

		msg, err := client.DecodeMsg(resp)
		if err != nil {
			log.Printf("Failed to decode message: %v", err)
			continue
		}

		switch m := msg.(type) {
		case string:
			log.Printf("Message (%s): %s", resp.EventId, m)
		case *exptypes.WebsocketBlock:
			log.Printf("Message (%s): WebsocketBlock(hash=%s)", resp.EventId, m.Block.Hash)
		case *exptypes.MempoolShort:
			t := time.Unix(m.Time, 0)
			log.Printf("Message (%s): MempoolShort(numTx=%d, time=%v)",
				resp.EventId, m.NumAll, t)
		case *pstypes.TxList:
			log.Printf("Message (%s): TxList(len=%d)", resp.EventId, len(*m))
		default:
			log.Printf("Message of type %v unhandled.", resp.EventId)
		}
	}
}
