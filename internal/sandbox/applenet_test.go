package sandbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const defaultNetwork = `[
  {
    "configuration" : { "mode" : "nat", "name" : "default",
      "plugin" : "container-network-vmnet" },
    "id" : "default",
    "status" : {
      "ipv4Gateway" : "192.168.64.1",
      "ipv4Subnet" : "192.168.64.0/24",
      "ipv6Subnet" : "fd09:bacc:5fa:eb01::/64"
    }
  }
]`

func TestTheGatewayIsReadFromTheDefaultNetwork(t *testing.T) {
	assert.Equal(t, "192.168.64.1", parseAppleGateway([]byte(defaultNetwork)))
}

func TestAnUnreadableNetworkGivesNoHost(t *testing.T) {
	for _, out := range []string{"", "[]", "not json", `[{"status":{}}]`, `{"status":{}}`} {
		assert.Empty(t, parseAppleGateway([]byte(out)), out)
	}
}
