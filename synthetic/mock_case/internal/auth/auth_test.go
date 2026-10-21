package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func req(header string) *http.Request {
	r := httptest.NewRequest("GET", "/x?token=runtime-token-runtime-token-runtime-token", nil)
	if header != "" {
		r.Header.Set("Authorization", header)
	}
	return r
}

func TestIdentify(t *testing.T) {
	a := New("runtime-token-runtime-token-runtime-token", "bench-token-bench-token-bench-token-xx")
	cases := map[string]struct {
		header string
		want   Identity
	}{
		"runtime":             {"Bearer runtime-token-runtime-token-runtime-token", Runtime},
		"scheme case":         {"bearer runtime-token-runtime-token-runtime-token", Runtime},
		"bench":               {"Bearer bench-token-bench-token-bench-token-xx", Bench},
		"no header":           {"", None},
		"empty bearer":        {"Bearer ", None},
		"basic scheme":        {"Basic runtime-token-runtime-token-runtime-token", None},
		"raw token":           {"runtime-token-runtime-token-runtime-token", None},
		"wrong token":         {"Bearer nope", None},
		"prefix of the token": {"Bearer runtime-token", None},
		"token with extra":    {"Bearer runtime-token-runtime-token-runtime-tokenX", None},
		"padded":              {"Bearer  runtime-token-runtime-token-runtime-token", None},
	}
	for name, tc := range cases {
		if got := a.Identify(req(tc.header)); got != tc.want {
			t.Errorf("%s: got %v want %v", name, got, tc.want)
		}
	}
}
