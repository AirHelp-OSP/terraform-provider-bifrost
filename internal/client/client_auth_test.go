package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDoRequestAuthorizationHeader(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		token    string
		want     string
	}{
		{name: "bearer token", token: "bfak_test", want: "Bearer bfak_test"},
		{name: "token wins over basic", username: "admin", password: "secret", token: "bfak_test", want: "Bearer bfak_test"},
		{name: "basic auth", username: "admin", password: "secret", want: "Basic YWRtaW46c2VjcmV0"},
		{name: "no credentials", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get("Authorization")
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			c := New(srv.URL, tt.username, tt.password)
			c.Token = tt.token
			if err := c.doRequest(context.Background(), http.MethodGet, "/api/ping", nil, nil); err != nil {
				t.Fatalf("doRequest: %v", err)
			}
			if got != tt.want {
				t.Errorf("Authorization = %q, want %q", got, tt.want)
			}
		})
	}
}
