package ip

import (
	"net"

	"github.com/pkg/errors"
)

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
