package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"go.uber.org/zap"
)

// NotifyContacts delivers all the provided pending notifications to their corresponding contacts.
//
// Each of the given notifications will either be marked as StateSent or StateFailed in the database.
// When a specific notification fails to be sent, it won't interrupt the subsequent notifications, instead
// it will simply log the error and continue sending the remaining ones.
//
// Returns an error if the specified context is cancelled, otherwise always nil.
func NotifyContacts(ctx context.Context, res *config.Resources, req *plugin.NotificationRequest, notifications PendingNotifications) error {
	for contact, entries := range notifications {
		for _, notification := range entries {
			if NotifyContact(contact, res, req, notification.ChannelID) != nil {
				notification.State = StateFailed
			} else {
				notification.State = StateSent
			}
			notification.SentAt = types.UnixMilli(time.Now())

			stmt, _ := res.DB.BuildUpdateStmt(notification)
			if _, err := res.DB.NamedExecContext(ctx, stmt, notification); err != nil {
				res.Logger.Errorw("Failed to update contact notified history",
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
func NotifyContact(c *recipient.Contact, res *config.Resources, req *plugin.NotificationRequest, chID int64) error {
	ch := res.RuntimeConfig.Channels[chID]
	if ch == nil {
		res.Logger.Errorw("Cannot not find config for channel", zap.Int64("channel_id", chID))
		return fmt.Errorf("cannot not find config for channel ID '%d'", chID)
	}

	res.Logger.Infow(fmt.Sprintf("Notify contact %q via %q of type %q", c, ch.Name, ch.Type),
		zap.Int64("channel_id", chID), zap.Stringer("event_tye", req.Event.Type))

	contactStruct := &plugin.Contact{FullName: c.FullName}
	for _, addr := range c.Addresses {
		contactStruct.Addresses = append(contactStruct.Addresses, &plugin.Address{Type: addr.Type, Address: addr.Address})
	}
	req.Contact = contactStruct

	if err := ch.Notify(req); err != nil {
		res.Logger.Errorw("Failed to send notification via channel plugin", zap.String("type", ch.Type), zap.Error(err))
		return err
	}

	res.Logger.Infow("Successfully sent a notification via channel plugin", zap.String("type", ch.Type),
		zap.String("contact", c.String()), zap.Stringer("event_type", req.Event.Type))

	return nil
}

// NewPluginRequest creates a new [plugin.NotificationRequest] from the specified object.Object and event.Event.
//
// The returned request will not contain any contact information, please make sure to set the
// [plugin.NotificationRequest.Contact] and if required the [plugin.NotificationRequest.Incident]
// fields before passing it to a channel's Notify method.
func NewPluginRequest(obj *object.Object, ev *event.Event) *plugin.NotificationRequest {
	return &plugin.NotificationRequest{
		Object: &plugin.Object{
			Name: obj.DisplayName(),
			Url:  ev.URL,
			Tags: obj.Tags,
		},
		Event: &plugin.Event{
			Time:     ev.Time,
			Type:     ev.Type,
			Username: ev.Username,
			Message:  ev.Message,
		},
	}
}
