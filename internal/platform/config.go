// (file intentionally left blank for now; config logic will be added here)

package platform

// import "time"

// FlagsConfig holds all boolean or string flags for the app.

type FlagsConfig struct {
	// Headless disables the HTTP server when true.
	Headless bool
	// Add more flags here as needed
}

// AppConfig contains the configuration for the app.
type AppConfig struct {
	Flags      *FlagsConfig
	NatsCfg    *EmbeddedServerConfig
	HTTPSrvCfg *HTTPServerConfig
}

// LoadAppConfig loads application configuration from environment variables and returns an AppConfig.
func LoadAppConfig() *AppConfig {
	return &AppConfig{
		Flags:      defaultFlagsCfg(),
		NatsCfg:    defaultNatsCfg(),
		HTTPSrvCfg: defaultHTTPServerCfg(),
	}
}

// defaultFlagsCfg returns the default FlagsConfig (from env).
func defaultFlagsCfg() *FlagsConfig {
	return &FlagsConfig{
		Headless: false, // strings.ToLower(os.Getenv("HEADLESS")) == "true",
	}
}

// defaultHTTPServerCfg returns sane defaults for the HTTP server.
func defaultHTTPServerCfg() *HTTPServerConfig {
	return &HTTPServerConfig{
		Port:         8080,
		ReadTimeout:  -1,
		WriteTimeout: -1,
		IdleTimeout:  -1,
		EnableTLS:    true,
		CertFile:     "./local_certs/localhost+2.pem",
		KeyFile:      "./local_certs/localhost+2-key.pem",
	}
}

// defaultNatsCfg returns the default EmbeddedServerConfig.
func defaultNatsCfg() *EmbeddedServerConfig {
	return &EmbeddedServerConfig{
		InProcess:       false,
		EnableLogging:   true,
		JetStream:       true,
		JetStreamDomain: "",
		StoreDir:        "./store/js",
	}
}
