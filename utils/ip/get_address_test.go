package ip

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_getClientIp(t *testing.T) {

	ipv4List, err := GetClientAllIpv4()
	require.Nil(t, err)
	fmt.Printf("ipv4:\v%v\n\n", ipv4List)
	ipv6List, err := GetClientAllIpv6()
	require.Nil(t, err)
	fmt.Printf("ipv6:\v%v\n\n", ipv6List)
}

func TestGetInterfaceIpv4(t *testing.T) {
	ipv4List, err := GetInterfaceIpv4("en0")
	require.Nil(t, err)
	fmt.Printf("ipv4:\v%v\n\n", ipv4List)

	ipv6List, err := GetInterfaceIpv6("en0")
	require.Nil(t, err)
	fmt.Printf("ipv6:\v%v\n\n", ipv6List)
}
