package contact

type Contact struct {
	FullName  string
	Username  string
	Addresses []*Address
}

type Address struct {
	Type    string
	Address string
}

type Group struct {
	Name    string
	Members []*Contact
}
