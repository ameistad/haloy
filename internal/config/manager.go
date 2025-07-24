package config

type ManagerConfig struct {
	API struct {
		Domain string `json:"domain"`
	} `json:"api"`
	Certificates struct {
		AcmeEmail string `json:"acmeEmail"`
	} `json:"certificates"`
}
