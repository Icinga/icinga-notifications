package notifications_test

import (
	"encoding/json"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"github.com/icinga/icinga-testing/utils/eventually"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

// TestNotificationRoundTrip instructs an Icinga 2 node to send a notification back for further inspection.
func TestNotificationRoundTrip(t *testing.T) {
	rdb := getDatabase(t)
	notifications := it.IcingaNotificationsInstanceT(t, rdb)
	icinga := it.Icinga2NodeT(t, "master")
	icinga.EnableIcingaNotifications(notifications)
	require.NoError(t, icinga.Reload(), "icinga.Reload()")

	db, err := sqlx.Open(rdb.Driver(), rdb.DSN())
	require.NoError(t, err, "SQL database open")
	defer func() { require.NoError(t, db.Close(), "db.Cleanup") }()

	webhookRec := it.IcingaNotificationsWebhookReceiverInstanceT(t)
	webhookRecReqCh := make(chan plugin.NotificationRequest)
	webhookRec.Handler = func(writer http.ResponseWriter, request *http.Request) {
		var notReq plugin.NotificationRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&notReq), "decoding NotificationRequest from request body")
		require.NoError(t, request.Body.Close(), "closing request body")
		webhookRecReqCh <- notReq
		http.Error(writer, "forwarded", http.StatusOK)
	}

	t.Run("configure channel in database", func(t *testing.T) {
		eventually.Require(t, func(t require.TestingT) {
			var channelCount int
			err := db.QueryRow(`SELECT COUNT(*) FROM available_channel_type WHERE type = 'webhook'`).Scan(&channelCount)
			require.NoError(t, err, "SQL SELECT FROM available_channel_type query")
			require.Equal(t, 1, channelCount, "webhook type missing from available_channel_type")
		}, 10*time.Second, time.Second)

		_, err := db.Exec(`
			INSERT INTO channel (id, name, type, config)
			VALUES (1, 'webhook', 'webhook', '{"method":"POST","url_template":"http:\/\/` + webhookRec.ListenAddr + `\/","request_body_template":"{{json .}}"}');

			INSERT INTO contact (id, full_name, username, default_channel_id, color)
			VALUES (1, 'icingaadmin', 'icingaadmin', 1, '#000000');

			INSERT INTO rule (id, name, is_active) VALUES (1, 'webhook', 'y');
			INSERT INTO rule_escalation (id, rule_id, position) VALUES (1, 1, 1);
			INSERT INTO rule_escalation_recipient (id, rule_escalation_id, contact_id, channel_id) VALUES (1, 1, 1, 1);`)
		require.NoError(t, err, "populating tables failed")
	})

	t.Run("create icinga objects", func(t *testing.T) {
		client := icinga.ApiClient()
		client.CreateObject(t, "checkcommands", "failure-check", map[string]any{
			"templates": []any{"plugin-check-command"},
			"attrs":     map[string]any{"command": []string{"/bin/false"}},
		})
		client.CreateHost(t, "test-host", map[string]any{
			"attrs": map[string]any{"check_command": "failure-check"},
		})
		client.CreateService(t, "test-host", "test-service", map[string]any{
			"attrs": map[string]any{"check_command": "failure-check"},
		})
	})

	t.Run("read notification back from channel", func(t *testing.T) {
		select {
		case notReq := <-webhookRecReqCh:
			require.Contains(t, notReq.Object.Name, "test-", "object name must contain test prefix")

		case <-time.After(5 * time.Minute):
			require.Fail(t, "no notification was received")
		}
	})
}
