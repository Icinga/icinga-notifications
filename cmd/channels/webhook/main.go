package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"text/template"
)

type Webhook struct {
	Method              string `json:"method"`
	URLTemplate         string `json:"url_template"`
	RequestBodyTemplate string `json:"request_body_template"`
	ResponseStatusCodes string `json:"response_status_codes"`

	tmplUrl         *template.Template
	tmplRequestBody *template.Template

	respStatusCodes []int
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

	return nil
}

func (ch *Webhook) SendNotification(req *plugin.NotificationRequest) error {
	var urlBuff, reqBodyBuff bytes.Buffer
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
	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, httpResp.Body)
	_ = httpResp.Body.Close()

	if !slices.Contains(ch.respStatusCodes, httpResp.StatusCode) {
		return fmt.Errorf("unaccepted HTTP response status code %d not in %v",
			httpResp.StatusCode, ch.respStatusCodes)
	}

	return nil
}

func main() {
	plugin.RunPlugin(&Webhook{})
}
