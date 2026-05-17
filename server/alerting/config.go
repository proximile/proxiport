// TODO(v0.2): Add a YAML loader for Config. Picking YAML over TOML
// so the rule and sink blocks can nest cleanly. See
// docs/alerting-design.md for the example config block and for the
// secret-handling rules sink configs must follow.

package alerting

import "time"

// Config holds operator-supplied alerting settings. While the package
// is scaffolding, no loader populates this; the struct exists to pin
// down the field set a future loader will use.
type Config struct {
	// Enabled gates the whole pipeline. When false, the disabled
	// dispatcher is used regardless of Sinks / Rules contents.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// DefaultTimeout caps per-sink Send calls. Zero means "use the
	// dispatcher's built-in default".
	DefaultTimeout time.Duration `mapstructure:"default_timeout" yaml:"default_timeout"`

	// Sinks is the list of configured delivery sinks.
	Sinks SinkConfigs `mapstructure:"sinks" yaml:"sinks"`

	// Rules is the inline rule list. Operators may also load rules
	// from a separate file via RulesFile; if both are set the file
	// wins.
	Rules []Rule `mapstructure:"rules" yaml:"rules"`

	// RulesFile is an absolute path to a YAML rule file. When set,
	// the inline Rules field is ignored.
	RulesFile string `mapstructure:"rules_file" yaml:"rules_file"`
}

// SinkConfigs groups one optional config block per built-in sink.
// Adding a new sink means adding a new field here and a new file
// under server/alerting/sinks.
type SinkConfigs struct {
	Email   *EmailSinkConfig   `mapstructure:"email" yaml:"email"`
	Slack   *SlackSinkConfig   `mapstructure:"slack" yaml:"slack"`
	Webhook *WebhookSinkConfig `mapstructure:"webhook" yaml:"webhook"`
}

// EmailSinkConfig configures the SMTP sink.
type EmailSinkConfig struct {
	Host        string   `mapstructure:"host" yaml:"host"`
	Port        int      `mapstructure:"port" yaml:"port"`
	Username    string   `mapstructure:"username" yaml:"username"`
	PasswordEnv string   `mapstructure:"password_env" yaml:"password_env"`
	From        string   `mapstructure:"from" yaml:"from"`
	To          []string `mapstructure:"to" yaml:"to"`
	StartTLS    bool     `mapstructure:"starttls" yaml:"starttls"`
}

// SlackSinkConfig configures the Slack sink. The webhook URL is read
// from the environment to keep secrets out of the config file.
type SlackSinkConfig struct {
	WebhookURLEnv string `mapstructure:"webhook_url_env" yaml:"webhook_url_env"`
	Channel       string `mapstructure:"channel" yaml:"channel"`
	Username      string `mapstructure:"username" yaml:"username"`
}

// WebhookSinkConfig configures the generic webhook sink.
type WebhookSinkConfig struct {
	URL           string            `mapstructure:"url" yaml:"url"`
	Method        string            `mapstructure:"method" yaml:"method"`
	Headers       map[string]string `mapstructure:"headers" yaml:"headers"`
	AuthBearerEnv string            `mapstructure:"auth_bearer_env" yaml:"auth_bearer_env"`
}
