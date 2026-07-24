package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-notifications/internal"
)

func main() {
	plugin.Run(&Webhook{})
}

type Webhook struct {
	Method              string `json:"method"`
	URLTemplate         string `json:"url_template"`
	RequestBodyTemplate string `json:"request_body_template"`
	ResponseStatusCodes string `json:"response_status_codes"`

	tmplUrl         *template.Template
	tmplRequestBody *template.Template

	respStatusCodes []int
	mu              sync.Mutex // Protects access to the Webhook struct fields during concurrent RPC calls.
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

	ch.mu.Lock()
	defer ch.mu.Unlock()

	ch.Method = tmpWh.Method
	ch.URLTemplate = tmpWh.URLTemplate
	ch.RequestBodyTemplate = tmpWh.RequestBodyTemplate
	ch.ResponseStatusCodes = tmpWh.ResponseStatusCodes

	ch.tmplUrl = tmpWh.tmplUrl
	ch.tmplRequestBody = tmpWh.tmplRequestBody
	ch.respStatusCodes = tmpWh.respStatusCodes

	return nil
}

func (ch *Webhook) SendNotification(req *plugin.NotificationRequest) error {
	ch.mu.Lock()
	method := ch.Method
	tmplUrl := ch.tmplUrl
	tmplRequestBody := ch.tmplRequestBody
	respStatusCodes := ch.respStatusCodes
	ch.mu.Unlock()

	var urlBuff, reqBodyBuff bytes.Buffer
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
	httpResp, err := http.DefaultClient.Do(httpReq) // #nosec G704 -- no SSRF, trusted user input
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, httpResp.Body)
	_ = httpResp.Body.Close()

	if !slices.Contains(respStatusCodes, httpResp.StatusCode) {
		return fmt.Errorf("unaccepted HTTP response status code %d not in %v",
			httpResp.StatusCode, respStatusCodes)
	}

	return nil
}
