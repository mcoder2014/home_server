package ddns

import (
	"github.com/cloudflare/cloudflare-go"
	"github.com/mcoder2014/home_server/client/config"
)

func convertToDNSRecordMap(records []cloudflare.DNSRecord) map[string]*cloudflare.DNSRecord {
	if len(records) == 0 {
		return nil
	}
	var res = make(map[string]*cloudflare.DNSRecord)
	for _, record := range records {
		tmpRecord := record
		res[record.Name] = &tmpRecord
	}
	return res
}

func getDNSRecordType(ipVersion config.IpVersion) string {
	recordType := "TXT"
	if ipVersion == config.IPV4 {
		recordType = "A"
	} else if ipVersion == config.IPV6 {
		recordType = "AAAA"
	}
	return recordType
}
