// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package rpcutils

import (
	"fmt"
	"io/ioutil"

	"github.com/dcrdata/dcrdata/semver"
	"github.com/decred/dcrrpcclient"
)

var requiredChainServerAPI = semver.NewSemver(2, 0, 0)

func ConnectNodeRPC(host, user, pass, cert string, disableTLS bool,
	ntfnHandlers ...*dcrrpcclient.NotificationHandlers) (*dcrrpcclient.Client, semver.Semver, error) {
	var dcrdCerts []byte
	var err error
	var nodeVer semver.Semver
	if !disableTLS {
		dcrdCerts, err = ioutil.ReadFile(cert)
		if err != nil {
			log.Errorf("Failed to read dcrd cert file at %s: %s\n",
				cert, err.Error())
			return nil, nodeVer, err
		}
	}

	log.Debugf("Attempting to connect to dcrd RPC %s as user %s "+
		"using certificate located in %s",
		host, user, cert)

	connCfgDaemon := &dcrrpcclient.ConnConfig{
		Host:         host,
		Endpoint:     "ws", // websocket
		User:         user,
		Pass:         pass,
		Certificates: dcrdCerts,
		DisableTLS:   disableTLS,
	}

	var ntfnHdlrs *dcrrpcclient.NotificationHandlers
	if len(ntfnHandlers) > 0 {
		if len(ntfnHandlers) > 1 {
			return nil, nodeVer, fmt.Errorf("Invalid notification handler argument.")
		}
		ntfnHdlrs = ntfnHandlers[0]
	}
	dcrdClient, err := dcrrpcclient.New(connCfgDaemon, ntfnHdlrs)
	if err != nil {
		return nil, nodeVer, fmt.Errorf("Failed to start dcrd RPC client: %s\n", err.Error())
	}

	// Ensure the RPC server has a compatible API version.
	ver, err := dcrdClient.Version()
	if err != nil {
		log.Error("Unable to get RPC version: ", err)
		return nil, nodeVer, fmt.Errorf("Unable to get node RPC version")
	}

	dcrdVer := ver["dcrdjsonrpcapi"]
	nodeVer = semver.NewSemver(dcrdVer.Major, dcrdVer.Minor, dcrdVer.Patch)

	if !semver.SemverCompatible(requiredChainServerAPI, nodeVer) {
		return nil, nodeVer, fmt.Errorf("Node JSON-RPC server does not have "+
			"a compatible API version. Advertises %v but require %v",
			nodeVer, requiredChainServerAPI)
	}

	return dcrdClient, nodeVer, nil
}
