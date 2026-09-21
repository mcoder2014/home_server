package config

import "testing"

func TestConfigurablePasswordBoundaries(t *testing.T) {
	for _, key := range []string{"min_password_length", "min_share_password_length", "share_code_length"} {
		values := DefaultRuntimeValues(Config{})
		values["account_policy"][key] = 4
		if _, err := BuildRuntimeSnapshot(Config{}, 0, nil, values); err != nil {
			t.Fatalf("%s=4: %v", key, err)
		}
		for _, invalid := range []interface{}{3, 0, nil, 4.5, 73} {
			values["account_policy"][key] = invalid
			if _, err := ValidateValues(Config{}, "account_policy", values["account_policy"]); err == nil {
				t.Fatalf("accepted %s=%v", key, invalid)
			}
		}
	}
}
