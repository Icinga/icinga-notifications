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
			ChannelType: "email",
			Schedules:   []*recipient.Schedule{recipient.OnCall},
		}},
	}

	LinuxOfficeHours = &Rule{
		Name:         "Linux Office Hours",
		TimePeriod:   timeperiod.OfficeHours,
		ObjectFilter: object.MustParseFilter("hostgroup/linux"),
		Escalations: []*Escalation{{
			Name:        "Level 1",
			Contacts:    []*recipient.Contact{recipient.John},
			ChannelType: "email",
		}, {
			Name:        "Level 2",
			Condition:   &Condition{MinDuration: 1 * time.Second},
			Contacts:    []*recipient.Contact{recipient.Jane},
			ChannelType: "email",
		}, {
			Name:        "Level 3",
			Condition:   &Condition{MinDuration: 2 * time.Second},
			Contacts:    []*recipient.Contact{recipient.John},
			ChannelType: "sms",
		}, {
			Name:        "Level 4",
			Condition:   &Condition{MinDuration: 3 * time.Second},
			Contacts:    []*recipient.Contact{recipient.Jane},
			ChannelType: "sms",
		}},
	}

	WindowsSeverity = &Rule{
		Name:         "Windows With Severity Filters",
		TimePeriod:   timeperiod.OfficeHours,
		ObjectFilter: object.MustParseFilter("hostgroup/windows"),
		Escalations: []*Escalation{{
			Condition:   &Condition{MinSeverity: event.SeverityWarning},
			Contacts:    []*recipient.Contact{recipient.John},
			ChannelType: "rocketchat",
		}, {
			Condition:     &Condition{MinSeverity: event.SeverityCrit},
			ContactGroups: []*recipient.Group{recipient.TeamOps},
			ChannelType:   "rocketchat",
		}},
	}

	Rules = []*Rule{
		ProductionOnCall,
		LinuxOfficeHours,
		WindowsSeverity,
	}
)
