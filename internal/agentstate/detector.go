package agentstate

import (
	"regexp"
	"strings"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const maxSnapshotLines = 100

var ansiEscapePattern = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\a]*(?:\a|\x1b\\))`)

type Snapshot struct {
	Lines []string
	Title string
}

type RuleEvidence struct {
	RuleID  string `json:"rule_id"`
	Matched bool   `json:"matched"`
}

type Decision struct {
	Activity        registry.Activity `json:"activity"`
	Reason          string            `json:"reason"`
	RuleID          string            `json:"rule_id,omitempty"`
	ManifestSource  string            `json:"manifest_source"`
	ManifestVersion int               `json:"manifest_version"`
	Warning         string            `json:"warning,omitempty"`
	Evidence        []RuleEvidence    `json:"evidence"`
}

func NormalizeSnapshot(text string, title string) Snapshot {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = ansiEscapePattern.ReplaceAllString(text, "")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > maxSnapshotLines {
		lines = lines[len(lines)-maxSnapshotLines:]
	}
	return Snapshot{Lines: lines, Title: ansiEscapePattern.ReplaceAllString(title, "")}
}

func Evaluate(manifest Manifest, snapshot Snapshot) Decision {
	decision := Decision{Activity: registry.ActivityUnknown, Reason: "no_rule_matched", RuleID: "", ManifestSource: manifest.Source, ManifestVersion: manifest.Version, Warning: manifest.Warning, Evidence: make([]RuleEvidence, 0, len(manifest.Rules))}
	for _, rule := range sortedRules(manifest.Rules) {
		matched := matchesRule(rule, snapshot)
		decision.Evidence = append(decision.Evidence, RuleEvidence{RuleID: rule.ID, Matched: matched})
		if !matched {
			continue
		}
		activity, _ := registry.NormalizeActivity(rule.State)
		decision.Activity = activity
		decision.Reason = "manifest_rule"
		decision.RuleID = rule.ID
		return decision
	}
	return decision
}

func matchesRule(rule Rule, snapshot Snapshot) bool {
	region, err := selectRegion(rule.Region, snapshot.Lines)
	if err != nil {
		return false
	}
	text := strings.Join(region, "\n")
	title := snapshot.Title
	if !rule.CaseSensitive {
		text = strings.ToLower(text)
		title = strings.ToLower(title)
	}
	return matchesText(rule, text) && matchesTitle(rule, title)
}

func matchesText(rule Rule, text string) bool {
	for _, literal := range rule.All {
		if !strings.Contains(text, normalizedLiteral(literal, rule.CaseSensitive)) {
			return false
		}
	}
	if len(rule.Any) > 0 && !containsAny(text, rule.Any, rule.CaseSensitive) {
		return false
	}
	if containsAny(text, rule.None, rule.CaseSensitive) || !matchesAllRegex(text, rule.RegexAll, rule.CaseSensitive) {
		return false
	}
	if len(rule.RegexAny) > 0 && !matchesAnyRegex(text, rule.RegexAny, rule.CaseSensitive) {
		return false
	}
	return !matchesAnyRegex(text, rule.RegexNone, rule.CaseSensitive)
}

func matchesTitle(rule Rule, title string) bool {
	if len(rule.TitleAny) > 0 && !containsAny(title, rule.TitleAny, rule.CaseSensitive) {
		return false
	}
	return len(rule.TitleRegexAny) == 0 || matchesAnyRegex(title, rule.TitleRegexAny, rule.CaseSensitive)
}

func normalizedLiteral(value string, caseSensitive bool) string {
	if caseSensitive {
		return value
	}
	return strings.ToLower(value)
}

func containsAny(text string, values []string, caseSensitive bool) bool {
	for _, value := range values {
		if strings.Contains(text, normalizedLiteral(value, caseSensitive)) {
			return true
		}
	}
	return false
}

func matchesAllRegex(text string, expressions []string, caseSensitive bool) bool {
	for _, expression := range expressions {
		if !compileRuleRegex(expression, caseSensitive).MatchString(text) {
			return false
		}
	}
	return true
}

func ruleRegexExpression(expression string, caseSensitive bool) string {
	if caseSensitive {
		return expression
	}
	return "(?i:" + expression + ")"
}

func compileRuleRegex(expression string, caseSensitive bool) *regexp.Regexp {
	return regexp.MustCompile(ruleRegexExpression(expression, caseSensitive))
}

func matchesAnyRegex(text string, expressions []string, caseSensitive bool) bool {
	for _, expression := range expressions {
		if compileRuleRegex(expression, caseSensitive).MatchString(text) {
			return true
		}
	}
	return false
}
