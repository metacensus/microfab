/*
 * SPDX-License-Identifier: Apache-2.0
 */

package channel_test

import (
	"crypto/sha256"
	"maps"
	"slices"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger-labs/microfab/internal/pkg/channel"
	"github.com/hyperledger-labs/microfab/internal/pkg/identity"
	"github.com/hyperledger-labs/microfab/internal/pkg/organization"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/msp"
	"github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric-protos-go/orderer/etcdraft"
	"github.com/hyperledger/fabric-protos-go/peer"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func unmarshal(data []byte, message proto.Message) {
	ExpectWithOffset(1, proto.Unmarshal(data, message)).To(Succeed())
}

func getConfig(block *common.Block) *common.Config {
	envelope := &common.Envelope{}
	unmarshal(block.Data.Data[0], envelope)
	payload := &common.Payload{}
	unmarshal(envelope.Payload, payload)
	configEnvelope := &common.ConfigEnvelope{}
	unmarshal(payload.Data, configEnvelope)
	return configEnvelope.Config
}

func getCapabilityLevels(group *common.ConfigGroup) []string {
	capabilities := &common.Capabilities{}
	unmarshal(group.Values["Capabilities"].Value, capabilities)
	return slices.Collect(maps.Keys(capabilities.Capabilities))
}

func getMSPConfig(group *common.ConfigGroup) *msp.FabricMSPConfig {
	mspConfig := &msp.MSPConfig{}
	unmarshal(group.Values["MSP"].Value, mspConfig)
	fabricMSPConfig := &msp.FabricMSPConfig{}
	unmarshal(mspConfig.Config, fabricMSPConfig)
	return fabricMSPConfig
}

var _ = Describe("channel.NewGenesisBlock()", func() {

	var ordererOrganization, org1, org2 *organization.Organization
	var consenter channel.Consenter
	var block *common.Block
	var config *common.Config

	BeforeEach(func() {
		var err error
		ordererOrganization, err = organization.New("Orderer", nil, nil)
		Expect(err).NotTo(HaveOccurred())
		org1, err = organization.New("Org1", nil, nil)
		Expect(err).NotTo(HaveOccurred())
		org2, err = organization.New("Org2", nil, nil)
		Expect(err).NotTo(HaveOccurred())
		tlsCA, err := identity.New("TLS CA", identity.WithIsCA(true))
		Expect(err).NotTo(HaveOccurred())
		tls, err := identity.New("Orderer Cluster", identity.UsingSigner(tlsCA))
		Expect(err).NotTo(HaveOccurred())
		consenter = channel.Consenter{Host: "localhost", Port: 7053, TLS: tls}
		block, err = channel.NewGenesisBlock(
			"mychannel",
			ordererOrganization,
			"orderer.example.com:7050",
			consenter,
			nil,
			[]*organization.Organization{org1, org2},
			channel.AddAnchorPeer(org1.MSPID(), "peer0.org1.example.com", 7051),
			channel.AddAnchorPeer(org2.MSPID(), "peer0.org2.example.com", 8051),
			channel.WithCapabilityLevel("V2_0"),
		)
		Expect(err).NotTo(HaveOccurred())
		config = getConfig(block)
	})

	It("builds block 0 whose data hash covers its data", func() {
		Expect(block.Header.Number).To(BeZero())
		dataHash := sha256.Sum256(block.Data.Data[0])
		Expect(block.Header.DataHash).To(Equal(dataHash[:]))
	})

	It("builds an orderer group with the ordering organization and one etcdraft consenter", func() {
		ordererGroup := config.ChannelGroup.Groups["Orderer"]
		Expect(ordererGroup.Groups).To(HaveLen(1))
		ordererOrgGroup := ordererGroup.Groups[ordererOrganization.MSPID()]
		Expect(ordererOrgGroup.ModPolicy).To(Equal("Admins"))
		mspConfig := getMSPConfig(ordererOrgGroup)
		Expect(mspConfig.Name).To(Equal(ordererOrganization.MSPID()))
		Expect(mspConfig.TlsRootCerts).To(Equal([][]byte{consenter.TLS.CA().Bytes()}))
		endpoints := &common.OrdererAddresses{}
		unmarshal(ordererOrgGroup.Values["Endpoints"].Value, endpoints)
		Expect(endpoints.Addresses).To(ConsistOf("orderer.example.com:7050"))

		batchSize := &orderer.BatchSize{}
		unmarshal(ordererGroup.Values["BatchSize"].Value, batchSize)
		Expect(batchSize.MaxMessageCount).To(BeEquivalentTo(10))
		batchTimeout := &orderer.BatchTimeout{}
		unmarshal(ordererGroup.Values["BatchTimeout"].Value, batchTimeout)
		Expect(batchTimeout.Timeout).To(Equal("100ms"))
		Expect(ordererGroup.Policies).To(HaveKey("BlockValidation"))

		consensusType := &orderer.ConsensusType{}
		unmarshal(ordererGroup.Values["ConsensusType"].Value, consensusType)
		Expect(consensusType.Type).To(Equal("etcdraft"))
		metadata := &etcdraft.ConfigMetadata{}
		unmarshal(consensusType.Metadata, metadata)
		Expect(metadata.Consenters).To(HaveLen(1))
		Expect(metadata.Consenters[0].Host).To(Equal("localhost"))
		Expect(metadata.Consenters[0].Port).To(BeEquivalentTo(7053))
		Expect(metadata.Consenters[0].ServerTlsCert).To(Equal(consenter.TLS.Certificate().Bytes()))
		Expect(metadata.Consenters[0].ClientTlsCert).To(Equal(consenter.TLS.Certificate().Bytes()))
	})

	It("builds an application group with each organization's MSP and anchor peers", func() {
		applicationGroup := config.ChannelGroup.Groups["Application"]
		Expect(applicationGroup.Groups).To(HaveLen(2))
		for org, anchorPeer := range map[*organization.Organization]*peer.AnchorPeer{
			org1: {Host: "peer0.org1.example.com", Port: 7051},
			org2: {Host: "peer0.org2.example.com", Port: 8051},
		} {
			orgGroup := applicationGroup.Groups[org.MSPID()]
			mspConfig := getMSPConfig(orgGroup)
			Expect(mspConfig.Name).To(Equal(org.MSPID()))
			Expect(mspConfig.RootCerts).To(Equal([][]byte{org.CA().Certificate().Bytes()}))
			Expect(mspConfig.TlsRootCerts).To(BeEmpty())
			anchorPeers := &peer.AnchorPeers{}
			unmarshal(orgGroup.Values["AnchorPeers"].Value, anchorPeers)
			Expect(anchorPeers.AnchorPeers).To(HaveLen(1))
			Expect(proto.Equal(anchorPeers.AnchorPeers[0], anchorPeer)).To(BeTrue())
		}
	})

	It("gives each endorsing organization the TLS root of the TLS identity, when given one", func() {
		tlsCA, err := identity.New("TLS CA", identity.WithIsCA(true))
		Expect(err).NotTo(HaveOccurred())
		tls, err := identity.New("TLS", identity.UsingSigner(tlsCA))
		Expect(err).NotTo(HaveOccurred())
		block, err := channel.NewGenesisBlock("mychannel", ordererOrganization, "orderer.example.com:7050", consenter, tls, []*organization.Organization{org1})
		Expect(err).NotTo(HaveOccurred())
		config := getConfig(block)
		Expect(getMSPConfig(config.ChannelGroup.Groups["Application"].Groups[org1.MSPID()]).TlsRootCerts).To(Equal([][]byte{tlsCA.Certificate().Bytes()}))
		Expect(getMSPConfig(config.ChannelGroup.Groups["Orderer"].Groups[ordererOrganization.MSPID()]).TlsRootCerts).To(Equal([][]byte{consenter.TLS.CA().Bytes()}))
	})

	It("sets the channel and orderer capabilities to V2_0 and the application capability to the configured level", func() {
		Expect(getCapabilityLevels(config.ChannelGroup)).To(ConsistOf("V2_0"))
		Expect(getCapabilityLevels(config.ChannelGroup.Groups["Orderer"])).To(ConsistOf("V2_0"))
		Expect(getCapabilityLevels(config.ChannelGroup.Groups["Application"])).To(ConsistOf("V2_0"))
	})

	It("omits the consortium", func() {
		Expect(config.ChannelGroup.Values).NotTo(HaveKey("Consortium"))
		Expect(config.ChannelGroup.Groups).NotTo(HaveKey("Consortiums"))
	})

})
