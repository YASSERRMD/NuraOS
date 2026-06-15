// Package compliance enforces data-residency policy and produces verifiable
// audit reports of per-turn provider handling.
//
// Data-residency policy controls which AI providers may handle which turns.
// A turn is classified as "sensitive" based on configurable criteria; sensitive
// turns must stay on local or explicitly-designated sovereign providers. Cross-
// border egress (to cloud providers in other jurisdictions) is only permitted
// when the policy allows it and every such turn is logged to provenance.
//
// The package also provides a data-retention policy enforcer and a deletion
// command that purges expired sessions, journals, and provenance records
// according to a configurable retention window.
package compliance

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// ResidencyClass classifies a provider's data jurisdiction.
type ResidencyClass string

const (
	// ResidencyLocal indicates data stays on the local device.
	ResidencyLocal ResidencyClass = "local"
	// ResidencySovereign indicates data is processed in a designated
	// jurisdiction (e.g. EU-only cloud endpoint).
	ResidencySovereign ResidencyClass = "sovereign"
	// ResidencyCrossBorder indicates data may leave the local jurisdiction.
	ResidencyCrossBorder ResidencyClass = "cross-border"
)

// ProviderPolicy describes the residency class of a named provider.
type ProviderPolicy struct {
	// Name is the provider identifier (e.g. "local", "anthropic", "openai").
	Name string `json:"name"`
	// Residency is the data jurisdiction of this provider.
	Residency ResidencyClass `json:"residency"`
	// AllowSensitive permits sensitive turns to be routed to this provider.
	// True only for local and designated sovereign providers.
	AllowSensitive bool `json:"allow_sensitive"`
}

// Policy is the data-residency policy for a NuraOS deployment.
type Policy struct {
	// AllowCrossBorder permits cross-border egress when true. When false,
	// any attempt to route a turn to a cross-border provider returns an error.
	AllowCrossBorder bool `json:"allow_cross_border"`
	// CrossBorderMustBeLogged requires that cross-border turns are written to
	// the provenance log before being sent to the provider.
	CrossBorderMustBeLogged bool `json:"cross_border_must_be_logged"`
	// Providers is the per-provider policy list.
	Providers []ProviderPolicy `json:"providers"`
	// RetentionDays is the maximum age in days for sessions/journal/provenance.
	// 0 means no automatic deletion.
	RetentionDays int `json:"retention_days"`
}

// DefaultPolicy returns a conservative policy: no cross-border, local only.
func DefaultPolicy() Policy {
	return Policy{
		AllowCrossBorder:        false,
		CrossBorderMustBeLogged: true,
		RetentionDays:           90,
		Providers: []ProviderPolicy{
			{Name: "local", Residency: ResidencyLocal, AllowSensitive: true},
			{Name: "anthropic", Residency: ResidencyCrossBorder, AllowSensitive: false},
			{Name: "openai", Residency: ResidencyCrossBorder, AllowSensitive: false},
		},
	}
}

// Load reads a compliance policy from path. Returns DefaultPolicy() if the
// file does not exist.
func Load(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultPolicy(), nil
	}
	if err != nil {
		return Policy{}, fmt.Errorf("compliance: read %s: %w", path, err)
	}
	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return Policy{}, fmt.Errorf("compliance: parse %s: %w", path, err)
	}
	return p, nil
}

// Save writes the policy to path atomically.
func Save(path string, p Policy) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ProviderForName returns the policy for the named provider, or a default
// cross-border policy if the provider is not listed.
func (p *Policy) ProviderForName(name string) ProviderPolicy {
	for _, pp := range p.Providers {
		if strings.EqualFold(pp.Name, name) {
			return pp
		}
	}
	return ProviderPolicy{
		Name:           name,
		Residency:      ResidencyCrossBorder,
		AllowSensitive: false,
	}
}

// CheckRoute evaluates whether a turn may be routed to the named provider.
// sensitive indicates whether the turn contains sensitive content.
// Returns an error if the route violates the policy.
func (p *Policy) CheckRoute(provider string, sensitive bool) error {
	pp := p.ProviderForName(provider)

	if sensitive && !pp.AllowSensitive {
		return fmt.Errorf("compliance: sensitive turn cannot be routed to %s (residency=%s, allow_sensitive=false)",
			provider, pp.Residency)
	}

	if pp.Residency == ResidencyCrossBorder && !p.AllowCrossBorder {
		return fmt.Errorf("compliance: cross-border egress to %s is not permitted by policy",
			provider)
	}

	return nil
}

// TurnRecord is an entry in the compliance audit log.
type TurnRecord struct {
	// TurnID is the unique turn identifier from provenance.
	TurnID string `json:"turn_id"`
	// Provider is the name of the provider that handled this turn.
	Provider string `json:"provider"`
	// Residency is the residency class of the provider.
	Residency ResidencyClass `json:"residency"`
	// Sensitive indicates whether this turn was classified as sensitive.
	Sensitive bool `json:"sensitive"`
	// CrossBorder is true when the turn was routed cross-border.
	CrossBorder bool `json:"cross_border"`
	// At is the UTC timestamp of the turn.
	At time.Time `json:"at"`
}

// AuditLog maintains an append-only log of turn routing decisions.
type AuditLog struct {
	path string
}

// NewAuditLog returns an AuditLog backed by path.
func NewAuditLog(path string) *AuditLog {
	return &AuditLog{path: path}
}

// Append adds a turn record to the audit log.
func (l *AuditLog) Append(rec TurnRecord) error {
	if l == nil {
		return nil
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return fmt.Errorf("compliance: open audit log %s: %w", l.path, err)
	}
	defer f.Close()
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

// Report reads the audit log and returns all turn records.
func (l *AuditLog) Report() ([]TurnRecord, error) {
	data, err := os.ReadFile(l.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("compliance: read audit log: %w", err)
	}
	var records []TurnRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec TurnRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records, nil
}
