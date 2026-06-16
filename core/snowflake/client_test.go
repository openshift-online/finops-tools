package snowflake

import "testing"

func TestOpenDBRejectsMixedCredentials(t *testing.T) {
	_, err := OpenDB(ConnectParams{
		Account:       "example",
		User:          "user",
		Warehouse:     "wh",
		Token:         "token",
		PrivateKeyPEM: "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
	})
	if err == nil {
		t.Fatal("expected error for mixed credentials")
	}
}
