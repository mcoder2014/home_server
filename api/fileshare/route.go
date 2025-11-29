package fileshare

import (
	"net/http"

	"github.com/mcoder2014/home_server/data"
)

func InitRoute() error {
	data.AddRoute(http.MethodGet, "/fileshare/test/*path", GetShareFile)
	return nil
}
