package common

type State struct {
	IP        string         `json:"ip"`
	VpnHost   string         `json:"vpn_host"`
	FrontHost string         `json:"front_host"`
	Token     string         `json:"token"`
	NodeCache []string       `json:"nodes"`
	Slots     map[string]int `json:"slots"`
	LastNode  string         `json:"last_node"`
}

type Device struct {
	Id     int    `json:"id"`
	Config string `json:"config"`
	Ip     string `json:"ip"`
}

type Status struct {
	Success bool `json:"success"`
}

type ChiselConfig struct {
	IP       string `json:"ip"`
	Front    string `json:"front"`
	Username string `json:"username"`
	Password string `json:"password"`
	Nodes    int    `json:"nodes"`
}
