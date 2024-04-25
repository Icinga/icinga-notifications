package notifyutils

import (
	"context"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/history"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// NotifyContacts executes all the provided pending notifications without an active incident.
//
// Each of the given notifications will either be marked as history.NotificationStateSent or
// history.NotificationStateFailed in the database. When a specific notification fails to be sent, it won't
// interrupt the subsequent notifications, it will simply log the error and continue sending the remaining ones.
// Returns an error if the specified context is cancelled, otherwise always nil.
func NotifyContacts(
	ctx context.Context, notifyCtx *contracts.DefaultNotifyCtx, db *icingadb.DB, rc *config.RuntimeConfig, ev *event.Event,
	notifications history.PendingNotifications,
) error {
	for contact, entries := range notifications {
		for _, notification := range entries {
			if NotifyContact(contact, notifyCtx, rc, ev, notification) != nil {
				notification.State = history.NotificationStateFailed
			} else {
				notification.State = history.NotificationStateSent
			}

			notification.SentAt = types.UnixMilli(time.Now())
			notification.Message = utils.ToDBString(ev.Message)

			stmt, _ := db.BuildUpdateStmt(notification)
			if _, err := db.NamedExecContext(ctx, stmt, notification); err != nil {
				notifyCtx.Logger().Errorw("Failed to update contact notified history",
					zap.String("contact", contact.String()), zap.Error(err))
			}
		}

		if err := ctx.Err(); err != nil {
			return errors.Wrap(event.ErrEventProcessing, err.Error())
		}
	}

	return nil
}

// NotifyContact notifies the given recipient via a channel matching the given ID.
// Please make sure not to call this method while holding the config.RuntimeConfig lock.
// Returns an error if unable to find a channel with the specified ID or fails to send the notification.
func NotifyContact(
	c *recipient.Contact, notifyCtx contracts.NotifyContext, rc *config.RuntimeConfig, ev *event.Event,
	entry *history.NotificationEntry,
) error {
	ch := rc.Channels[entry.ChannelID]
	if ch == nil {
		notifyCtx.Logger().Errorw("Could not find config for channel", zap.Int64("channel_id", entry.ChannelID))
		return errors.Wrapf(event.ErrEventProcessing, "cannot not find config for channel ID: %d", entry.ChannelID)
	}

	notifyCtx.Logger().Infow(fmt.Sprintf("Notify contact %q via %q of type %q", c, ch.Name, ch.Type),
		zap.Int64("channel_id", entry.ChannelID), zap.String("event_tye", ev.Type))

	if err := ch.Notify(c, notifyCtx, ev, daemon.Config().Icingaweb2URL); err != nil {
		notifyCtx.Logger().Errorw("Failed to send notification via channel plugin", zap.String("type", ch.Type),
			zap.Error(err))

		return err
	}

	notifyCtx.Logger().Infow("Successfully sent a notification via channel plugin", zap.String("type", ch.Type),
		zap.String("contact", c.String()), zap.String("event_type", ev.Type))

	return nil
}
