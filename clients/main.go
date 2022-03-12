package main

import (
	"errors"
	"github.com/sirupsen/logrus"
	"net"
)

func main()  {
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors: false,
		FullTimestamp: true,
	})
	logrus.Infof("client daemon starting...\nScan ipv4 addr")
	ipv4, err := getClientIpv4()
	if err != nil {
		panic(err)
	}
	logrus.Infof("Ipv4 addrs :%v", ipv4)
	logrus.Infof("program end.")
}

func getClientIpv4() ([]string ,error) {
	addrs, err := net.InterfaceAddrs()

	if err != nil {
		return nil, err
	}

	rets := make([]string, 0, len(addrs))

	for _, address := range addrs {
		// 检查ip地址判断是否回环地址
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				rets = append(rets, ipnet.IP.String())
			}
		}
	}
	if len(rets) == 0 {
		return nil, errors.New("Can not find the client ip address")
	}

	return rets, nil
}