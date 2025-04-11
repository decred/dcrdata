// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package main

import (
	"flag"
	"os"
	"testing"

	"github.com/decred/dcrdata/exchanges/v3"
	dcrrates "github.com/decred/dcrdata/exchanges/v3/ratesproto"
)

func TestAddDeleteClient(t *testing.T) {
	server := NewRateServer("", nil)
	_, sid := server.addClient(nil, nil)
	if len(server.clients) != 1 {
		t.Fatalf("client length after addClient: %d, expecting 1", len(server.clients))
	}
	server.deleteClient(sid)
	if len(server.clients) != 0 {
		t.Fatalf("client length after deleteClient %d, expecting 0", len(server.clients))
	}
}

type clientStub struct {
	dcrExchanges map[string]map[exchanges.CurrencyPair]*exchanges.ExchangeState
}

func (c *clientStub) SendExchangeUpdate(update *dcrrates.ExchangeRateUpdate) error {
	if c.dcrExchanges == nil {
		c.dcrExchanges = make(map[string]map[exchanges.CurrencyPair]*exchanges.ExchangeState)
	}

	if c.dcrExchanges[update.Token] == nil {
		c.dcrExchanges[update.Token] = make(map[exchanges.CurrencyPair]*exchanges.ExchangeState)
	}

	currencyPair := exchanges.CurrencyPair(update.CurrencyPair)
	c.dcrExchanges[update.Token][currencyPair] = &exchanges.ExchangeState{
		BaseState: exchanges.BaseState{
			Price:      update.GetPrice(),
			BaseVolume: update.GetBaseVolume(),
			Volume:     update.GetVolume(),
			Change:     update.GetChange(),
			Stamp:      update.GetStamp(),
		},
	}

	return nil
}

func (c *clientStub) Stream() GRPCStream {
	return nil
}

func TestSendStateList(t *testing.T) {
	updates := make(map[string]map[exchanges.CurrencyPair]*exchanges.ExchangeState)
	currencyPair := exchanges.CurrencyPairDCRBTC
	xcToken := "DummyToken"
	updates[xcToken] = map[exchanges.CurrencyPair]*exchanges.ExchangeState{
		currencyPair: {},
	}

	client := &clientStub{}
	err := sendStateList(client, updates)
	if err != nil {
		t.Fatalf("Error sending exchange states: %v", err)
	}

	if client.dcrExchanges[xcToken][currencyPair] == nil {
		t.Fatalf("expected at least one exchange state for currency pair %s", currencyPair)
	}
}

type certWriterStub struct {
	lengths map[string]int
}

func (w certWriterStub) WriteCertificate(certPath string, cert []byte) error {
	w.lengths[certPath] = len(cert)
	return nil
}

// TestDefaultAltDNSNames ensures that there are no additional hostnames added
// by default during the configuration load phase.
func TestDefaultAltDNSNames(t *testing.T) {
	// Parse the -test.* flags before removing them from the command line
	// arguments list, which we do to allow go-flags to succeed.
	flag.Parse()
	os.Args = os.Args[:1]

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("Failed to load dcrd config: %s", err)
	}
	if len(cfg.AltDNSNames) != 0 {
		t.Fatalf("Invalid default value for altdnsnames: %s", cfg.AltDNSNames)
	}
}

func TestGenerateRPCKeyPair(t *testing.T) {
	writer := certWriterStub{lengths: make(map[string]int)}
	_, err := generateRPCKeyPair("./cert", "./key", []string(nil), writer)
	if err != nil {
		t.Fatalf("Error generating TLS certificate: %v", err)
	}
	certLen, ok := writer.lengths["./cert"]
	if !ok {
		t.Fatal("Dummy certificate path index not found")
	}
	if certLen == 0 {
		t.Fatal("Zero length certificate")
	}
	keyLen, ok := writer.lengths["./key"]
	if !ok {
		t.Fatal("Dummy key path index not found")
	}
	if keyLen == 0 {
		t.Fatal("Zero length key")
	}
}
