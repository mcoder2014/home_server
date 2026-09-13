package passport

import (
	"fmt"
	"testing"

	jsoniter "github.com/json-iterator/go"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/mcoder2014/home_server/utils"
	"github.com/stretchr/testify/require"
)

// TestGenMockData 为示例身份生成密码哈希并序列化为 JSON，确认生成与序列化过程没有错误。
func TestGenMockData(t *testing.T) {
	var testData = []*model.UserIdentity{
		{
			ID:             123,
			UserName:       "test",
			BcryptPassword: "ss",
			Email:          "m123@123.com",
			Mobile:         "12345678901",
		},
	}

	for _, i := range testData {
		var err error
		i.BcryptPassword, err = utils.HashAndSalt(i.BcryptPassword)
		require.Nil(t, err)
	}

	res, err := jsoniter.Marshal(testData)
	require.Nil(t, err)
	fmt.Printf("marshal config:\n\n\n%v\n\n\n", string(res))
}
