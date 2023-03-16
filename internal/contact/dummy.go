package contact

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
		},
	}

	TeamOps = &Group{
		Name:    "Team Ops",
		Members: []*Contact{John, Jane},
	}
)
