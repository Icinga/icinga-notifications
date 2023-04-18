package rule

import (
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/object"
	"github.com/icinga/noma/internal/recipient"
	"github.com/icinga/noma/internal/timeperiod"
	"time"
)

var (
	ProductionOnCall = &Rule{
		Name:         "Production On-Call",
		ObjectFilter: object.MustParseFilter("hostgroup/production"),
		Escalations: []*Escalation{{
			Recipients: []*EscalationRecipient{{
				ChannelType: "email",
				Recipient:   recipient.OnCall,
			}},
		}},
	}

	LinuxOfficeHours = &Rule{
		Name:         "Linux Office Hours",
		TimePeriod:   timeperiod.OfficeHours,
		ObjectFilter: object.MustParseFilter("hostgroup/linux"),
		Escalations: []*Escalation{{
			Name: "Level 1",
			Recipients: []*EscalationRecipient{{
				ChannelType: "email",
				Recipient:   recipient.John,
			}},
		}, {
			Name:      "Level 2",
			Condition: &Condition{MinDuration: 1 * time.Second},
			Recipients: []*EscalationRecipient{{
				ChannelType: "email",
				Recipient:   recipient.Jane,
			}},
		}, {
			Name:      "Level 3",
			Condition: &Condition{MinDuration: 2 * time.Second},
			Recipients: []*EscalationRecipient{{
				ChannelType: "sms",
				Recipient:   recipient.John,
			}},
		}, {
			Name:      "Level 4",
			Condition: &Condition{MinDuration: 3 * time.Second},
			Recipients: []*EscalationRecipient{{
				ChannelType: "sms",
				Recipient:   recipient.Jane,
			}},
		}},
	}

	WindowsSeverity = &Rule{
		Name:         "Windows With Severity Filters",
		TimePeriod:   timeperiod.OfficeHours,
		ObjectFilter: object.MustParseFilter("hostgroup/windows"),
		Escalations: []*Escalation{{
			Condition: &Condition{MinSeverity: event.SeverityWarning},
			Recipients: []*EscalationRecipient{{
				ChannelType: "rocketchat",
				Recipient:   recipient.John,
			}},
		}, {
			Condition: &Condition{MinSeverity: event.SeverityCrit},
			Recipients: []*EscalationRecipient{{
				ChannelType: "rocketchat",
				Recipient:   recipient.TeamOps,
			}},
		}},
	}

	Everything = &Rule{
		Name: "Just Send Everything",
		Escalations: []*Escalation{{
			Recipients: []*EscalationRecipient{{
				ChannelType: "email",
				Recipient:   recipient.John,
			}},
		}, {
			Recipients: []*EscalationRecipient{{
				ChannelType: "rocketchat",
				Recipient:   recipient.Jane,
			}},
		}},
	}

	Rules = []*Rule{
		ProductionOnCall,
		LinuxOfficeHours,
		WindowsSeverity,
		Everything,
	}
)
