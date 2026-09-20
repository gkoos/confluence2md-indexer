package query

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gkoos/confluence2md-indexer/internal/db"
)

// Prior identifies one metadata ranking signal. Prior values are normalised to
// [0,1] and fold into the fused score multiplicatively, so a prior can nudge an
// ordering without ever overriding text relevance.
type Prior string

const (
	PriorRecency   Prior = "recency"
	PriorAuthority Prior = "authority"
	PriorSeed      Prior = "seed"
	PriorDepth     Prior = "depth"
	PriorRichness  Prior = "richness"
)

const (
	// DefaultPriorStrength bounds how far the priors can move a score: at most 15%
	// of the fused value, which reorders near-ties without burying a clearly better
	// text match.
	DefaultPriorStrength = 0.15
	// DefaultRecencyHalfLife is the age at which a page keeps half of its recency
	// value.
	DefaultRecencyHalfLife = 180 * 24 * time.Hour
	// authorityLinkCap is the in-degree that already counts as fully authoritative.
	authorityLinkCap = 10
	// richnessCap is the attachment-plus-comment count that already counts as fully
	// rich.
	richnessCap = 5
)

// AvailablePriors lists the priors in the order they are documented.
func AvailablePriors() []string {
	return []string{
		string(PriorRecency),
		string(PriorAuthority),
		string(PriorSeed),
		string(PriorDepth),
		string(PriorRichness),
	}
}

// ParsePriors turns a comma separated list into the priors to apply. An empty list
// means "no priors", which is the default and keeps rankings unchanged.
func ParsePriors(list string) ([]Prior, error) {
	seen := make(map[Prior]bool, len(AvailablePriors()))
	priors := make([]Prior, 0, len(AvailablePriors()))

	for _, raw := range strings.Split(list, ",") {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}

		prior := Prior(name)
		if !isKnownPrior(prior) {
			return nil, fmt.Errorf("unknown prior %q (available: %s)", name, strings.Join(AvailablePriors(), ", "))
		}
		if seen[prior] {
			continue
		}

		seen[prior] = true
		priors = append(priors, prior)
	}

	return priors, nil
}

func isKnownPrior(prior Prior) bool {
	for _, available := range AvailablePriors() {
		if string(prior) == available {
			return true
		}
	}

	return false
}

// PriorConfig carries the priors to apply and the knobs that shape them.
type PriorConfig struct {
	// Priors is the list to apply; empty disables every prior.
	Priors []Prior
	// Strength is the largest relative adjustment; zero means the default.
	Strength float64
	// RecencyHalfLife is the age at which recency falls to one half; zero means the
	// default.
	RecencyHalfLife time.Duration
	// Now anchors recency, so a run is reproducible from its inputs.
	Now time.Time
}

// enabled reports whether any prior needs to run.
func (c PriorConfig) Enabled() bool { return len(c.Priors) > 0 }

// Resolved fills the zero values with their defaults and clamps the strength, so
// callers and explain output agree on what was applied.
func (c PriorConfig) Resolved() PriorConfig {
	if c.Strength == 0 {
		c.Strength = DefaultPriorStrength
	}
	c.Strength = math.Min(1, math.Max(0, c.Strength))

	if c.RecencyHalfLife <= 0 {
		c.RecencyHalfLife = DefaultRecencyHalfLife
	}
	if c.Now.IsZero() {
		c.Now = time.Now().UTC()
	}

	return c
}

// Values returns the normalised value of every enabled prior for one candidate.
// A value of zero means "no signal": the field is missing, or it points the other way.
func (c PriorConfig) Values(candidate db.Candidate) map[string]float64 {
	values := make(map[string]float64, len(c.Priors))
	for _, prior := range c.Priors {
		values[string(prior)] = c.value(prior, candidate)
	}

	return values
}

func (c PriorConfig) value(prior Prior, candidate db.Candidate) float64 {
	switch prior {
	case PriorRecency:
		return recencyValue(candidate.LastModifiedAt, c.Now, c.RecencyHalfLife)
	case PriorAuthority:
		return math.Min(1, math.Log1p(float64(candidate.LinkIn))/math.Log1p(authorityLinkCap))
	case PriorSeed:
		if candidate.IsSeed {
			return 1
		}

		return 0
	case PriorDepth:
		if candidate.Depth < 0 {
			return 0
		}

		return 1 / (1 + float64(candidate.Depth))
	case PriorRichness:
		rich := candidate.Attachments + candidate.Comments
		if rich <= 0 {
			return 0
		}

		return math.Min(1, float64(rich)/richnessCap)
	default:
		return 0
	}
}

// recencyValue decays from 1 for a page modified now towards 0, halving every
// half-life. A missing or unparsable timestamp carries no recency signal.
func recencyValue(lastModifiedAt string, now time.Time, halfLife time.Duration) float64 {
	modified, err := time.Parse(time.RFC3339, strings.TrimSpace(lastModifiedAt))
	if err != nil {
		return 0
	}

	age := now.Sub(modified)
	if age < 0 {
		age = 0
	}

	return 1 / (1 + age.Hours()/halfLife.Hours())
}
