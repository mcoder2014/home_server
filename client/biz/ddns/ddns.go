package ddns

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go"
	"github.com/mcoder2014/home_server/client/config"
	"github.com/mcoder2014/home_server/client/rpc"
	myErrors "github.com/mcoder2014/home_server/errors"
	serverRpc "github.com/mcoder2014/home_server/rpc"
	"github.com/mcoder2014/home_server/utils"
	"github.com/mcoder2014/home_server/utils/log"
	"github.com/sirupsen/logrus"
)

func StartDDNSRoutine() {
	logrus.Infof("StartDDNSRoutine")
	ticker := time.NewTicker(120 * time.Second)
	for range ticker.C {

		ctx := log.GetCtxWithLogID(context.Background())

		// 处理 DDNS
		ddnsConfs := config.Global().DDNSConfig
		if len(ddnsConfs) == 0 {
			log.Ctx(ctx).Infof("ddns config len 0, continue")
			continue
		}

		// 查询 ddns record
		records, err := rpc.GetAllDNSRecord(ctx, config.Global().Cloudflare.Zone)
		recordMap := convertToDNSRecordMap(records)
		if err != nil {
			log.Ctx(ctx).WithError(err).Errorf("GetAllDNSRecord failed")
			continue
		}
		for _, domainConfig := range ddnsConfs {
			err = domainCheck(ctx, domainConfig, recordMap)
			if err != nil {
				log.Ctx(ctx).WithError(err).Errorf("ddns check domain %v failed", domainConfig.Domain)
			}
		}
	}
}

func domainCheck(ctx context.Context, domainConfig *config.DomainConfig, recordMap map[string]*cloudflare.DNSRecord) (err error) {
	ipAddress, err := getIp(ctx, domainConfig)
	if err != nil {
		return err
	}

	// check records
	record, ok := recordMap[domainConfig.Domain]
	if !ok {
		// create
		log.Ctx(ctx).Infof("domain:%v has no record, prepare to create new record", domainConfig.Domain)
		err = createNewRecord(ctx, domainConfig, ipAddress)
		if err != nil {
			log.Ctx(ctx).WithError(err).Errorf("domain:%v has no record, create new record failed", domainConfig.Domain)
			return err
		}
		log.Ctx(ctx).Infof("domain:%v has no record, create content:%v , create new record success", domainConfig.Domain, ipAddress)
		return nil
	}

	// update
	return updateNewRecord(ctx, domainConfig, ipAddress, *record)
}

func getIp(ctx context.Context, domainConfig *config.DomainConfig) (ipAddress string, err error) {
	// check ip address
	if domainConfig.IPVersion == config.IPV4 {
		ipAddress, err = serverRpc.GetDefaultIpv4(ctx)

	} else if domainConfig.IPVersion == config.IPV6 {
		ipAddress, err = serverRpc.GetDefaultIpv6(ctx)
	} else {
		return "", fmt.Errorf("client config ip version is not support, domain:%v version:%v", domainConfig.Domain, domainConfig.IPVersion)
	}
	if err != nil {
		return "", myErrors.Wrapf(err, myErrors.ErrorCodeRpcFailed, "domainCheck get ip address failed")
	}
	return
}

func updateNewRecord(ctx context.Context, domainConfig *config.DomainConfig, ipAddr string, record cloudflare.DNSRecord) error {
	// update
	if strings.EqualFold(ipAddr, record.Content) {
		log.Ctx(ctx).Infof("domain %v is not changed, ip:%v  no need update", domainConfig.Domain, ipAddr)
		return nil
	}

	log.Ctx(ctx).Infof("domain %v has changed from %v to %v, prepare to update", domainConfig.Domain, record.Content, ipAddr)
	record.Content = ipAddr

	err := rpc.UpdateRecord(ctx, config.Global().Cloudflare.Zone, record.ID, record)
	if err != nil {
		log.Ctx(ctx).WithError(err).Errorf("domain %v update failed", domainConfig.Domain)
		return err
	}
	log.Ctx(ctx).Infof("domain %v update success", domainConfig.Domain)
	return nil
}

func createNewRecord(ctx context.Context, domainConfig *config.DomainConfig, ipAddr string) error {
	record := cloudflare.DNSRecord{
		Name:    domainConfig.Domain,
		Type:    getDNSRecordType(domainConfig.IPVersion),
		Content: ipAddr,
		Proxied: utils.Bool(false),
		TTL:     300,
	}

	return rpc.CreateRecord(ctx, config.Global().Cloudflare.Zone, record)
}
