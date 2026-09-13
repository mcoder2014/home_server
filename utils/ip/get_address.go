package ip

import (
	"net"

	"github.com/pkg/errors"
)

// GetClientAllIpv4 遍历本机接口地址，收集所有非回环 IPv4 地址。
func GetClientAllIpv4() ([]string, error) {
	addrs, err := net.InterfaceAddrs()

	if err != nil {
		return nil, err
	}

	var res []string

	for _, address := range addrs {
		// 检查ip地址判断是否回环地址
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				res = append(res, ipnet.IP.String())
			}
		}
	}

	return res, nil
}

func GetClientAllIpv6() ([]string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	var res []string
	for _, address := range addrs {
		// 检查ip地址判断是否回环地址
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() == nil {
				res = append(res, ipnet.IP.String())
			}
		}
	}
	return res, nil
}

// GetInterfaceIpv4 按网卡名称查找非回环 IPv4 地址，并在接口或地址读取失败时附带网卡信息。
func GetInterfaceIpv4(name string) ([]string, error) {
	netInterface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, errors.Wrapf(err, "not found interface: %v", name)
	}

	var res []string
	addrList, err := netInterface.Addrs()
	if err != nil {
		return nil, errors.Wrapf(err, "interface %v not found addrs", name)
	}
	for _, addr := range addrList {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				res = append(res, ipNet.IP.String())
			}
		}
	}
	return res, nil
}

// GetInterfaceIpv6 按网卡名称收集非回环且不能转换为 IPv4 的地址，保留接口查询错误。
func GetInterfaceIpv6(name string) ([]string, error) {
	netInterface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, errors.Wrapf(err, "not found interface: %v", name)
	}

	var res []string
	addrList, err := netInterface.Addrs()
	if err != nil {
		return nil, errors.Wrapf(err, "interface %v not found addrs", name)
	}
	for _, addr := range addrList {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() == nil {
				res = append(res, ipNet.IP.String())
			}
		}
	}
	return res, nil
}
