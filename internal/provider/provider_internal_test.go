package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestResolveAuth(t *testing.T) {
	cases := []struct {
		name                                  string
		config                                BifrostProviderModel
		env                                   map[string]string
		wantUsername, wantPassword, wantToken string
	}{
		{
			name:      "token in config",
			config:    BifrostProviderModel{Token: types.StringValue("bfak_hcl")},
			wantToken: "bfak_hcl",
		},
		{
			name:      "token in config wins over basic in config",
			config:    BifrostProviderModel{Username: types.StringValue("admin"), Password: types.StringValue("secret"), Token: types.StringValue("bfak_hcl")},
			wantToken: "bfak_hcl",
		},
		{
			name:         "basic in config ignores BIFROST_TOKEN",
			config:       BifrostProviderModel{Username: types.StringValue("admin"), Password: types.StringValue("secret")},
			env:          map[string]string{"BIFROST_TOKEN": "bfak_env"},
			wantUsername: "admin",
			wantPassword: "secret",
		},
		{
			name:         "username in config, password from env",
			config:       BifrostProviderModel{Username: types.StringValue("admin")},
			env:          map[string]string{"BIFROST_PASSWORD": "secret", "BIFROST_TOKEN": "bfak_env"},
			wantUsername: "admin",
			wantPassword: "secret",
		},
		{
			name:         "env only",
			env:          map[string]string{"BIFROST_USERNAME": "admin", "BIFROST_PASSWORD": "secret", "BIFROST_TOKEN": "bfak_env"},
			wantUsername: "admin",
			wantPassword: "secret",
			wantToken:    "bfak_env",
		},
		{
			name: "nothing set",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"BIFROST_USERNAME", "BIFROST_PASSWORD", "BIFROST_TOKEN"} {
				t.Setenv(k, tc.env[k])
			}

			username, password, token := resolveAuth(tc.config)
			if username != tc.wantUsername || password != tc.wantPassword || token != tc.wantToken {
				t.Errorf("got (%q, %q, %q), want (%q, %q, %q)",
					username, password, token, tc.wantUsername, tc.wantPassword, tc.wantToken)
			}
		})
	}
}
