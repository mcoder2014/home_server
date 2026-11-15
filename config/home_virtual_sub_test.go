package config

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestHomeVirtualSubConfig(t *testing.T) {
	convey.Convey("旧配置保持关闭，启用后限制私有连接与凭证路径", t, func() {
		var legacy HomeVirtualSubConfig
		convey.So(NormalizeHomeVirtualSub(&legacy), convey.ShouldBeNil)
		convey.So(legacy.Enabled, convey.ShouldBeFalse)

		active := HomeVirtualSubConfig{Enabled: true, TokenFile: "/etc/home_server/home_virtual_sub.token"}
		convey.So(NormalizeHomeVirtualSub(&active), convey.ShouldBeNil)
		convey.So(active.BaseURL, convey.ShouldEqual, "http://127.0.0.1:18190")
		convey.So(active.TimeoutMS, convey.ShouldEqual, 5000)
		for _, address := range []string{"http://example.com", "http://user:secret@127.0.0.1", "http://127.0.0.1/other", "http://127.0.0.1?token=secret", "file:///tmp/service"} {
			candidate := active
			candidate.BaseURL = address
			convey.So(NormalizeHomeVirtualSub(&candidate), convey.ShouldNotBeNil)
		}
		active.TokenFile = "relative.token"
		convey.So(NormalizeHomeVirtualSub(&active), convey.ShouldNotBeNil)
	})
}
