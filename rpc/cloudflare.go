package rpc

import (
	"context"

	"github.com/cloudflare/cloudflare-go"
	"github.com/mcoder2014/home_server/config"
)

func GetAllDNSRecord(ctx context.Context, zone string) ([]cloudflare.DNSRecord, error) {

	apiToken := config.Global().RPC.Cloudflare.APIToken
	api, err := cloudflare.NewWithAPIToken(apiToken)
	if err != nil {
		return nil, err
	}

	// Fetch the zone ID for zone example.org
	zoneID, err := api.ZoneIDByName(zone)
	if err != nil {
		return nil, err
	}

	// Fetch all DNS records for example.org
	return api.DNSRecords(context.Background(), zoneID, cloudflare.DNSRecord{})
}
