/*
 * SPDX-License-Identifier: Apache-2.0
 */

package orderer

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/hyperledger-labs/microfab/internal/pkg/blocks"
	"github.com/hyperledger-labs/microfab/internal/pkg/util"
	"github.com/pkg/errors"
)

// Start starts the orderer.
func (o *Orderer) Start(timeout time.Duration) error {
	err := o.createDirectories()
	if err != nil {
		return err
	}
	dataDirectory := path.Join(o.directory, "data")
	logsDirectory := path.Join(o.directory, "logs")
	mspDirectory := path.Join(o.directory, "msp")
	tlsDirectory := path.Join(o.directory, "tls")
	err = util.CreateMSPDirectory(mspDirectory, o.identity)
	if err != nil {
		return err
	}
	clusterCertFile := path.Join(tlsDirectory, "cluster-cert.pem")
	clusterKeyFile := path.Join(tlsDirectory, "cluster-key.pem")
	if err := ioutil.WriteFile(clusterCertFile, o.clusterTLS.Certificate().Bytes(), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(clusterKeyFile, o.clusterTLS.PrivateKey().Bytes(), 0644); err != nil {
		return err
	}
	cmd := exec.Command("orderer", "start")
	cmd.Env = os.Environ()
	extraEnvs := []string{
		"FABRIC_LOGGING_SPEC=info",
		fmt.Sprintf("ORDERER_GENERAL_LOCALMSPDIR=%s", mspDirectory),
		fmt.Sprintf("ORDERER_GENERAL_LOCALMSPID=%s", o.mspID),
		"ORDERER_GENERAL_BOOTSTRAPMETHOD=none",
		"ORDERER_CHANNELPARTICIPATION_ENABLED=true",
		fmt.Sprintf("ORDERER_ADMIN_LISTENADDRESS=%s", o.AdminURL().Host),
		"ORDERER_ADMIN_TLS_ENABLED=false",
		fmt.Sprintf("ORDERER_GENERAL_CLUSTER_LISTENADDRESS=%s", o.ClusterHostname()),
		fmt.Sprintf("ORDERER_GENERAL_CLUSTER_LISTENPORT=%d", o.clusterPort),
		fmt.Sprintf("ORDERER_GENERAL_CLUSTER_SERVERCERTIFICATE=%s", clusterCertFile),
		fmt.Sprintf("ORDERER_GENERAL_CLUSTER_SERVERPRIVATEKEY=%s", clusterKeyFile),
		fmt.Sprintf("ORDERER_GENERAL_CLUSTER_CLIENTCERTIFICATE=%s", clusterCertFile),
		fmt.Sprintf("ORDERER_GENERAL_CLUSTER_CLIENTPRIVATEKEY=%s", clusterKeyFile),
		fmt.Sprintf("ORDERER_FILELEDGER_LOCATION=%s", dataDirectory),
		fmt.Sprintf("ORDERER_CONSENSUS_WALDIR=%s", path.Join(dataDirectory, "etcdraft", "wal")),
		fmt.Sprintf("ORDERER_CONSENSUS_SNAPDIR=%s", path.Join(dataDirectory, "etcdraft", "snapshot")),
		"ORDERER_METRICS_PROVIDER=prometheus",
		"ORDERER_GENERAL_LISTENADDRESS=0.0.0.0",
		fmt.Sprintf("ORDERER_GENERAL_LISTENPORT=%d", o.apiPort),
		fmt.Sprintf("ORDERER_OPERATIONS_LISTENADDRESS=0.0.0.0:%d", o.operationsPort),
	}
	if o.tls != nil {
		certFile := path.Join(tlsDirectory, "cert.pem")
		keyFile := path.Join(tlsDirectory, "key.pem")
		caFile := path.Join(tlsDirectory, "ca.pem")
		extraEnvs = append(extraEnvs,
			"ORDERER_GENERAL_TLS_ENABLED=true",
			fmt.Sprintf("ORDERER_GENERAL_TLS_CERTIFICATE=%s", certFile),
			fmt.Sprintf("ORDERER_GENERAL_TLS_PRIVATEKEY=%s", keyFile),
			fmt.Sprintf("ORDERER_GENERAL_TLS_ROOTCAS=%s", caFile),
			"ORDERER_OPERATIONS_TLS_ENABLED=true",
			fmt.Sprintf("ORDERER_OPERATIONS_TLS_CERTIFICATE=%s", certFile),
			fmt.Sprintf("ORDERER_OPERATIONS_TLS_PRIVATEKEY=%s", keyFile),
		)
		if err := ioutil.WriteFile(certFile, o.tls.Certificate().Bytes(), 0644); err != nil {
			return err
		}
		if err := ioutil.WriteFile(keyFile, o.tls.PrivateKey().Bytes(), 0644); err != nil {
			return err
		}
		if err := ioutil.WriteFile(caFile, o.tls.CA().Bytes(), 0644); err != nil {
			return err
		}
	}
	cmd.Env = append(cmd.Env, extraEnvs...)
	cmd.Stdin = nil
	logFile, err := os.OpenFile(path.Join(logsDirectory, "orderer.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return errors.WithMessage(err, "failed to open orderer log file")
	}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return errors.WithMessage(err, "failed to open pipe")
	}
	go func() {
		reader := bufio.NewReader(pipe)
		scanner := bufio.NewScanner(reader)
		scanner.Split(bufio.ScanLines)
		id := "orderer"
		logger := log.New(os.Stdout, fmt.Sprintf("[%16s] ", id), 0)
		for scanner.Scan() {
			logger.Println(scanner.Text())
			logFile.WriteString(scanner.Text())
		}
		pipe.Close()
		logFile.Close()
	}()
	cmd.Stderr = cmd.Stdout
	err = cmd.Start()
	if err != nil {
		return errors.WithMessage(err, "failed to start orderer")
	}
	o.command = cmd
	errchan := make(chan error, 1)
	go func() {
		err = cmd.Wait()
		if err != nil {
			errchan <- err
		}
	}()
	timeoutCh := time.After(timeout)
	tick := time.Tick(250 * time.Millisecond)
	for {
		select {
		case <-timeoutCh:
			o.Stop()
			return errors.New("timeout whilst waiting for orderer to start")
		case err := <-errchan:
			o.Stop()
			return errors.WithMessage(err, "failed to start orderer")
		case <-tick:
			if o.hasStarted() {
				return nil
			}
		}
	}
}

// Stop stops the orderer.
func (o *Orderer) Stop() error {
	if o.command != nil {
		err := o.command.Process.Kill()
		if err != nil {
			return errors.WithMessage(err, "failed to stop orderer")
		}
		o.command = nil
	}
	return nil
}

// WaitForLeader waits until the specified channel has a Raft leader; until then, the orderer refuses to deliver its blocks.
func (o *Orderer) WaitForLeader(channel string, timeout time.Duration) error {
	connection, err := Connect(o, o.mspID, o.identity)
	if err != nil {
		return err
	}
	defer connection.Close()
	deadline := time.Now().Add(timeout)
	for {
		_, err := blocks.GetNewestBlock(connection, channel)
		if err == nil {
			return nil
		} else if time.Now().After(deadline) {
			return errors.WithMessagef(err, "timeout whilst waiting for a leader of channel %s", channel)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (o *Orderer) createDirectories() error {
	directories := []string{
		o.directory,
		path.Join(o.directory, "data"),
		path.Join(o.directory, "logs"),
		path.Join(o.directory, "msp"),
		path.Join(o.directory, "tls"),
	}
	for _, dir := range directories {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
	}
	return nil
}

func (o *Orderer) hasStarted() bool {
	cli := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	resp, err := cli.Get(fmt.Sprintf("%s/healthz", o.OperationsURL(true)))
	if err != nil {
		log.Printf("error waiting for orderer: %v\n", err)
		return false
	}
	return resp.StatusCode == 200
}
