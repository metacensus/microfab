/*
 * SPDX-License-Identifier: Apache-2.0
 */

package peer

import (
	"github.com/hyperledger/fabric-protos-go/gateway"
)

// Gateway returns a client for the peer's Fabric Gateway service.
func (c *Connection) Gateway() gateway.GatewayClient {
	return gateway.NewGatewayClient(c.clientConn)
}
