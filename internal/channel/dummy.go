package channel

var DummyChannels = []*Channel{{
	Name:   "E-Mail",
	Type:   "email",
	Config: "",
}, {
	Name:   "RocketChat",
	Type:   "rocketchat",
	Config: `{"url":"https://chat.example.com", "user_id": "invalid", "token": "invalid"}`,
}}
