/*
 * SPDX-License-Identifier: Apache-2.0
 */

package channel

import (
	"github.com/golang/protobuf/proto"
	"github.com/hyperledger-labs/microfab/internal/pkg/identity"
	"github.com/hyperledger-labs/microfab/internal/pkg/organization"
	"github.com/hyperledger-labs/microfab/internal/pkg/protoutil"
	"github.com/hyperledger-labs/microfab/internal/pkg/txid"
	"github.com/hyperledger-labs/microfab/internal/pkg/util"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric-protos-go/orderer/etcdraft"
)

// Consenter is the channel's one etcdraft node; the CA of its TLS identity becomes every organization's TLS root.
type Consenter struct {
	Host string
	Port uint32
	TLS  *identity.Identity
}

// NewGenesisBlock builds block 0 of an application channel, for an orderer to join without a system channel.
func NewGenesisBlock(channel string, ordererOrganization *organization.Organization, ordererEndpoint string, consenter Consenter, endorsingOrganizations []*organization.Organization, opts ...Option) (*common.Block, error) {
	ordererOrgGroup, err := protoutil.BuildConfigGroupFromOrganization(ordererOrganization, consenter.TLS)
	if err != nil {
		return nil, err
	}
	ordererOrgGroup.ModPolicy = "Admins"
	ordererOrgGroup.Values["Endpoints"] = configValue(&common.OrdererAddresses{Addresses: []string{ordererEndpoint}})
	endorsingGroups := map[string]*common.ConfigGroup{}
	for _, endorsingOrganization := range endorsingOrganizations {
		endorsingGroups[endorsingOrganization.MSPID()], err = protoutil.BuildConfigGroupFromOrganization(endorsingOrganization, consenter.TLS)
		if err != nil {
			return nil, err
		}
	}
	operation := &channelOperation{
		config: &common.Config{
			ChannelGroup: &common.ConfigGroup{
				Groups: map[string]*common.ConfigGroup{
					"Application": {
						Groups:    endorsingGroups,
						ModPolicy: "Admins",
						Policies: map[string]*common.ConfigPolicy{
							"Admins":               anyPolicy("Admins"),
							"Endorsement":          anyPolicy("Endorsement"),
							"LifecycleEndorsement": anyPolicy("Endorsement"),
							"Readers":              anyPolicy("Readers"),
							"Writers":              anyPolicy("Writers"),
						},
						Values: map[string]*common.ConfigValue{
							"Capabilities": configValue(capabilities("V2_5")),
						},
					},
					"Orderer": {
						Groups:    map[string]*common.ConfigGroup{ordererOrganization.MSPID(): ordererOrgGroup},
						ModPolicy: "Admins",
						Policies: map[string]*common.ConfigPolicy{
							"Admins":          anyPolicy("Admins"),
							"BlockValidation": anyPolicy("Writers"),
							"Readers":         anyPolicy("Readers"),
							"Writers":         anyPolicy("Writers"),
						},
						Values: map[string]*common.ConfigValue{
							"BatchSize": configValue(&orderer.BatchSize{
								AbsoluteMaxBytes:  103809024,
								MaxMessageCount:   10,
								PreferredMaxBytes: 524288,
							}),
							"BatchTimeout":        configValue(&orderer.BatchTimeout{Timeout: "100ms"}),
							"Capabilities":        configValue(capabilities("V2_0")),
							"ChannelRestrictions": configValue(&orderer.ChannelRestrictions{}),
							"ConsensusType": configValue(&orderer.ConsensusType{
								Metadata: util.MarshalOrPanic(&etcdraft.ConfigMetadata{
									Consenters: []*etcdraft.Consenter{
										{
											Host:          consenter.Host,
											Port:          consenter.Port,
											ClientTlsCert: consenter.TLS.Certificate().Bytes(),
											ServerTlsCert: consenter.TLS.Certificate().Bytes(),
										},
									},
									Options: &etcdraft.Options{
										TickInterval:         "2500ms",
										ElectionTick:         5,
										HeartbeatTick:        1,
										MaxInflightBlocks:    5,
										SnapshotIntervalSize: 1048576,
									},
								}),
								State: orderer.ConsensusType_STATE_NORMAL,
								Type:  "etcdraft",
							}),
						},
					},
				},
				ModPolicy: "Admins",
				Policies: map[string]*common.ConfigPolicy{
					"Admins":  anyPolicy("Admins"),
					"Readers": anyPolicy("Readers"),
					"Writers": anyPolicy("Writers"),
				},
				Values: map[string]*common.ConfigValue{
					"BlockDataHashingStructure": configValue(&common.BlockDataHashingStructure{Width: 4294967295}),
					"Capabilities":              configValue(capabilities("V2_0")),
					"HashingAlgorithm":          configValue(&common.HashingAlgorithm{Name: "SHA256"}),
				},
			},
		},
		mspID:    ordererOrganization.MSPID(),
		identity: ordererOrganization.Admin(),
	}
	for _, opt := range opts {
		err := opt(operation)
		if err != nil {
			return nil, err
		}
	}
	txID := txid.New(operation.mspID, operation.identity)
	header := protoutil.BuildHeader(common.HeaderType_CONFIG, channel, txID)
	payload := protoutil.BuildPayload(header, &common.ConfigEnvelope{Config: operation.config})
	envelope := protoutil.BuildEnvelope(payload, operation.identity)
	return protoutil.BuildGenesisBlock(envelope), nil
}

func configValue(value proto.Message) *common.ConfigValue {
	return &common.ConfigValue{
		ModPolicy: "Admins",
		Value:     util.MarshalOrPanic(value),
	}
}

func capabilities(level string) *common.Capabilities {
	return &common.Capabilities{
		Capabilities: map[string]*common.Capability{
			level: {},
		},
	}
}

func anyPolicy(subPolicy string) *common.ConfigPolicy {
	return protoutil.BuildImplicitMetaConfigPolicy(common.ImplicitMetaPolicy_ANY, subPolicy)
}
