package config

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(dur)
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

type Config struct {
	Listen ListenConfig `yaml:"listen"`
	Global GlobalConfig `yaml:"global"`
	Routes []Route      `yaml:"routes"`
}

type ListenConfig struct {
	Host string    `yaml:"host"`
	Port int       `yaml:"port"`
	TLS  TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type GlobalConfig struct {
	Timeout         Duration `yaml:"timeout"`
	FollowRedirects bool     `yaml:"follow_redirects"`
}

type Route struct {
	Path     string          `yaml:"path"`
	Methods  []string        `yaml:"methods"`
	Backend  string          `yaml:"backend"`
	Webhooks []WebhookConfig `yaml:"webhooks"`
}

type WebhookConfig struct {
	Type     string            `yaml:"type"`
	URL      string            `yaml:"url"`
	Method   string            `yaml:"method"`
	Headers  map[string]string `yaml:"headers"`
	Payload  string            `yaml:"payload"`
	Template string            `yaml:"template"`
	Retry    RetryConfig       `yaml:"retry"`
}

type RetryConfig struct {
	Attempts int    `yaml:"attempts"`
	Backoff  string `yaml:"backoff"`
}

var validMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}

func (c *Config) Validate() error {
	if c.Listen.Host == "" {
		c.Listen.Host = "0.0.0.0"
	}
	if c.Listen.Port <= 0 || c.Listen.Port > 65535 {
		return fmt.Errorf("listen.port must be between 1 and 65535")
	}
	if c.Listen.TLS.Enabled {
		if c.Listen.TLS.CertFile == "" {
			return fmt.Errorf("listen.tls.cert_file is required when TLS is enabled")
		}
		if c.Listen.TLS.KeyFile == "" {
			return fmt.Errorf("listen.tls.key_file is required when TLS is enabled")
		}
	}
	if time.Duration(c.Global.Timeout) <= 0 {
		c.Global.Timeout = Duration(30 * time.Second)
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("at least one route is required")
	}
	for i, route := range c.Routes {
		if route.Path == "" {
			return fmt.Errorf("routes[%d].path is required", i)
		}
		if route.Backend == "" {
			return fmt.Errorf("routes[%d].backend is required", i)
		}
		for _, m := range route.Methods {
			if !validMethods[strings.ToUpper(m)] {
				return fmt.Errorf("routes[%d].methods contains invalid method: %s", i, m)
			}
		}
		for j, wh := range route.Webhooks {
			if wh.URL == "" {
				return fmt.Errorf("routes[%d].webhooks[%d].url is required", i, j)
			}
			switch wh.Payload {
			case "", "metadata", "body", "custom":
			default:
				return fmt.Errorf("routes[%d].webhooks[%d].payload must be metadata, body, or custom", i, j)
			}
		}
	}
	return nil
}
