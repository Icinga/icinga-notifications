package notificationrule

import (
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/rule"
	"go.uber.org/zap"
)

func GetRecipientsByMatchingRules(rc *config.RuntimeConfig, l *zap.SugaredLogger, ev *event.Event) ([]*rule.NotificationRecipient, error) {
	rules := make(map[int64]*rule.Rule)

	src, ok := rc.Sources[ev.SourceId]
	if !ok {
		l.Warnw("Received event from unknown source, might got deleted", zap.Int64("source_id", ev.SourceId))
		return nil, nil
	}

	for id := range src.RuleIDs() {
		if _, ok := rules[id]; !ok {
			r, ok := rc.Rules[id]
			if !ok {
				l.Errorw("BUG: source references unknown event rule", zap.Object("source", src))
				continue
			}
			if r.Type != rule.TypeNotification {
				continue
			}

			if r.SourceType != src.Type {
				l.Errorw("BUG: source references notification rule with mismatching source type",
					zap.Object("source", src),
					zap.Object("rule", r))
				continue
			}
			matched, err := r.Eval(ev)
			if err != nil {
				l.Errorw("Failed to evaluate object filter", zap.Object("rule", r), zap.Error(err))
			}

			if err == nil && matched {
				rules[id] = r
			}
		}
	}

	return nil, nil
}
