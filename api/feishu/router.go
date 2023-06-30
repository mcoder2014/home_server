package feishu

import (
	"github.com/mcoder2014/home_server/data"
)

func InitRouter() error {
	data.AddRoute("POST", "/feishu/event", HandleEvent)
	return nil
}
