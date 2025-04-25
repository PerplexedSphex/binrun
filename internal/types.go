package core

// AppConfig holds top-level application configuration.
type AppConfig struct {
	Flags   *FlagsConfig
	NatsCfg *EmbeddedServerConfig
	SimCfg  *SimConfig
}
