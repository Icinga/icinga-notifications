package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/template"
)

func main() {
	plugin.RunPlugin(&Webhook{})
}

// transport is a http.Transport with a custom User-Agent.
type transport http.Transport

// RoundTrip implements http.RoundTripper with a custom User-Agent header.
func (trans *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", "icinga-notifications-webhook/"+internal.Version.Version)
	return (*http.Transport)(trans).RoundTrip(req)
}

type Webhook struct {
	Method                 string      `json:"method"`
	URLTemplate            string      `json:"url_template"`
	RequestHeadersTemplate string      `json:"request_headers_template"`
	RequestBodyTemplate    string      `json:"request_body_template"`
	ResponseStatusCodes    string      `json:"response_status_codes"`
	TlsCommonName          string      `json:"tls_common_name"`
	TlsCaPemFile           string      `json:"tls_ca_pem_file"`
	TlsInsecure            plugin.Bool `json:"tls_insecure"`

	tmplUrl            *template.Template
	tmplRequestHeaders map[string]*template.Template
	tmplRequestBody    *template.Template
	respStatusCodes    []int
	httpTransport      http.RoundTripper
}

func (ch *Webhook) GetInfo() *plugin.Info {
	configAttrs := plugin.ConfigOptions{
		{
			Name: "method",
			Type: "string",
			Label: map[string]string{
				"en_US": "HTTP Method",
				"de_DE": "HTTP-Methode",
			},
			Help: map[string]string{
				"en_US": "HTTP request method used for the web request.",
				"de_DE": "HTTP-Methode für die Anfrage.",
			},
			Default:  "POST",
			Required: true,
		},
		{
			Name: "url_template",
			Type: "string",
			Label: map[string]string{
				"en_US": "URL Template",
				"de_DE": "URL-Template",
			},
			Help: map[string]string{
				"en_US": "URL, optionally as a Go template over the current plugin.NotificationRequest.",
				"de_DE": "URL, optional als Go-Template über das zu verarbeitende plugin.NotificationRequest.",
			},
			Required: true,
		},
		{
			Name: "request_headers_template",
			Type: "text",
			Label: map[string]string{
				"en_US": "Request Header Template",
				"de_DE": "Request Header-Template",
			},
			Help: map[string]string{
				"en_US": "Multiple lines of 'HTTP-HEADER=TEMPLATE' with TEMPLATE being a Go template about the current plugin.NotificationRequest.",
				"de_DE": "Mehrere Zeilen im Format 'HTTP-HEADER=TEMPLATE', wobei TEMPLATE ein Go-Template über das zu verarbeitende plugin.NotificationRequest ist.",
			},
			Default: "",
		},
		{
			Name: "request_body_template",
			Type: "string",
			Label: map[string]string{
				"en_US": "Request Body Template",
				"de_DE": "Anfragedaten-Template",
			},
			Help: map[string]string{
				"en_US": "Go template applied to the current plugin.NotificationRequest to create an request body.",
				"de_DE": "Go-Template über das zu verarbeitende plugin.NotificationRequest zum Erzeugen der mitgesendeten Anfragedaten.",
			},
			Default: "{{json .}}",
		},
		{
			Name: "response_status_codes",
			Type: "string",
			Label: map[string]string{
				"en_US": "Response Status Codes",
				"de_DE": "Antwort-Status-Codes",
			},
			Help: map[string]string{
				"en_US": "Comma separated list of expected HTTP response status code, e.g., 200,201,202,208,418",
				"de_DE": "Kommaseparierte Liste erwarteter Status-Code der HTTP-Antwort, z.B.: 200,201,202,208,418",
			},
			Default:  "200",
			Required: true,
		},
		{
			Name: "tls_common_name",
			Type: "string",
			Label: map[string]string{
				"en_US": "TLS Common Name",
				"de_DE": "TLS Common Name",
			},
			Help: map[string]string{
				"en_US": "Expect this CN for the server's TLS certificate instead of the URL's hostname.",
				"de_DE": "Erwarte diesen CN für das TLS-Zertifikat des Servers anstelle des Hostnames aus der URL.",
			},
			Default: "",
		},
		{
			Name: "tls_ca_pem_file",
			Type: "string",
			Label: map[string]string{
				"en_US": "CA PEM File",
				"de_DE": "CA-PEM-Datei",
			},
			Help: map[string]string{
				"en_US": "Path to a custom CA as a PEM file to be used for TLS certificate verification.",
				"de_DE": "Dateipfad zu einer eigenen CA als PEM-Datei zum Verifizieren des TLS-Zertifikats.",
			},
			Default: "",
		},
		{
			Name: "tls_insecure",
			Type: "bool",
			Label: map[string]string{
				"en_US": "No TLS Verification",
				"de_DE": "Keine TLS-Verifizierung",
			},
			Help: map[string]string{
				"en_US": "Skip TLS verification. This might be insecure.",
				"de_DE": "Führe keine TLS-Verifizierung durch. Dies vermag unsicher zu sein.",
			},
			Default: false,
		},
	}

	return &plugin.Info{
		Name:             "Webhook",
		Version:          internal.Version.Version,
		Author:           "Icinga GmbH",
		ConfigAttributes: configAttrs,
	}
}

