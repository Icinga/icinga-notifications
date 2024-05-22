package incident

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/notification"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"go.uber.org/zap"
	"net/url"
)

// makeNotificationRequest generates a *plugin.NotificationRequest for the provided event.
// Fails fatally when fails to parse the Icinga Web 2 url.
func (i *Incident) makeNotificationRequest(ev *event.Event) *plugin.NotificationRequest {
	baseUrl, err := url.Parse(daemon.Config().Icingaweb2URL)
	if err != nil {
		i.Logger.Panicw("Failed to parse Icinga Web 2 URL", zap.String("url", daemon.Config().Icingaweb2URL), zap.Error(err))
	}

	incidentUrl := baseUrl.JoinPath("/notifications/incident")
	incidentUrl.RawQuery = fmt.Sprintf("id=%d", i.ID)

	req := notification.NewPluginRequest(i.Object, ev)
	req.Incident = &plugin.Incident{
		Id:       i.ID,
		Url:      incidentUrl.String(),
		Severity: i.Severity.String(),
	}

	return req
}
