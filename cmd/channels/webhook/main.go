package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/icinga/icinga-go-library/config"
	"github.com/icinga/icinga-go-library/notifications/jsonrpc"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-notifications/internal"
	"go.uber.org/zap/zapcore"
)

func main() {
	plugin.Run(&Webhook{})
}

type Webhook struct {
	Method                 string `json:"method"`
	URLTemplate            string `json:"url_template"`
	RequestHeadersTemplate string `json:"request_headers_template"`
	RequestBodyTemplate    string `json:"request_body_template"`
	ResponseStatusCodes    string `json:"response_status_codes"`
	TlsConfig              bool   `json:"tls_config"`
	TlsServerName          string `json:"tls_server_name"`
	TlsCert                string `json:"tls_cert"`
	TlsKey                 string `json:"tls_key"`
	TlsCa                  string `json:"tls_ca"`
	TlsInsecure            bool   `json:"tls_insecure"`

	tmplUrl            *template.Template
	tmplRequestHeaders map[string]*template.Template
	tmplRequestBody    *template.Template
	respStatusCodes    []int
	httpTransport      http.RoundTripper
	mu                 sync.Mutex // Protects access to the Webhook struct fields during concurrent RPC calls.

	// rpcCtx and rpcEp are used to make RPC calls back to Icinga Notifications.
	rpcCtx context.Context
	rpcEp  *jsonrpc.Endpoint
}

