package wallet

import (
    "fmt"
    "log"
    "io/ioutil"
    "net/http"
    "encoding/json"

    "github.com/decred/dcrrpcclient"
)
type (
    Wallet struct {
        client *dcrrpcclient.Client
    }
)

func NewWalletClient(host, user, pass, cert string, disableTLS bool) *Wallet {
    return &Wallet{
        client: connectWalletRPC(host, user, pass, cert, disableTLS),
    }
}

func connectWalletRPC(host, user, pass, cert string, disableTLS bool) *dcrrpcclient.Client {
    ntfnHandlers := dcrrpcclient.NotificationHandlers{}

    fmt.Println(cert)

    certs, err := ioutil.ReadFile(cert)
    if err != nil {
        log.Fatal(err)
    }

    //Connect to local dcrwallet RPC server using websockets
    connCfg := &dcrrpcclient.ConnConfig{
		Host:         host,
		Endpoint:     "ws",
		User:         user,
		Pass:         pass,
		Certificates: certs,
        DisableTLS:   disableTLS,
	}
	client, err := dcrrpcclient.New(connCfg, &ntfnHandlers)
	if err != nil {
		log.Fatal(err)
	}
    return client

}


func (scope *Wallet) GetUnspent(w http.ResponseWriter, r *http.Request) {
    unspent, err := scope.client.ListUnspent()
    if err != nil {
        log.Fatal(err)
    }
    scope.json(w, unspent)
}

func (scope *Wallet) GetAccounts(w http.ResponseWriter, r *http.Request) {
    accounts, err := scope.client.ListAccounts()
    if err != nil {
        log.Fatal(err)
    }

    scope.json(w, accounts)
}

func (scope *Wallet) GetBalance(w http.ResponseWriter, r *http.Request) {
    balance, err := scope.client.GetBalance("*")
    if err != nil {
        log.Fatal(err)
    }
    scope.json(w, balance)
}

func (scope *Wallet) GetTransactions(w http.ResponseWriter, r *http.Request) {
    transactions, err := scope.client.ListTransactions("*")
    if err != nil {
        log.Fatal(err)
    }
    scope.json(w, transactions)
}


func (scope *Wallet) json(w http.ResponseWriter, data interface{}) {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    d,err := json.Marshal(data)

    if err == nil {
        w.Write(d)
    }
}
