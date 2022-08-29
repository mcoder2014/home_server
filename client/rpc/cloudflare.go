package rpc

import (
	"context"
	"fmt"

	"github.com/cloudflare/cloudflare-go"
	"github.com/mcoder2014/home_server/client/config"
)

func getApi() (*cloudflare.API, error) {
	apiToken := config.Global().Cloudflare.APIToken
	api, err := cloudflare.NewWithAPIToken(apiToken)
	if err != nil {
		return nil, err
	}
	api.Debug = config.Global().Cloudflare.Debug
	return api, nil
}

func GetAllDNSRecord(ctx context.Context, zone string) ([]cloudflare.DNSRecord, error) {
	api, err := getApi()
	if err != nil {
		return nil, err
	}

	//// Fetch the zone ID for zone example.org
	//zoneID, err := api.ZoneIDByName(zone)
	//if err != nil {
	//	return nil, err
	//}

	// Fetch all DNS records for example.org
	return api.DNSRecords(context.Background(), zone, cloudflare.DNSRecord{})
}

func CreateRecord(ctx context.Context, zone string, record cloudflare.DNSRecord) error {
	api, err := getApi()
	if err != nil {
		return err
	}

	resp, err := api.CreateDNSRecord(ctx, zone, record)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("create dns record failed: %+v", resp)
	}
	return nil
}

func UpdateRecord(ctx context.Context, zone string, recordID string, record cloudflare.DNSRecord) error {
	api, err := getApi()
	if err != nil {
		return err
	}
	err = api.UpdateDNSRecord(ctx, zone, recordID, record)
	return err
}
