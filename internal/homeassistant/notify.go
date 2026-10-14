package homeassistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// notifyPrefix is the domain HA puts the mobile apps under. A target is one
// service inside it, named after the device that registered.
const notifyPrefix = "notify."

// serviceName is what a HA service id may contain. The name goes into a URL
// path, so a "/" or ".." in it would call some other service with the token.
var serviceName = regexp.MustCompile(`^[a-z0-9_]+$`)

// Targets lists what this Home Assistant can notify, so the settings page can
// offer them rather than asking you to spell a service name.
func (c Client) Targets(ctx context.Context) ([]string, error) {
	base, err := ValidURL(c.URL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(c.Token) == "" {
		return nil, fmt.Errorf("Home Assistant needs a long-lived access token")
	}
	body, err := c.get(ctx, base+"/api/services")
	if err != nil {
		return nil, err
	}
	var domains []struct {
		Domain   string         `json:"domain"`
		Services map[string]any `json:"services"`
	}
	if err := json.Unmarshal(body, &domains); err != nil {
		return nil, fmt.Errorf("read Home Assistant services: %w", err)
	}
	var out []string
	for _, domain := range domains {
		if domain.Domain != "notify" {
			continue
		}
		for name := range domain.Services {
			out = append(out, notifyPrefix+name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Notify sends one notification. target is a service as Targets lists it,
// "notify.mobile_app_something".
func (c Client) Notify(ctx context.Context, target, title, message string) error {
	base, err := ValidURL(c.URL)
	if err != nil {
		return err
	}
	service, ok := strings.CutPrefix(strings.TrimSpace(target), notifyPrefix)
	if !ok || !serviceName.MatchString(service) {
		return fmt.Errorf("%q is not a notify service", target)
	}
	if strings.TrimSpace(c.Token) == "" {
		return fmt.Errorf("Home Assistant needs a long-lived access token")
	}
	body, err := json.Marshal(map[string]string{"title": title, "message": message})
	if err != nil {
		return err
	}
	return c.post(ctx, base+"/api/services/notify/"+service, body)
}

func (c Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create Home Assistant request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	res, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("ask Home Assistant: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("ask Home Assistant: %s", res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 1<<20))
}

func (c Client) post(ctx context.Context, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Home Assistant request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("call Home Assistant: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	message, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	return fmt.Errorf("call Home Assistant: %s: %s", res.Status, strings.TrimSpace(string(message)))
}
