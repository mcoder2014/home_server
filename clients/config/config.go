package config

type ClientConfig struct {
	// home.lljstar.com
	ServerDomain string

	// Server Port
	ServerPort int64

	// 上报时间间隔， 60000 ms
	UploadInterval int64
}

func LoadFromFile(filepath string) (*ClientConfig,error) {
	return nil, nil
}