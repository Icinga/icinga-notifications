package rule

import (
	"github.com/icinga/icinga-notifications/internal/recipient"
)

type NotificationRecipient struct {
	recipient.CommonRecipient
	RuleID int64 `db:"rule_id"`
}

func (r *NotificationRecipient) TableName() string {
	return "rule_recipient"
}
