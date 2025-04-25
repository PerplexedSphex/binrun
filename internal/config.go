// (file intentionally left blank for now; config logic will be added here)

package core

// FlagsConfig holds all boolean or string flags for the app.
type FlagsConfig struct {
	Sim bool
	// Add more flags here as needed
}

// LoadAppConfig loads application configuration from environment variables and returns an AppConfig.
func LoadAppConfig() *AppConfig {
	return &AppConfig{
		Flags:   defaultFlagsCfg(),
		NatsCfg: defaultNatsCfg(),
		SimCfg:  defaultSimCfg(),
	}
}

// defaultFlagsCfg returns the default FlagsConfig (from env).
func defaultFlagsCfg() *FlagsConfig {
	return &FlagsConfig{
		Sim: false, // strings.ToLower(os.Getenv("SIM")) == "true",
	}
}

// defaultNatsCfg returns the default EmbeddedServerConfig.
func defaultNatsCfg() *EmbeddedServerConfig {
	return &EmbeddedServerConfig{
		InProcess:       true,
		EnableLogging:   true,
		JetStream:       true,
		JetStreamDomain: "",
		StoreDir:        "./store/js",
	}
}

// defaultSimCfg returns the default SimConfig.
func defaultSimCfg() *SimConfig {
	return &SimConfig{
		NumSessions:           10,
		NumSubjectsPerSession: 3,
		NumEventsPerSubject:   5,
		NumCommands:           5,
		SessionChurn:          2,
		InspectionLevel:       1,
	}
}
