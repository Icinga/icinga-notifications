package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/emersion/go-sasl"
	"github.com/icinga/icinga-go-library/config"
	"github.com/stretchr/testify/assert"
)

func TestEmail_SetConfig(t *testing.T) {
	tlsConf := config.TLS{ // #nosec G101 -- demo private key
		Enable: true,
		Cert: `-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE3MTAyMDE5NDMwNloXDTE4MTAyMDE5NDMwNlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABD0d
7VNhbWvZLWPuj/RtHFjvtJBEwOkhbN/BnnE8rnZR8+sbwnc/KhCk3FhnpHZnQz7B
5aETbbIgmuvewdjvSBSjYzBhMA4GA1UdDwEB/wQEAwICpDATBgNVHSUEDDAKBggr
BgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MCkGA1UdEQQiMCCCDmxvY2FsaG9zdDo1
NDUzgg4xMjcuMC4wLjE6NTQ1MzAKBggqhkjOPQQDAgNIADBFAiEA2zpJEPQyz6/l
Wf86aX6PepsntZv2GYlA5UpabfT2EZICICpJ5h/iI+i341gBmLiAFQOyTDT+/wQc
6MF9+Yw1Yy0t
-----END CERTIFICATE-----`,
		Key: `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIIrYSSNQFaA2Hwf1duRSxKtLYX5CB04fSeQ6tF1aY/PuoAoGCCqGSM49
AwEHoUQDQgAEPR3tU2Fta9ktY+6P9G0cWO+0kETA6SFs38GecTyudlHz6xvCdz8q
EKTcWGekdmdDPsHloRNtsiCa697B2O9IFA==
-----END EC PRIVATE KEY-----`,
		Ca: `-----BEGIN CERTIFICATE-----
MIICSTCCAfOgAwIBAgIUcmQfIJAvbxdVm0PFanS4FWH71Z0wDQYJKoZIhvcNAQEL
BQAweTELMAkGA1UEBhMCREUxEjAQBgNVBAgMCUZyYW5jb25pYTESMBAGA1UEBwwJ
TnVyZW1iZXJnMUIwQAYDVQQKDDlIb25lc3QgTWFya3VzJyBVc2VkIE51Y2xlYXIg
UG93ZXIgUGxhbnRzIGFuZCBDZXJ0aWZpY2F0ZXMwHhcNMjUwMzA1MDk0ODIwWhcN
MjUwMzA2MDk0ODIwWjB5MQswCQYDVQQGEwJERTESMBAGA1UECAwJRnJhbmNvbmlh
MRIwEAYDVQQHDAlOdXJlbWJlcmcxQjBABgNVBAoMOUhvbmVzdCBNYXJrdXMnIFVz
ZWQgTnVjbGVhciBQb3dlciBQbGFudHMgYW5kIENlcnRpZmljYXRlczBcMA0GCSqG
SIb3DQEBAQUAA0sAMEgCQQCeEGX2IolvELSUjC1DqvJRbTs4DKwE8ZZHDAGrc5K9
DFrLKvkwgfv3g9R2NJE5o/A5vBLq22IDCFdI26M6t10HAgMBAAGjUzBRMB0GA1Ud
DgQWBBQn+dCzVtAzYOGC8tIi9JLmRbWI7jAfBgNVHSMEGDAWgBQn+dCzVtAzYOGC
8tIi9JLmRbWI7jAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA0EAlA27
ti1NKC+o+iZtyU8I/32aPaFme1+eQNIxvqXfw49jSM/FyDjhfZ0XlAxmK6tzF3mM
LJZsYbxapLeyWoA05Q==
-----END CERTIFICATE-----`,
	}

	tests := []struct {
		name    string
		jsonMsg string
		want    *Email
		wantErr bool
	}{
		{
			name:    "empty-string",
			jsonMsg: ``,
			wantErr: true,
		},
		{
			name:    "empty-json-obj-use-defaults",
			jsonMsg: `{}`,
			want:    &Email{SenderName: "Icinga"},
		},
		{
			name:    "sender-mail-null-equals-defaults",
			jsonMsg: `{"sender_mail": null}`,
			want:    &Email{SenderName: "Icinga"},
		},
		{
			name:    "sender-mail-overwrite",
			jsonMsg: `{"sender_mail": "foo@bar"}`,
			want:    &Email{SenderName: "Icinga", SenderMail: "foo@bar"},
		},
		{
			name:    "sender-mail-overwrite-empty",
			jsonMsg: `{"sender_mail": ""}`,
			want:    &Email{SenderName: "Icinga", SenderMail: ""},
		},
		{
			name:    "full-example-config",
			jsonMsg: `{"sender_name":"icinga","sender_mail":"icinga@example.com","host":"smtp.example.com","port":"25","encryption":"none"}`,
			want: &Email{
				Host:       "smtp.example.com",
				Port:       "25",
				SenderName: "icinga",
				SenderMail: "icinga@example.com",
				User:       "",
				Password:   "",
				Encryption: "none",
			},
		},
		{
			name:    "user-but-missing-pass",
			jsonMsg: `{"user": "foo"}`,
			wantErr: true,
		},
		{
			name:    "login-without-credentials",
			jsonMsg: `{"auth_method":"LOGIN"}`,
			wantErr: true,
		},
		{
			name:    "oauthbearer-without-credentials",
			jsonMsg: `{"auth_method":"OAUTHBEARER"}`,
			wantErr: true,
		},
		{
			name:    "login-with-credentials",
			jsonMsg: `{"auth_method":"LOGIN","user":"foo","password":"bar"}`,
			want: &Email{
				SenderName: "Icinga",
				User:       "foo",
				Password:   "bar",
				AuthMethod: sasl.Login,
			},
		},
		{
			name: "external-mtls",
			jsonMsg: `{
				"host":"smtp.example.com",
				"port":"993",
				"encryption":"tls",
				"tls_cert":"` + strings.ReplaceAll(tlsConf.Cert, "\n", "\\n") + `",
				"tls_key": "` + strings.ReplaceAll(tlsConf.Key, "\n", "\\n") + `",
				"tls_ca": "` + strings.ReplaceAll(tlsConf.Ca, "\n", "\\n") + `",
				"auth_method":"EXTERNAL"
				}`,
			want: &Email{
				Host:       "smtp.example.com",
				Port:       "993",
				SenderName: "Icinga",
				Encryption: EncryptionTLS,
				TlsCert:    tlsConf.Cert,
				TlsKey:     tlsConf.Key,
				TlsCa:      tlsConf.Ca,
				AuthMethod: sasl.External,
			},
		},
		{
			name: "external-mtls-no-tls",
			jsonMsg: `{
				"host":"smtp.example.com",
				"encryption":"none",
				"auth_method":"EXTERNAL"
				}`,
			wantErr: true,
		},
		{
			name: "external-mtls-missing-client-cert",
			jsonMsg: `{
				"host":"smtp.example.com",
				"port":"993",
				"encryption":"tls",
				"tls_ca": "` + strings.ReplaceAll(tlsConf.Ca, "\n", "\\n") + `",
				"auth_method":"EXTERNAL"
				}`,
			wantErr: true,
		},
		{
			name:    "unsupported-auth-method",
			jsonMsg: `{"auth_method":"foo"}`,
			wantErr: true,
		},
		{
			name:    "plain-without-credentials",
			jsonMsg: `{"auth_method":"PLAIN"}`,
			wantErr: true,
		},
		{
			name:    "empty-auth-method-is-unauthenticated",
			jsonMsg: `{"auth_method":""}`,
			want:    &Email{SenderName: "Icinga"},
		},
		{
			name:    "legacy-config-with-credentials",
			jsonMsg: `{"user":"foo","password":"bar"}`,
			want: &Email{
				SenderName: "Icinga",
				User:       "foo",
				Password:   "bar",
				AuthMethod: sasl.Plain,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email := &Email{}
			err := email.SetConfig(json.RawMessage(tt.jsonMsg))
			assert.Equal(t, tt.wantErr, err != nil, "SetConfig() error = %v, wantErr = %t", err, tt.wantErr)
			if err != nil {
				return
			}

			// Unset Email.tlsConf as pointer comparison would fail.
			email.tlsConf = nil
			assert.Equal(t, tt.want, email, "Email differs")
		})
	}
}
