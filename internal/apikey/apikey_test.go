package apikey

import (
	"errors"
	"testing"
)

func TestParseBearerHeader(t *testing.T) {
	for _, header := range []string{"Bearer cc_token", "bearer cc_token", "BEARER   cc_token"} {
		token, err := ParseHeader(header)
		if err != nil || token != "cc_token" {
			t.Fatalf("parse %q: %q, %v", header, token, err)
		}
	}
	for _, header := range []string{"", "ApiKey cc_token", "Basic cc_token", "Bearer", "Bearer cc_", "Bearer cc_a cc_b", "Bearer cc_a\tcc_b", "Bearer other"} {
		if _, err := ParseHeader(header); !errors.Is(err, ErrMalformed) {
			t.Fatalf("accepted malformed header %q", header)
		}
	}
}
