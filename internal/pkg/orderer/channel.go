/*
 * SPDX-License-Identifier: Apache-2.0
 */

package orderer

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"mime/multipart"
	"net/http"

	"github.com/hyperledger-labs/microfab/internal/pkg/util"
	"github.com/hyperledger/fabric-protos-go/common"
)

// JoinChannel asks the orderer to join the channel that the specified genesis block starts.
func (o *Orderer) JoinChannel(block *common.Block) error {
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	part, err := form.CreateFormFile("config-block", "config.block")
	if err != nil {
		return err
	}
	if _, err := part.Write(util.MarshalOrPanic(block)); err != nil {
		return err
	}
	if err := form.Close(); err != nil {
		return err
	}
	response, err := http.Post(fmt.Sprintf("%s/participation/v1/channels", o.AdminURL()), form.FormDataContentType(), body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		message, _ := ioutil.ReadAll(response.Body)
		return fmt.Errorf("Bad channel participation response: status %s, message %s", response.Status, message)
	}
	return nil
}