// ReceiveEndpoint implements the [plugin.RPCEndpointReceiver] interface.
func (ch *Webhook) ReceiveEndpoint(ctx context.Context, ep *jsonrpc.Endpoint) {
	ch.rpcCtx = ctx
	ch.rpcEp = ep
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
				"en_US": "Line-separated 'HTTP-HEADER:TEMPLATE' entries, where TEMPLATE is a Go template over the current plugin.NotificationRequest.",
				"de_DE": "Zeilengetrennte 'HTTP-HEADER:TEMPLATE' Einträge, wobei TEMPLATE ein Go-Template über das zu verarbeitende plugin.NotificationRequest ist.",
			},
		},
		{
			Name: "request_body_template",
			Type: "text",
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
			Name: "tls_config",
			Type: "bool",
			Label: map[string]string{
				"en_US": "TLS Config",
				"de_DE": "TLS-Einstellungen",
			},
			Help: map[string]string{
				"en_US": "Customized TLS configuration.",
				"de_DE": "TLS-Einstellungen anpassen.",
			},
			Children: []plugin.ChildOption{
				{
					Name: "tls_server_name",
					Type: "string",
					Label: map[string]string{
						"en_US": "TLS Server Name",
						"de_DE": "TLS-Servername",
					},
					Help: map[string]string{
						"en_US": "Use this server name for the TLS handshake instead of those from the URL.",
						"de_DE": "Verwende diesen Servernamen anstelle den aus der URL für den TLS-Handshake.",
					},
					ParentValues: []any{true},
				},
				{
					Name: "tls_cert",
					Type: "text",
					Label: map[string]string{
						"en_US": "TLS Client Certificate",
						"de_DE": "TLS Client-Zertifikat",
					},
					Help: map[string]string{
						"en_US": "If set, use this client certificate. Either a file path to a PEM file or a PEM block. Requires TLS Client Key.",
						"de_DE": "Falls gesetzt, verwende dieses Client-Zertifikat. Entweder ein Dateipfad zu einer PEM-Datei oder ein PEM-Block. Benötigt TLS Client-Schlüssel.",
					},
					ParentValues: []any{true},
				},
				{
					Name: "tls_key",
					Type: "text",
					Label: map[string]string{
						"en_US": "TLS Client Key",
						"de_DE": "TLS Client-Schlüssel",
					},
					Help: map[string]string{
						"en_US": "If set, use this client key. Either a file path to a PEM file or a PEM block.",
						"de_DE": "Falls gesetzt, verwende diesen Client-Schlüssel. Entweder ein Dateipfad zu einer PEM-Datei oder ein PEM-Block.",
					},
					ParentValues: []any{true},
				},
				{
					Name: "tls_ca",
					Type: "text",
					Label: map[string]string{
						"en_US": "TLS CA Certificate",
						"de_DE": "TLS CA-Zertifikat",
					},
					Help: map[string]string{
						"en_US": "If set, use a custom CA instead of the OS CA store. Either a file path to a PEM file or a PEM block.",
						"de_DE": "Falls gesetzt, verwende diese CA anstelle des OS CA-Stores. Entweder ein Dateipfad zu einer PEM-Datei oder ein PEM-Block.",
					},
					ParentValues: []any{true},
				},
				{
					Name: "tls_insecure",
					Type: "bool",
					Label: map[string]string{
						"en_US": "No TLS Verification",
						"de_DE": "Keine TLS-Verifizierung",
					},
					Help: map[string]string{
						"en_US": "Skip TLS verification. This might be insecure!",
						"de_DE": "Führe keine TLS-Verifizierung durch. Dies vermag unsicher zu sein!",
					},
					ParentValues: []any{true},
				},
			},
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
	var tmpWh Webhook
	err := plugin.PopulateDefaults(&tmpWh)
	if err != nil {
		return err
	}

	err = json.Unmarshal(jsonStr, &tmpWh)
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

	tmpWh.tmplUrl, err = template.New("url").Funcs(tmplFuncs).Parse(tmpWh.URLTemplate)
	if err != nil {
		return fmt.Errorf("cannot parse URL template: %w", err)
	}

	tmpWh.tmplRequestHeaders = make(map[string]*template.Template)
	for reqHeaderEntry := range strings.SplitSeq(tmpWh.RequestHeadersTemplate, "\n") {
		reqHeaderEntry = strings.TrimSpace(reqHeaderEntry)
		if reqHeaderEntry == "" {
			continue
		}

		key, tmplValue, found := strings.Cut(reqHeaderEntry, ":")
		if !found {
			return fmt.Errorf("cannot process invalid Request Header pair %q", reqHeaderEntry)
		}

		key, tmplValue = strings.TrimSpace(key), strings.TrimSpace(tmplValue)
		if key == "" {
			return fmt.Errorf("cannot process Request Header pair %q with an empty key", reqHeaderEntry)
		}

		tmpl, err := template.New("request_header_" + key).Funcs(tmplFuncs).Parse(tmplValue)
		if err != nil {
			return fmt.Errorf("cannot parse Request Header pair %q as a template: %w", reqHeaderEntry, err)
		}

		tmpWh.tmplRequestHeaders[key] = tmpl
	}

	tmpWh.tmplRequestBody, err = template.New("request_body").Funcs(tmplFuncs).Parse(tmpWh.RequestBodyTemplate)
	if err != nil {
		return fmt.Errorf("cannot parse Request Body template: %w", err)
	}

	respStatusCodes := strings.Split(tmpWh.ResponseStatusCodes, ",")
	tmpWh.respStatusCodes = make([]int, len(respStatusCodes))
	for i, respStatusCodeStr := range respStatusCodes {
		respStatusCode, err := strconv.Atoi(respStatusCodeStr)
		if err != nil {
			return fmt.Errorf("cannot convert status code %q to int: %w", respStatusCodeStr, err)
		}
		tmpWh.respStatusCodes[i] = respStatusCode
	}

	if tmpWh.TlsConfig {
		baseTlsConf := config.TLS{
			Enable:   true,
			Cert:     tmpWh.TlsCert,
			Key:      tmpWh.TlsKey,
			Ca:       tmpWh.TlsCa,
			Insecure: tmpWh.TlsInsecure,
		}
		tlsConf, err := baseTlsConf.MakeConfig(tmpWh.TlsServerName)
		if err != nil {
			return fmt.Errorf("cannot create TLS configuration: %w", err)
		}

		httpTransport := http.DefaultTransport.(*http.Transport).Clone() //nolint:forcetypeassert
		httpTransport.TLSClientConfig = tlsConf
		tmpWh.httpTransport = httpTransport
	} else {
		tmpWh.httpTransport = http.DefaultTransport
	}

	ch.mu.Lock()
	defer ch.mu.Unlock()

	ch.Method = tmpWh.Method
	ch.URLTemplate = tmpWh.URLTemplate
	ch.RequestHeadersTemplate = tmpWh.RequestHeadersTemplate
	ch.RequestBodyTemplate = tmpWh.RequestBodyTemplate
	ch.ResponseStatusCodes = tmpWh.ResponseStatusCodes
	ch.TlsConfig = tmpWh.TlsConfig
	ch.TlsServerName = tmpWh.TlsServerName
	ch.TlsCert = tmpWh.TlsCert
	ch.TlsKey = tmpWh.TlsKey
	ch.TlsCa = tmpWh.TlsCa
	ch.TlsInsecure = tmpWh.TlsInsecure

	ch.tmplUrl = tmpWh.tmplUrl
	ch.tmplRequestHeaders = tmpWh.tmplRequestHeaders
	ch.tmplRequestBody = tmpWh.tmplRequestBody
	ch.respStatusCodes = tmpWh.respStatusCodes
	ch.httpTransport = tmpWh.httpTransport

	return nil
}