func (ch *Webhook) SetConfig(jsonStr json.RawMessage) error {
	err := plugin.PopulateDefaults(ch)
	if err != nil {
		return err
	}

	err = json.Unmarshal(jsonStr, ch)
	if err != nil {
		return err
	}

	tmplFuncs := template.FuncMap{
		"json": func(a any) (string, error) {
			data, err := json.Marshal(a)
			if err != nil {
				return "", err
			}
			return string(data), nil

		},
	}

	ch.tmplUrl, err = template.New("url").Funcs(tmplFuncs).Parse(ch.URLTemplate)
	if err != nil {
		return fmt.Errorf("cannot parse URL template: %w", err)
	}

	ch.tmplRequestHeaders = make(map[string]*template.Template)
	for _, reqHeaderEntry := range strings.Split(ch.RequestHeadersTemplate, "\n") {
		key, tmplValue, found := strings.Cut(reqHeaderEntry, "=")
		if !found {
			return fmt.Errorf("cannot process invalid Request Header pair %q", reqHeaderEntry)
		}

		key = strings.TrimSpace(key)
		tmplValue = strings.TrimSpace(tmplValue)

		if key == "" {
			return fmt.Errorf("cannot process Request Header pair %q with an empty key", reqHeaderEntry)
		}

		tmpl, err := template.New("request_header_" + key).Funcs(tmplFuncs).Parse(tmplValue)
		if err != nil {
			return fmt.Errorf("cannot parse Request Header pair %q as a template: %w", reqHeaderEntry, err)
		}

		ch.tmplRequestHeaders[key] = tmpl
	}

	ch.tmplRequestBody, err = template.New("request_body").Funcs(tmplFuncs).Parse(ch.RequestBodyTemplate)
	if err != nil {
		return fmt.Errorf("cannot parse Request Body template: %w", err)
	}

	respStatusCodes := strings.Split(ch.ResponseStatusCodes, ",")
	ch.respStatusCodes = make([]int, len(respStatusCodes))
	for i, respStatusCodeStr := range respStatusCodes {
		respStatusCode, err := strconv.Atoi(respStatusCodeStr)
		if err != nil {
			return fmt.Errorf("cannot convert status code %q to int: %w", respStatusCodeStr, err)
		}
		ch.respStatusCodes[i] = respStatusCode
	}

	tlsConf := &tls.Config{
		// https://ssl-config.mozilla.org/#server=go&config=intermediate
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
	}

	if ch.TlsCommonName != "" {
		tlsConf.ServerName = ch.TlsCommonName
	}
	if ch.TlsCaPemFile != "" {
		caPem, err := os.ReadFile(ch.TlsCaPemFile)
		if err != nil {
			return fmt.Errorf("cannot open custom CA PEM file %q: %v", ch.TlsCaPemFile, err)
		}

		tlsConf.RootCAs = x509.NewCertPool()
		if !tlsConf.RootCAs.AppendCertsFromPEM(caPem) {
			return fmt.Errorf("cannot parse CA PEM file %q", ch.TlsCaPemFile)
		}
	}
	if ch.TlsInsecure {
		tlsConf.InsecureSkipVerify = true
	}

	ch.httpTransport = &transport{TLSClientConfig: tlsConf}

	return nil
}

func (ch *Webhook) SendNotification(req *plugin.NotificationRequest) error {
	var urlBuff, reqBodyBuff, respBuffer bytes.Buffer
	if err := ch.tmplUrl.Execute(&urlBuff, req); err != nil {
		return fmt.Errorf("cannot execute URL template: %w", err)
	}
	if err := ch.tmplRequestBody.Execute(&reqBodyBuff, req); err != nil {
		return fmt.Errorf("cannot execute Request Body template: %w", err)
	}

	httpReq, err := http.NewRequest(ch.Method, urlBuff.String(), &reqBodyBuff)
	if err != nil {
		return err
	}
	for key, tmplValue := range ch.tmplRequestHeaders {
		var valueBuff bytes.Buffer
		if err := tmplValue.Execute(&valueBuff, req); err != nil {
			return fmt.Errorf("cannot execute Request Header template for key %q: %w", key, err)
		}
		httpReq.Header.Set(key, valueBuff.String())
	}

	httpClient := &http.Client{Transport: ch.httpTransport}
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return err
	}

	defer func() {
		_, _ = io.Copy(io.Discard, httpResp.Body)
		_ = httpResp.Body.Close()
	}()

	// Limit response to 1 MiB as it will be logged in case of an unexpected status code.
	limitedRespReader := io.LimitReader(httpResp.Body, 1024*1024)
	if _, err := io.Copy(&respBuffer, limitedRespReader); err != nil {
		return fmt.Errorf("cannot read response: %w", err)
	}

	if !slices.Contains(ch.respStatusCodes, httpResp.StatusCode) {
		_, _ = fmt.Fprintf(os.Stderr, "received unexpected HTTP response code %d with body %q\n",
			httpResp.StatusCode, respBuffer.String())

		return fmt.Errorf("unaccepted HTTP response status code %d not in %v",
			httpResp.StatusCode, ch.respStatusCodes)
	}

	return nil
}
