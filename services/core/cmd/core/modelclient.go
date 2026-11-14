package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Noemai-org/noema/services/core/understanding"
)

// modelclient.go builds the core's client of the model service over mutual TLS (UND-05, noema#555; the Founder's order of 14 Nov and ruling A208, relayed, not yet committed;
// PERD 36.1 and 18.4 boundary 2: mTLS between the two services, never a shared bearer; INV-29, AGENT-13, AGENT-16). The core presents its own certificate, trusts only the CA file
// it is given (not the system's roots) and verifies the model service's certificate against it. Every value comes from a setting with no default. No error carries a key.

// The settings of the model service client. All five or none: with none the core has no Interpreter and /readyz says so.
const (
	// EnvModelURL is the model service's base URL: https://host[:port], no path.
	EnvModelURL = "NOEMA_MODEL_URL"
	// EnvModelClientCertFile and EnvModelClientKeyFile are the core's client certificate and its private key (PEM, file-mounted).
	EnvModelClientCertFile = "NOEMA_MODEL_TLS_CERT_FILE"
	EnvModelClientKeyFile  = "NOEMA_MODEL_TLS_KEY_FILE"
	// EnvModelCAFile is the CA that signs the model service's certificate (PEM, file-mounted): the only root the client trusts.
	EnvModelCAFile = "NOEMA_MODEL_TLS_CA_FILE"
	// EnvModelMaxResponseBytes is the longest answer the client accepts (understanding.ClientConfig.MaxResponseBytes): a positive integer with no default.
	EnvModelMaxResponseBytes = "NOEMA_MODEL_MAX_RESPONSE_BYTES"
)

var modelSettings = []string{EnvModelURL, EnvModelClientCertFile, EnvModelClientKeyFile, EnvModelCAFile, EnvModelMaxResponseBytes}

// buildModelHTTPClient builds the mutual-TLS HTTP client. It refuses a URL that is not https, an unset setting, a file that is not a PEM certificate, key or CA, and a key that
// is not the certificate's. The TLS minimum is 1.2, as the egress gateway's; the caller's context deadline bounds every call (understanding.Client requires one).
func buildModelHTTPClient(env Env) (*http.Client, error) {
	for _, s := range modelSettings {
		if env.Getenv(s) == "" {
			return nil, fmt.Errorf("cmd/core: the model service client needs %s: all five model settings or none", s)
		}
	}
	u, err := url.Parse(env.Getenv(EnvModelURL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("cmd/core: %s must be https://host[:port]: the model service is called over mutual TLS only", EnvModelURL)
	}
	if _, err := modelMaxBytes(env); err != nil {
		return nil, err
	}
	if env.ReadFile == nil {
		return nil, errors.New("cmd/core: the process has no file access wired for the model service client")
	}
	certPEM, err := env.ReadFile(env.Getenv(EnvModelClientCertFile))
	if err != nil {
		return nil, fmt.Errorf("cmd/core: %s cannot be read", EnvModelClientCertFile)
	}
	keyPEM, err := env.ReadFile(env.Getenv(EnvModelClientKeyFile))
	if err != nil {
		return nil, fmt.Errorf("cmd/core: %s cannot be read", EnvModelClientKeyFile)
	}
	caPEM, err := env.ReadFile(env.Getenv(EnvModelCAFile))
	if err != nil {
		return nil, fmt.Errorf("cmd/core: %s cannot be read", EnvModelCAFile)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM) // its error can name a PEM block type, never the key: it is dropped all the same
	if err != nil {
		return nil, fmt.Errorf("cmd/core: %s and %s are not a certificate and its private key", EnvModelClientCertFile, EnvModelClientKeyFile)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("cmd/core: %s holds no PEM certificate", EnvModelCAFile)
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, RootCAs: roots}}}, nil
}

// buildModelClient builds the understanding client over the mutual-TLS HTTP client. With none of the model settings it returns no client and no error (the Interpreter is
// then not configured); with some but not all, or any that cannot be applied, it returns an error naming the setting.
func buildModelClient(env Env) (*understanding.Client, error) {
	none := true
	for _, s := range modelSettings {
		if env.Getenv(s) != "" {
			none = false
		}
	}
	if none {
		return nil, nil
	}
	hc, err := buildModelHTTPClient(env)
	if err != nil {
		return nil, err
	}
	max, err := modelMaxBytes(env)
	if err != nil {
		return nil, err
	}
	return understanding.NewClient(understanding.ClientConfig{BaseURL: env.Getenv(EnvModelURL), HTTP: hc, MaxResponseBytes: max})
}

// modelMaxBytes reads the response bound: a positive integer, no default (AGENT-16).
func modelMaxBytes(env Env) (int64, error) {
	n, err := strconv.ParseInt(env.Getenv(EnvModelMaxResponseBytes), 10, 64)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("cmd/core: %s must be a positive integer", EnvModelMaxResponseBytes)
	}
	return n, nil
}
