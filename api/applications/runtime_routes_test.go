package applications

import (
	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/data"
	"testing"
)

func TestApplicationRoutesCanBeEnabledWithoutRestart(t *testing.T) {
	old, routes := config.Global(), data.RouterMap
	t.Cleanup(func() { config.SetGlobalConfig(old); data.RouterMap = routes })
	data.RouterMap = map[string]map[string]data.HttpRoute{}
	config.SetGlobalConfig(config.Config{})
	if err := InitRouter(); err != nil {
		t.Fatal(err)
	}
	if _, ok := data.RouterMap["/api/applications"]; !ok {
		t.Fatal("disabled-at-start application routes cannot be enabled dynamically")
	}
}
