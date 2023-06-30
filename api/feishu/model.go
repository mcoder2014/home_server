package feishu

type EncryptEvent struct {
	Encrypt string `json:"encrypt"`
}

/**
{
    "schema": "2.0",
    "header": {
        "event_id": "f7984f25108f8137722bb63cee927e66",
        "token": "066zT6pS4QCbgj5Do145GfDbbagCHGgF",
        "create_time": "1603977298000000",
        "event_type": "contact.user_group.created_v3",
        "tenant_key": "xxxxxxx",
        "app_id": "cli_xxxxxxxx",
    },
    "event":{
    }
}
*/

type EventV2 struct {
	Schema string `json:"schema"`
	Header struct {
		EventID    string `json:"event_id"`
		Token      string `json:"token"`
		CreateTime string `json:"create_time"`
		EventType  string `json:"event_type"`
		TenantKey  string `json:"tenant_key"`
		AppID      string `json:"app_id"`
	}
	Event interface{} `json:"event"`

	// 飞书验证消息用的
	Challenge string `json:"challenge"`
	Token     string `json:"token"`
	Type      string `json:"type"`
}
