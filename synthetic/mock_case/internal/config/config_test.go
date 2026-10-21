package config

import (
	"errors"
	"strings"
	"testing"
)

const tokA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const tokB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func env(extra map[string]string) func(string) string {
	base := map[string]string{
		"MOCK_CASE_BENCH_BIND_VIA": "mock-case-db", "MOCK_CASE_DB_APP_PASSWORD_FILE": "/app", "MOCK_CASE_DB_ADMIN_PASSWORD_FILE": "/admin",
		"MOCK_CASE_RUNTIME_TOKEN_FILE": "/rt", "MOCK_CASE_BENCH_TOKEN_FILE": "/bt",
	}
	for k, v := range extra {
		if v == "-" {
			delete(base, k)
		} else {
			base[k] = v
		}
	}
	return func(k string) string { return base[k] }
}

func files(m map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if v, ok := m[p]; ok {
			return []byte(v), nil
		}
		return nil, errors.New("no such file " + p)
	}
}

var good = map[string]string{"/app": "pw1\n", "/admin": "pw2\n", "/rt": tokA + "\n", "/bt": tokB + "\n"}

func TestLoadDefaultsAndDSN(t *testing.T) {
	c, err := Load(env(nil), files(good))
	if err != nil {
		t.Fatal(err)
	}
	if c.RuntimeToken != tokA || c.BenchToken != tokB || c.DefaultQueue != "PAYMENTS_OPERATIONS" || len(c.Queues) != 4 {
		t.Fatalf("%+v", c)
	}
	if got := c.AppDSN(); got != "postgres://mock_case_app:pw1@mock-case-db:5432/mock_case?sslmode=disable" {
		t.Fatal(got)
	}
	if !strings.Contains(c.AdminDSN(), "mock_case_admin:pw2@") {
		t.Fatal(c.AdminDSN())
	}
}

func TestLoadRefusals(t *testing.T) {
	short := map[string]string{"/app": "a", "/admin": "b", "/rt": "short", "/bt": tokB}
	same := map[string]string{"/app": "a", "/admin": "b", "/rt": tokA, "/bt": tokA}
	cases := map[string]struct {
		env   map[string]string
		files map[string]string
		want  string
	}{
		"bench listener unset":     {map[string]string{"MOCK_CASE_BENCH_BIND_VIA": "-"}, good, "exactly one"},
		"bench listener twice":     {map[string]string{"MOCK_CASE_BENCH_LISTEN": "127.0.0.1:1"}, good, "exactly one"},
		"secret path missing":      {map[string]string{"MOCK_CASE_RUNTIME_TOKEN_FILE": "-"}, good, "MOCK_CASE_RUNTIME_TOKEN_FILE is required"},
		"secret file unreadable":   {nil, map[string]string{"/app": "a"}, "no such file"},
		"empty secret":             {nil, map[string]string{"/app": " \n", "/admin": "b", "/rt": tokA, "/bt": tokB}, "empty"},
		"short token":              {nil, short, "at least"},
		"same token":               {nil, same, "must differ"},
		"default queue not listed": {map[string]string{"MOCK_CASE_DEFAULT_QUEUE": "NOPE"}, good, "DEFAULT_QUEUE"},
		"default priority":         {map[string]string{"MOCK_CASE_DEFAULT_PRIORITY": "NOPE"}, good, "DEFAULT_PRIORITY"},
		"db port":                  {map[string]string{"MOCK_CASE_DB_PORT": "x"}, good, "PORT"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(env(tc.env), files(tc.files))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want mention of %q", err, tc.want)
			}
		})
	}
}
