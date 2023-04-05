package recipient

import "github.com/icinga/noma/internal/timeperiod"

var (
	John = &Contact{
		FullName: "John Doe",
		Username: "john.doe",
		Addresses: []*Address{
			{Type: "email", Address: "john.doe@example.com"},
		},
	}

	Jane = &Contact{
		FullName: "Jane Smith",
		Username: "jane.smith",
		Addresses: []*Address{
			{Type: "email", Address: "jane.smith@example.com"},
			{Type: "rocketchat", Address: "@jsmith"},
		},
	}

	TeamOps = &Group{
		Name:    "Team Ops",
		Members: []*Contact{John, Jane},
	}

	OnCall = &Schedule{
		Name: "On Call",
		Members: []*Member{{
			TimePeriod: timeperiod.EveryEvenHour,
			Contact:    John,
		}, {
			TimePeriod: timeperiod.EveryOddHour,
			Contact:    Jane,
		}},
	}
)
