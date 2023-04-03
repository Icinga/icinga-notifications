package channel

var DummyChannels = []*Channel{{
	Name:   "E-Mail",
	Type:   "email",
	Config: `{"host": "localhost", "port": 25}`,
}, {
	Name:   "RocketChat",
	Type:   "rocketchat",
	Config: `{"url":"https://chat.example.com", "user_id": "invalid", "token": "invalid"}`,
}}
