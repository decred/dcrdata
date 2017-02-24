// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

import (
	"fmt"
	"io/ioutil"

	"github.com/dcrdata/dcrdata/semver"
	"github.com/decred/dcrrpcclient"
)

var requiredChainServerAPI = semver.Semver{major: 2, minor: 0, patch: 0}

func ConnectNodeRPC(host, user, pass, cert string, disableTLS bool) (*dcrrpcclient.Client, semver, error) {
	var dcrdCerts []byte
	var err error
	var nodeVer semver.Semver
	if !cfg.DisableDaemonTLS {
		dcrdCerts, err = ioutil.ReadFile(cfg.DcrdCert)
		if err != nil {
			log.Errorf("Failed to read dcrd cert file at %s: %s\n",
				cfg.DcrdCert, err.Error())
			return nil, nodeVer, err
		}
	}

	log.Debugf("Attempting to connect to dcrd RPC %s as user %s "+
		"using certificate located in %s",
		cfg.DcrdServ, cfg.DcrdUser, cfg.DcrdCert)

	connCfgDaemon := &dcrrpcclient.ConnConfig{
		Host:         cfg.DcrdServ,
		Endpoint:     "ws", // websocket
		User:         cfg.DcrdUser,
		Pass:         cfg.DcrdPass,
		Certificates: dcrdCerts,
		DisableTLS:   cfg.DisableDaemonTLS,
	}

	ntfnHandlers := getNodeNtfnHandlers(cfg)
	dcrdClient, err := dcrrpcclient.New(connCfgDaemon, ntfnHandlers)
	if err != nil {
		log.Errorf("Failed to start dcrd RPC client: %s\n", err.Error())
		return nil, nodeVer, err
	}

	// Ensure the RPC server has a compatible API version.
	ver, err := dcrdClient.Version()
	if err != nil {
		log.Error("Unable to get RPC version: ", err)
		return nil, nodeVer, fmt.Errorf("Unable to get node RPC version")
	}

	dcrdVer := ver["dcrdjsonrpcapi"]
	nodeVer = semver.Semver{dcrdVer.Major, dcrdVer.Minor, dcrdVer.Patch}

	if !semver.SemverCompatible(requiredChainServerAPI, nodeVer) {
		return nil, nodeVer, fmt.Errorf("Node JSON-RPC server does not have "+
			"a compatible API version. Advertises %v but require %v",
			nodeVer, requiredChainServerAPI)
	}

	return dcrdClient, nodeVer, nil
}