func (ch *Webhook) SendNotification(req *plugin.NotificationRequest) error {
	ch.mu.Lock()
	method := ch.Method
	tmplUrl := ch.tmplUrl
	tmplRequestHeaders := ch.tmplRequestHeaders
	tmplRequestBody := ch.tmplRequestBody
	respStatusCodes := ch.respStatusCodes
	httpClient := &http.Client{
		Transport: ch.httpTransport,
		Timeout:   10 * time.Second,
	}
	ch.mu.Unlock()

	var urlBuff, reqBodyBuff, respBuffer bytes.Buffer
	if err := tmplUrl.Execute(&urlBuff, req); err != nil {
		return fmt.Errorf("cannot execute URL template: %w", err)
	}
	if err := tmplRequestBody.Execute(&reqBodyBuff, req); err != nil {
		return fmt.Errorf("cannot execute Request Body template: %w", err)
	}

	httpReq, err := http.NewRequest(method, urlBuff.String(), &reqBodyBuff)
	if err != nil {
		return err
	}

	httpReq.Header.Set("User-Agent", "icinga-notifications-webhook/"+internal.Version.Version)
	for key, tmplValue := range tmplRequestHeaders {
		var valueBuff bytes.Buffer
		if err := tmplValue.Execute(&valueBuff, req); err != nil {
			return fmt.Errorf("cannot execute Request Header template for key %q: %w", key, err)
		}
		httpReq.Header.Set(key, valueBuff.String())
	}

	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return err
	}

	defer func() {
		_, _ = io.Copy(io.Discard, httpResp.Body)
		_ = httpResp.Body.Close()
	}()

	if !slices.Contains(respStatusCodes, httpResp.StatusCode) {
		// Limit response to 1 MiB as it will be logged; rest is going to be discarded.
		limitedRespReader := io.LimitReader(httpResp.Body, 1024*1024)
		if _, err := io.Copy(&respBuffer, limitedRespReader); err != nil {
			return fmt.Errorf("cannot read response: %w", err)
		}

		err := ch.rpcEp.NotifyLog(
			ch.rpcCtx,
			zapcore.ErrorLevel,
			"Received unexpected HTTP Response Code",
			"status_code", httpResp.StatusCode,
			"body", respBuffer.String())
		if err != nil {
			slog.ErrorContext(ch.rpcCtx, "Failed to log HTTP response body", "error", err)
		}

		return fmt.Errorf("unaccepted HTTP response status code %d not in %v",
			httpResp.StatusCode, respStatusCodes)
	}

	return nil
}
