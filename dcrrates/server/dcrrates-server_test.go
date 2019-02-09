// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package main

import (
	"testing"

	"github.com/decred/dcrdata/v4/dcrrates"
	"github.com/decred/dcrdata/v4/exchanges"
)

func TestAddDeleteClient(t *testing.T) {
	server := NewRateServer("", nil)
	_, sid := server.AddClient(nil, nil)
	if len(server.clients) != 1 {
		t.Fatalf("client length after AddClient: %d, expecting 1", len(server.clients))
	}
	server.DeleteClient(sid)
	if len(server.clients) != 0 {
		t.Fatalf("client length after DeleteClient %d, expecting 0", len(server.clients))
	}
}

type clientStub struct{}

func (clientStub) SendExchangeUpdate(*dcrrates.ExchangeRateUpdate) error {
	return nil
}

func (clientStub) Stream() GRPCStream {
	return nil
}

func TestSendStateList(t *testing.T) {
	updates := make(map[string]*exchanges.ExchangeState)
	updates["DummyToken"] = &exchanges.ExchangeState{}
	err := sendStateList(clientStub{}, updates)
	if err != nil {
		t.Fatalf("Error sending exchange states: %v", err)
	}
}

type certWriterStub struct {
	lengths map[string]int
}

func (w certWriterStub) WriteCertificate(certPath string, cert []byte) error {
	w.lengths[certPath] = len(cert)
	return nil
}

func TestGenerateRPCKeyPair(t *testing.T) {
	writer := certWriterStub{lengths: make(map[string]int)}
	_, err := generateRPCKeyPair("./cert", "./key", writer)
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
