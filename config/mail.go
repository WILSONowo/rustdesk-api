package config

type Mail struct {
	Enabled                         bool   `mapstructure:"enabled"`
	RequireRegistrationVerification bool   `mapstructure:"require-registration-verification"`
	Host                            string `mapstructure:"host"`
	Port                            int    `mapstructure:"port"`
	Username                        string `mapstructure:"username"`
	Password                        string `mapstructure:"password"`
	From                            string `mapstructure:"from"`
	FromName                        string `mapstructure:"from-name"`
	TLS                             string `mapstructure:"tls"`
	SendIntervalSeconds             int    `mapstructure:"send-interval-seconds"`
	DailyLimit                      int    `mapstructure:"daily-limit"`
	PublicURL                       string `mapstructure:"public-url"`
}
