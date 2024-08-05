package notification

import (
	"context"
	"fmt"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"go.uber.org/zap"
	"time"
)

// Notifier is helper type used to send notifications requests to their recipients.
type Notifier struct {
	DB            *database.DB          `db:"-" json:"-"`
	RuntimeConfig *config.RuntimeConfig `db:"-" json:"-"`
	Logger        *zap.SugaredLogger    `db:"-" json:"-"`
}

// NotifyContacts delivers all the provided pending notifications to their corresponding contacts.
//
// Each of the given notifications will either be marked as StateSent or StateFailed in the database.
// When a specific notification fails to be sent, it won't interrupt the subsequent notifications, instead
// it will simply log the error and continue sending the remaining ones.
//
// Returns an error if the specified context is cancelled, otherwise always nil.
func (n *Notifier) NotifyContacts(ctx context.Context, req *plugin.NotificationRequest, notifications PendingNotifications) error {
	for contact, entries := range notifications {
		for _, notification := range entries {
			if n.NotifyContact(contact, req, notification.ChannelID) != nil {
				notification.State = StateFailed
			} else {
				notification.State = StateSent
			}
			notification.SentAt = types.UnixMilli(time.Now())

			stmt, _ := n.DB.BuildUpdateStmt(notification)
			if _, err := n.DB.NamedExecContext(ctx, stmt, notification); err != nil {
				n.Logger.Errorw("Failed to update contact notified history",
					zap.String("contact", contact.String()), zap.Error(err))
			}
		}

		if err := ctx.Err(); err != nil {
			return err
		}
	}

	return nil
}

// NotifyContact notifies the given recipient via a channel matching the given ID.
//
// Please make sure to call this method while holding the config.RuntimeConfig lock.
// Returns an error if unable to find a channel with the specified ID or fails to send the notification.
func (n *Notifier) NotifyContact(c *recipient.Contact, req *plugin.NotificationRequest, chID int64) error {
	ch := n.RuntimeConfig.Channels[chID]
	if ch == nil {
		n.Logger.Errorw("Cannot not find config for channel", zap.Int64("channel_id", chID))
		return fmt.Errorf("cannot not find config for channel ID '%d'", chID)
	}

	n.Logger.Infow(fmt.Sprintf("Notify contact %q via %q of type %q", c, ch.Name, ch.Type),
		zap.Int64("channel_id", chID), zap.String("event_tye", req.Event.Type))

	contactStruct := &plugin.Contact{FullName: c.FullName}
	for _, addr := range c.Addresses {
		contactStruct.Addresses = append(contactStruct.Addresses, &plugin.Address{Type: addr.Type, Address: addr.Address})
	}
	req.Contact = contactStruct

	if err := ch.Notify(req); err != nil {
		n.Logger.Errorw("Failed to send notification via channel plugin", zap.String("type", ch.Type), zap.Error(err))
		return err
	}

	n.Logger.Infow("Successfully sent a notification via channel plugin", zap.String("type", ch.Type),
		zap.String("contact", c.String()), zap.String("event_type", req.Event.Type))

	return nil
}

// NewPluginRequest returns a new plugin.NotificationRequest from the given arguments.
func NewPluginRequest(obj *object.Object, ev *event.Event) *plugin.NotificationRequest {
	return &plugin.NotificationRequest{
		Object: &plugin.Object{
			Name:      obj.DisplayName(),
			Url:       ev.URL,
			Tags:      obj.Tags,
			ExtraTags: obj.ExtraTags,
		},
		Event: &plugin.Event{
			Time:     ev.Time,
			Type:     ev.Type,
			Username: ev.Username,
			Message:  ev.Message,
		},
	}
}
