// Package config defines the scenario file schema for loadcannon and loads/validates it.
//
// Scenario files are plain JSON (no external dependency required to parse them,
// which keeps loadcannon a single static binary with zero third-party deps).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// TargetType distinguishes internal (VPC/VPN-only) endpoints from public ones.
// It doesn't change how requests are made — it's metadata used for reporting
// and to trigger reachability hints in `loadcannon validate`.
type TargetType string

const (
	TargetInternal TargetType = "internal"
	TargetPublic   TargetType = "public"
)

type Target struct {
	Type               TargetType `json:"type"`
	URL                string     `json:"url"`                            // LB DNS name, public hostname, or raw IP (with scheme)
	HostOverride       string     `json:"host_override,omitempty"`        // sets Host header + TLS ServerName when hitting an IP directly behind a name-based LB/ingress
	InsecureSkipVerify bool       `json:"insecure_skip_verify,omitempty"` // for direct-IP hits where the cert CN/SAN won't match
}

// AuthMode selects how the Authorization/credentials are built.
type AuthMode string

const (
	AuthNone   AuthMode = "none"
	AuthBearer AuthMode = "bearer"
	AuthBasic  AuthMode = "basic"
	AuthAPIKey AuthMode = "apikey"
)

// SecretSource says where a referenced secret value actually lives.
// loadcannon never accepts raw secret values inline in the scenario file —
// only references, resolved at run time.
type SecretSource string

const (
	SourceEnv    SecretSource = "env"    // read from an environment variable by name
	SourceFile   SecretSource = "file"   // read from a file path (e.g. a mounted CI secret, chmod 600)
	SourceSSM    SecretSource = "ssm"    // AWS SSM Parameter Store, SecureString, resolved via `aws ssm get-parameter`
	SourcePrompt SecretSource = "prompt" // interactive masked prompt at run time
)

type Auth struct {
	AuthMode AuthMode `json:"mode"`

	Header string `json:"header,omitempty"` // header name for bearer/apikey modes, default "Authorization"
	Prefix string `json:"prefix,omitempty"` // value prefix, default "Bearer " for bearer mode

	TokenSource SecretSource `json:"token_source,omitempty"`
	TokenRef    string       `json:"token_ref,omitempty"`

	UsernameSource SecretSource `json:"username_source,omitempty"`
	UsernameRef    string       `json:"username_ref,omitempty"`
	PasswordSource SecretSource `json:"password_source,omitempty"`
	PasswordRef    string       `json:"password_ref,omitempty"`
}

type Scenario struct {
	Name         string            `json:"name"`
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Weight       int               `json:"weight,omitempty"` // relative selection weight during the run, default 1
	Headers      map[string]string `json:"headers,omitempty"`
	BodyFile     string            `json:"body_file,omitempty"`
	Body         string            `json:"body,omitempty"`
	ExpectStatus int               `json:"expect_status,omitempty"` // default 200
}

type Stage struct {
	Duration string `json:"duration"` // e.g. "30s"
	Target   int    `json:"target"`   // VUs to ramp to by end of stage
}

type Load struct {
	VUs      int     `json:"vus,omitempty"`      // used when Stages is empty (flat load)
	Duration string  `json:"duration,omitempty"` // used when Stages is empty
	Stages   []Stage `json:"stages,omitempty"`
}

type Config struct {
	Name       string            `json:"name"`
	Target     Target            `json:"target"`
	Auth       Auth              `json:"auth"`
	Scenarios  []Scenario        `json:"scenarios"`
	Load       Load              `json:"load"`
	Thresholds map[string]string `json:"thresholds,omitempty"`
}

// Load reads and validates a scenario file from disk.
func Load2(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading scenario file: %w", err)
	}
	var c Config
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parsing scenario file (must be valid JSON, see scenarios/example-*.json): %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if c.Target.URL == "" {
		return fmt.Errorf("target.url is required")
	}
	if c.Target.Type == "" {
		c.Target.Type = TargetPublic
	}
	if c.Target.Type != TargetInternal && c.Target.Type != TargetPublic {
		return fmt.Errorf("target.type must be %q or %q", TargetInternal, TargetPublic)
	}
	if !strings.HasPrefix(c.Target.URL, "http://") && !strings.HasPrefix(c.Target.URL, "https://") {
		return fmt.Errorf("target.url must include a scheme, e.g. https://10.0.1.23 or https://internal-lb.example.local")
	}
	if len(c.Scenarios) == 0 {
		return fmt.Errorf("at least one entry under scenarios is required")
	}
	for i, s := range c.Scenarios {
		if s.Name == "" {
			return fmt.Errorf("scenarios[%d].name is required", i)
		}
		if s.Method == "" {
			c.Scenarios[i].Method = "GET"
		}
		if s.Path == "" {
			return fmt.Errorf("scenarios[%d].path is required", i)
		}
		if s.Weight <= 0 {
			c.Scenarios[i].Weight = 1
		}
		if s.ExpectStatus == 0 {
			c.Scenarios[i].ExpectStatus = 200
		}
	}
	switch c.Auth.AuthMode {
	case "", AuthNone:
		c.Auth.AuthMode = AuthNone
	case AuthBearer, AuthAPIKey:
		if c.Auth.TokenRef == "" {
			return fmt.Errorf("auth.token_ref is required for auth.mode=%s", c.Auth.AuthMode)
		}
		if c.Auth.TokenSource == "" {
			return fmt.Errorf("auth.token_source is required for auth.mode=%s (env|file|ssm|prompt)", c.Auth.AuthMode)
		}
		if c.Auth.Header == "" {
			c.Auth.Header = "Authorization"
		}
		if c.Auth.AuthMode == AuthBearer && c.Auth.Prefix == "" {
			c.Auth.Prefix = "Bearer "
		}
	case AuthBasic:
		if c.Auth.UsernameRef == "" || c.Auth.PasswordRef == "" {
			return fmt.Errorf("auth.username_ref and auth.password_ref are required for auth.mode=basic")
		}
		if c.Auth.UsernameSource == "" || c.Auth.PasswordSource == "" {
			return fmt.Errorf("auth.username_source and auth.password_source are required for auth.mode=basic (env|file|ssm|prompt)")
		}
	default:
		return fmt.Errorf("unknown auth.mode %q (use none|bearer|basic|apikey)", c.Auth.AuthMode)
	}
	if len(c.Load.Stages) == 0 {
		if c.Load.VUs == 0 {
			c.Load.VUs = 10
		}
		if c.Load.Duration == "" {
			c.Load.Duration = "30s"
		}
	}
	return nil
}
