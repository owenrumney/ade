package agentbus

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var shortPR = regexp.MustCompile(`^([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)#([0-9]+)$`)

var prNumber = regexp.MustCompile(`^[0-9]+$`)

// ParseTarget reads the short form a person types: "" or "anyone", "*",
// "user@example.com", "user@example.com/instance-id", or a group name.
func ParseTarget(s string) (Target, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "" || s == "anyone":
		return Target{Anyone: true}, nil
	case s == "*":
		return Target{All: true}, nil
	case strings.Contains(s, "/"):
		return Target{Instance: s}, nil
	case strings.Contains(s, "@"):
		return Target{User: strings.ToLower(s)}, nil
	case strings.ContainsAny(s, " \t"):
		return Target{}, fmt.Errorf("target: bad value %q", s)
	}
	return Target{Group: s}, nil
}

// ParsePR accepts a GitHub PR URL or "owner/repo#123", and answers the
// canonical URL with "owner/repo".
func ParsePR(s string) (prURL, repo string, err error) {
	s = strings.TrimSpace(s)
	if m := shortPR.FindStringSubmatch(s); m != nil {
		return fmt.Sprintf("https://github.com/%s/%s/pull/%s", m[1], m[2], m[3]), m[1] + "/" + m[2], nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host != "github.com" {
		return "", "", fmt.Errorf("pr: want a github.com PR URL or owner/repo#123, got %q", s)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "pull" || !prNumber.MatchString(parts[3]) {
		return "", "", fmt.Errorf("pr: want a github.com PR URL or owner/repo#123, got %q", s)
	}
	return fmt.Sprintf("https://github.com/%s/%s/pull/%s", parts[0], parts[1], parts[3]), parts[0] + "/" + parts[1], nil
}
