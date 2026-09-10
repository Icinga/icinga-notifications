package main

import (
	"encoding/json"
	"github.com/emersion/go-sasl"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestEmail_SetConfig(t *testing.T) {
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
			name:    "external-is-not-supported",
			jsonMsg: `{"auth_method":"EXTERNAL"}`,
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

			assert.Equal(t, tt.want, email, "Email differs")
		})
	}
}
