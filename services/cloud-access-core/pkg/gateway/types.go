package gateway

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config controls runtime behaviour of the gateway server.
type Config struct {
	Enabled             bool
	RoutesPath          string
	AdminToken          string
	DefaultCapacity     float64
	DefaultRefillPerSec float64
	DefaultWindow       time.Duration
	CertBundlePath      string
}

// RouteSet represents the serialised routes document.
type RouteSet struct {
	Version string            `json:"version" yaml:"version"`
	Routes  []RouteDefinition `json:"routes" yaml:"routes"`
}

// RouteDefinition describes a single proxied route.
type RouteDefinition struct {
	Name            string            `json:"name" yaml:"name"`
	Description     string            `json:"description" yaml:"description"`
	Methods         []string          `json:"methods" yaml:"methods"`
	Path            string            `json:"path" yaml:"path"`
	StripPathPrefix string            `json:"strip_path_prefix" yaml:"strip_path_prefix"`
	Upstream        UpstreamTarget    `json:"upstream" yaml:"upstream"`
	Auth            RouteAuthConfig   `json:"auth" yaml:"auth"`
	RateLimit       RateLimitConfig   `json:"rate_limit" yaml:"rate_limit"`
	Headers         map[string]string `json:"headers" yaml:"headers"`
}

// UpstreamTarget identifies the backend that will receive proxied traffic.
type UpstreamTarget struct {
	URL                string `json:"url" yaml:"url"`
	TimeoutSeconds     int    `json:"timeout_seconds" yaml:"timeout_seconds"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify" yaml:"insecure_skip_verify"`
}

// RouteAuthConfig captures authorisation requirements for a route.
type RouteAuthConfig struct {
	AllowAnonymous bool     `json:"allow_anonymous" yaml:"allow_anonymous"`
	TokenFormat    string   `json:"token_format" yaml:"token_format"`
	Action         string   `json:"action" yaml:"action"`
	Resource       string   `json:"resource" yaml:"resource"`
	RequiredRoles  []string `json:"required_roles" yaml:"required_roles"`
}

// RateLimitConfig defines distributed throttling behaviour for a route.
type RateLimitConfig struct {
	Capacity        float64 `json:"capacity" yaml:"capacity"`
	RefillPerSecond float64 `json:"refill_per_second" yaml:"refill_per_second"`
	WindowSeconds   int     `json:"window_seconds" yaml:"window_seconds"`
	PerTenant       bool    `json:"per_tenant" yaml:"per_tenant"`
	PerUser         bool    `json:"per_user" yaml:"per_user"`
	Amount          float64 `json:"amount" yaml:"amount"`
}

// LoadRouteSetFromFile attempts to parse either YAML or JSON into a RouteSet.
func LoadRouteSetFromFile(path string) (*RouteSet, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	return ParseRouteSet(data)
}

// ParseRouteSet parses JSON or YAML documents into a RouteSet.
func ParseRouteSet(raw []byte) (*RouteSet, error) {
	set := &RouteSet{}
	if err := yaml.Unmarshal(raw, set); err != nil {
		return nil, fmt.Errorf("parse routes: %w", err)
	}
	return set, nil
}

// MarshalRouteSet marshals the set to YAML for persistence.
func MarshalRouteSet(set *RouteSet) ([]byte, error) {
	if set == nil {
		return nil, fmt.Errorf("route set missing")
	}
	body, err := yaml.Marshal(set)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// CloneRouteSet performs a deep copy using JSON marshal/unmarshal to ensure immutability.
func CloneRouteSet(set *RouteSet) (*RouteSet, error) {
	if set == nil {
		return nil, fmt.Errorf("route set missing")
	}
	body, err := json.Marshal(set)
	if err != nil {
		return nil, err
	}
	var clone RouteSet
	if err := json.Unmarshal(body, &clone); err != nil {
		return nil, err
	}
	return &clone, nil
}
