package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"io"
	"net/http"
	"text/template"
)

type Webhook struct {
	Method              string `json:"method"`
	URLTemplate         string `json:"url_template"`
	RequestBodyTemplate string `json:"request_body_template"`

	tmplUrl         *template.Template
	tmplRequestBody *template.Template
}

func (ch *Webhook) GetInfo() *plugin.Info {
	elements := []*plugin.ConfigOption{
		{
			Name: "method",
			Type: "option",
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
			Options: map[string]string{
				// https://developer.mozilla.org/en-US/docs/Web/HTTP/Methods
				"GET":     "GET",
				"HEAD":    "HEAD",
				"POST":    "POST",
				"PUT":     "PUT",
				"DELETE":  "DELETE",
				"CONNECT": "CONNECT",
				"OPTIONS": "OPTIONS",
				"TRACE":   "TRACE",
				"PATCH":   "PATCH",
			},
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
			// Default as a minimal working example to send a JSON object.
			Default: `{"id": {{.Incident.Id}}, "type": "{{.Event.Type}}", "name": "{{.Object.Name}}", "severity": "{{.Incident.Severity}}", "url": "{{.Incident.Url}}"}`,
		},
	}

	configAttrs, err := json.Marshal(elements)
	if err != nil {
		panic(err)
	}

	return &plugin.Info{
		Name:             "Webhook",
		Version:          internal.Version.Version,
		Author:           "Icinga GmbH",
		ConfigAttributes: configAttrs,
	}
}

func (ch *Webhook) SetConfig(jsonStr json.RawMessage) error {
	err := json.Unmarshal(jsonStr, ch)
	if err != nil {
		return err
	}

	tmplFuncs := template.FuncMap{
		"json": func(a any) string {
			var buff bytes.Buffer
			if err := json.NewEncoder(&buff).Encode(a); err != nil {
				panic(err)
			}
			return buff.String()
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

	return nil
}

func main() {
	plugin.RunPlugin(&Webhook{})
}
