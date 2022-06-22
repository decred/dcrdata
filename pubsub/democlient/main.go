// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/slog"
	survey "gopkg.in/AlecAivazis/survey.v1"

	exptypes "github.com/decred/dcrdata/v8/explorer/types"
	"github.com/decred/dcrdata/v8/pubsub/psclient"
	pstypes "github.com/decred/dcrdata/v8/pubsub/types"
	"github.com/decred/dcrdata/v8/semver"
)

var cfg *config

func main() {
	var err error
	cfg, err = loadConfig()
	if err != nil {
		log.Fatalf("%v", err)
		return
	}

	backend := slog.NewBackend(os.Stdout).Logger("PSCL")
	backend.SetLevel(slog.LevelDebug)
	psclient.UseLogger(backend)

	params := chaincfg.MainNetParams()

	// Create the pubsub client, opening a connection to the URL.
	ctx, cancel := context.WithCancel(context.Background())
	opts := psclient.Opts{
		ReadTimeout:  psclient.DefaultReadTimeout,
		WriteTimeout: 3 * time.Second,
	}
	cl, err := psclient.New(cfg.URL, ctx, &opts)
	if err != nil {
		log.Fatalf("Failed to connect to %s: %v", cfg.URL, err)
		os.Exit(1)
	}
	defer cl.Stop()

	log.Printf("You are now connected to %s.\n", cfg.URL)

	// Subscribe/unsubscribe to several events.
	var currentSubs []string
	allSubs := []string{"ping", "newtxs", "newblock", "mempool", "address:Dcur2mcGjmENx4DhNqDctW5wJCVyT3Qeqkx", "address"}
	subscribe := func(newsubs []string) error {
		for _, sub := range newsubs {
			if subd, _ := strInSlice(currentSubs, sub); subd {
				log.Printf("Already subscribed to %s.", sub)
				continue
			}
			currentSubs = append(currentSubs, sub)
			resp, err := cl.Subscribe(sub)
			if err != nil {
				return fmt.Errorf("Failed to subscribe: %v", err)
			}
			// Print the json.RawMessage in the response message
			log.Printf("Response: success=%v, message=%s\n", resp.Success, resp.Data)
		}
		return nil
	}
	unsubscribe := func(rmsubs []string) error {
		for _, sub := range rmsubs {
			subd, i := strInSlice(currentSubs, sub)
			if !subd {
				log.Printf("Not subscribed to %s.", sub)
				continue
			}
			currentSubs = append(currentSubs[:i], currentSubs[i+1:]...)
			resp, err := cl.Unsubscribe(sub)
			if err != nil {
				return fmt.Errorf("Failed to unsubscribe: %v", err)
			}
			// Print the json.RawMessage in the response message
			log.Printf("Response: success=%v, message=%s\n", resp.Success, resp.Data)
		}
		return nil
	}

	// Prompts
	type actionData struct {
		action string
		data   []string
	}

	subPrompt := &survey.MultiSelect{
		Message: "Subscribe to events:",
		Options: allSubs,
	}
	unsubPrompt := &survey.MultiSelect{
		Message: "Unsubscribe to events:",
		Options: allSubs,
	}

	hitEnter := func() {
		fmt.Println("Hit a key to choose an action.")
		//bufio.NewReaderSize(os.Stdin, 1).ReadByte()
		os.Stdin.Read([]byte{0})
	}

	// Prompting goroutine
	go func() {
		for {
			hitEnter()
			var a actionData
			actionPrompt := &survey.Select{
				Message: "What now?",
				Options: []string{"subscribe", "unsubscribe", "ping the server", "version", "quit"},
			}
			err := survey.AskOne(actionPrompt, &a.action, nil)
			if err != nil {
				log.Fatal(err)
				continue
			}

			switch a.action {
			case "subscribe":
				subPrompt.Default = AnotInB(allSubs, append(currentSubs, "address"))
				_ = survey.AskOne(subPrompt, &a.data, nil)
				data := make([]string, 0, len(a.data))
				for i := range a.data {
					if a.data[i] == "address" {
						var addr string
						err = survey.AskOne(&survey.Input{Message: "Type the address."}, &addr, nil)
						if err != nil {
							log.Fatal(err)
							continue
						}
						_, err = stdaddr.DecodeAddress(addr, params)
						if err != nil {
							log.Fatalf("Invalid address %s: %v", addr, err)
							continue
						}

						data = append(data, a.data[i]+":"+addr)
					} else {
						data = append(data, a.data[i])
					}
				}
				a.data = data

				err := subscribe(a.data)
				if err != nil {
					log.Printf("Failed to subscribe: %v", err)
					continue
				}

			case "unsubscribe":
				unsubPrompt.Options = currentSubs
				_ = survey.AskOne(unsubPrompt, &a.data, nil)

				err := unsubscribe(a.data)
				if err != nil {
					log.Printf("Failed to unsubscribe: %v", err)
					continue
				}

			case "ping the server":
				err := cl.Ping()
				if err != nil {
					log.Printf("Failed to ping the server: %v", err)
					continue
				}
				log.Println("Ping sent!")

			case "version":
				serverVer, err := cl.ServerVersion()
				if err != nil {
					log.Printf("Failed to get server version: %v", err)
					continue
				}
				log.Printf("Server version: %s\n", serverVer)

				clientSemVer := psclient.Version()
				serverSemVer := semver.NewSemver(serverVer.Major, serverVer.Minor, serverVer.Patch)
				if !semver.Compatible(clientSemVer, serverSemVer) {
					log.Printf("WARNING! Server version is %v, but client is version %v",
						serverSemVer, clientSemVer)
				}

			case "quit":
				cancel()
				os.Exit(0)

			default:
				log.Fatalf("invalid action")
				continue
			}
		}
	}()

	// Receive subscribed broadcast messages in an orderly fashion.
	for {
		msg := <-cl.Receive()
		if msg == nil {
			fmt.Printf("ReceiveMsg failed: %v", err)
			return
		}

		switch m := msg.Message.(type) {
		case *pstypes.ResponseMessage:
			log.Printf("%s request (ID=%d) success = %v. Data: %v",
				m.RequestEventId, m.RequestId, m.Success, m.Data)
		case *pstypes.Ver:
			log.Printf("Server Version: %v", m)
		case string:
			// generic "message"
			log.Printf("Message (%s): %s", msg.EventId, m)
		case int:
			// e.g. "ping"
			log.Printf("Message (%s): %d", msg.EventId, m)
		case *exptypes.WebsocketBlock:
			log.Printf("Message (%s): WebsocketBlock(hash=%s)", msg.EventId, m.Block.Hash)
		case *exptypes.MempoolShort:
			t := time.Unix(m.Time, 0)
			log.Printf("Message (%s): MempoolShort(numTx=%d, time=%v)",
				msg.EventId, m.NumAll, t)
		case *pstypes.TxList:
			log.Printf("Message (%s): TxList(len=%d)", msg.EventId, len(*m))
		case *pstypes.AddressMessage:
			log.Printf("Message (%s): AddressMessage(address=%s, txHash=%s)",
				msg.EventId, m.Address, m.TxHash)
		case *pstypes.HangUp:
			log.Printf("Hung up. Bye!")
			return
		default:
			log.Printf("Message of type %v unhandled.", msg.EventId)
		}
	}
}

func strInSlice(sl []string, str string) (bool, int) {
	for i, s := range sl {
		if s == str {
			return true, i
		}
	}
	return false, -1
}

// AnotInB returns strings in the slice sA that are not in slice sB.
func AnotInB(sA []string, sB []string) (AnotB []string) {
	for _, s := range sA {
		if found, _ := strInSlice(sB, s); found {
			continue
		}
		AnotB = append(AnotB, s)
	}
	return
}
