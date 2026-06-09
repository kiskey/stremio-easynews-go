package api

import "encoding/base64"

// CreateBasic creates a standard Basic Authentication header value.
// It uses UTF-8 encoding before converting to Base64, matching the behavior
// of the original Node.js Buffer-based implementation.
func CreateBasic(username, password string) string {
	creds := username + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
}
