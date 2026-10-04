/*
 * SPDX-License-Identifier: Apache-2.0
 */

package channel

import (
	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger-labs/microfab/internal/pkg/util"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/peer"
)

// Option configures NewGenesisBlock.
type Option func(*common.Config) error

// AddAnchorPeer adds the specified anchor peer to the channel.
func AddAnchorPeer(mspID string, hostname string, port int32) Option {
	return func(config *common.Config) error {
		msp, ok := config.GetChannelGroup().Groups["Application"].Groups[mspID]
		if !ok {
			return fmt.Errorf("The channel does not contain an MSP with ID %s", mspID)
		}
		cv, ok := msp.Values["AnchorPeers"]
		if !ok {
			cv = configValue(&peer.AnchorPeers{})
			msp.Values["AnchorPeers"] = cv
		}
		aps := &peer.AnchorPeers{}
		proto.Unmarshal(cv.Value, aps)
		aps.AnchorPeers = append(aps.AnchorPeers, &peer.AnchorPeer{
			Host: hostname,
			Port: port,
		})
		cv.Value = util.MarshalOrPanic(aps)
		return nil
	}
}

// WithCapabilityLevel set the specified capability level for the channel.
func WithCapabilityLevel(capabilityLevel string) Option {
	return func(config *common.Config) error {
		config.GetChannelGroup().Groups["Application"].Values["Capabilities"] = configValue(capabilities(capabilityLevel))
		return nil
	}
}
