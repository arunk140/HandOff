package config

import (
	"testing"
	"time"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				Listen: ListenConfig{Host: "0.0.0.0", Port: 8080},
				Global: GlobalConfig{Timeout: Duration(30 * time.Second)},
				Routes: []Route{
					{Path: "/api/**", Backend: "https://example.com"},
				},
			},
			wantErr: false,
		},
		{
			name: "missing port",
			cfg: Config{
				Listen: ListenConfig{Host: "0.0.0.0"},
				Routes: []Route{{Path: "/", Backend: "https://example.com"}},
			},
			wantErr: true,
		},
		{
			name: "tls enabled missing cert",
			cfg: Config{
				Listen: ListenConfig{Host: "0.0.0.0", Port: 8443, TLS: TLSConfig{Enabled: true}},
				Routes: []Route{{Path: "/", Backend: "https://example.com"}},
			},
			wantErr: true,
		},
		{
			name:    "no routes and no default_backend",
			cfg:     Config{Listen: ListenConfig{Port: 8080}},
			wantErr: true,
		},
		{
			name: "valid config with default_backend and no routes",
			cfg: Config{
				Listen:         ListenConfig{Port: 8080},
				DefaultBackend: "https://example.com",
			},
			wantErr: false,
		},
		{
			name: "valid config with routes and default_backend fallback",
			cfg: Config{
				Listen:         ListenConfig{Port: 8080},
				DefaultBackend: "https://fallback.example.com",
				Routes: []Route{
					{Path: "/api/**", Backend: "https://api.example.com"},
				},
			},
			wantErr: false,
		},
		{
			name: "route missing path",
			cfg: Config{
				Listen: ListenConfig{Port: 8080},
				Routes: []Route{{Backend: "https://example.com"}},
			},
			wantErr: true,
		},
		{
			name: "route missing backend",
			cfg: Config{
				Listen: ListenConfig{Port: 8080},
				Routes: []Route{{Path: "/api/**"}},
			},
			wantErr: true,
		},
		{
			name: "invalid webhook payload mode",
			cfg: Config{
				Listen: ListenConfig{Port: 8080},
				Routes: []Route{
					{
						Path:    "/api/**",
						Backend: "https://example.com",
						Webhooks: []WebhookConfig{
							{URL: "https://hooks.example.com", Payload: "invalid"},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "valid webhook payload modes",
			cfg: Config{
				Listen: ListenConfig{Port: 8080},
				Routes: []Route{
					{
						Path:    "/api/**",
						Backend: "https://example.com",
						Webhooks: []WebhookConfig{
							{URL: "https://hooks.example.com", Payload: "metadata"},
							{URL: "https://hooks.example.com", Payload: "body"},
							{URL: "https://hooks.example.com", Payload: "custom"},
							{URL: "https://hooks.example.com"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "default host set",
			cfg: Config{
				Listen: ListenConfig{Port: 8080},
				Routes: []Route{{Path: "/", Backend: "https://example.com"}},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultHost(t *testing.T) {
	cfg := Config{
		Listen: ListenConfig{Port: 8080},
		Routes: []Route{{Path: "/", Backend: "https://example.com"}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.Host != "0.0.0.0" {
		t.Errorf("expected default host 0.0.0.0, got %s", cfg.Listen.Host)
	}
}

func TestDefaultTimeout(t *testing.T) {
	cfg := Config{
		Listen: ListenConfig{Port: 8080},
		Routes: []Route{{Path: "/", Backend: "https://example.com"}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if time.Duration(cfg.Global.Timeout) != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", time.Duration(cfg.Global.Timeout))
	}
}

func TestWebhookURLRequired(t *testing.T) {
	cfg := Config{
		Listen: ListenConfig{Port: 8080},
		Routes: []Route{
			{
				Path:    "/api/**",
				Backend: "https://example.com",
				Webhooks: []WebhookConfig{
					{Payload: "metadata"},
				},
			},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for webhook missing URL")
	}
}

func TestDefaultBackendOnly(t *testing.T) {
	cfg := Config{
		Listen:         ListenConfig{Port: 8080},
		DefaultBackend: "https://example.com",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config with only default_backend, got: %v", err)
	}
}

func TestNoRoutesNoDefaultBackend(t *testing.T) {
	cfg := Config{Listen: ListenConfig{Port: 8080}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for no routes and no default_backend")
	}
}

func TestInvalidMethod(t *testing.T) {
	cfg := Config{
		Listen: ListenConfig{Port: 8080},
		Routes: []Route{
			{
				Path:    "/api/**",
				Backend: "https://example.com",
				Methods: []string{"GET", "INVALID"},
			},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid method")
	}
}
