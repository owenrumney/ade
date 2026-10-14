// Package homeassistant sends the small completion signal ade owns.
package homeassistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	URL   string
	Token string
	HTTP  *http.Client
}

type lightState struct {
	State      string `json:"state"`
	Attributes struct {
		Brightness *int      `json:"brightness"`
		ColorMode  string    `json:"color_mode"`
		ColorTemp  *int      `json:"color_temp"`
		HSColor    []float64 `json:"hs_color"`
		RGBColor   []int     `json:"rgb_color"`
	} `json:"attributes"`
}

// flashHold is how long the light stays red. Long enough to catch across a
// room, short enough not to read as a light someone left on. A var for tests.
var flashHold = 2 * time.Second

// Hold is what a caller has to leave room for: Flash waits this long between
// the two calls, and a context that expires first leaves the light red.
func Hold() time.Duration { return flashHold }

// Hues the desk flashes in. A lamp is a lamp, so what it says has to fit in
// two seconds: the colour is the whole message.
const (
	HueDone   = 120.0 // green: the turn ended and asked nothing
	HueAsking = 0.0   // red: it wants you
)

// Flash holds the light at hue for a moment, then puts it back as it was.
func (c Client) Flash(ctx context.Context, entity string, hue float64) error {
	base, err := ValidURL(c.URL)
	if err != nil {
		return err
	}
	entity = strings.TrimSpace(entity)
	if !strings.HasPrefix(entity, "light.") {
		return fmt.Errorf("%q is not a light entity", entity)
	}
	if strings.TrimSpace(c.Token) == "" {
		return fmt.Errorf("Home Assistant needs a long-lived access token")
	}
	before, err := c.state(ctx, base, entity)
	if err != nil {
		return err
	}
	restoreValues, err := before.restoreValues()
	if err != nil {
		return err
	}
	if err := c.turnOn(ctx, base, entity, map[string]any{"hs_color": []float64{hue, 100}}); err != nil {
		return err
	}
	select {
	case <-time.After(flashHold):
	case <-ctx.Done():
	}
	restore, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.turnOn(restore, base, entity, restoreValues)
}

func (s lightState) restoreValues() (map[string]any, error) {
	if s.State != "on" {
		return nil, fmt.Errorf("Home Assistant light must be on before it can flash")
	}
	values := map[string]any{}
	if s.Attributes.Brightness != nil {
		values["brightness"] = *s.Attributes.Brightness
	}
	switch s.Attributes.ColorMode {
	case "color_temp":
		if s.Attributes.ColorTemp != nil {
			values["color_temp"] = *s.Attributes.ColorTemp
		}
	case "hs":
		if len(s.Attributes.HSColor) == 2 {
			values["hs_color"] = s.Attributes.HSColor
		}
	case "rgb":
		if len(s.Attributes.RGBColor) == 3 {
			values["rgb_color"] = s.Attributes.RGBColor
		}
	}
	if len(values) == 0 || len(values) == 1 && s.Attributes.Brightness != nil {
		return nil, fmt.Errorf("Home Assistant light has no restorable colour")
	}
	return values, nil
}

func (c Client) state(ctx context.Context, base, entity string) (lightState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/states/"+url.PathEscape(entity), nil)
	if err != nil {
		return lightState{}, fmt.Errorf("create Home Assistant state request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	res, err := c.client().Do(req)
	if err != nil {
		return lightState{}, fmt.Errorf("read Home Assistant light: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return lightState{}, fmt.Errorf("read Home Assistant light: %s: %s", res.Status, strings.TrimSpace(string(message)))
	}
	var state lightState
	if err := json.NewDecoder(res.Body).Decode(&state); err != nil {
		return lightState{}, fmt.Errorf("decode Home Assistant light: %w", err)
	}
	return state, nil
}

func (c Client) turnOn(ctx context.Context, base, entity string, values map[string]any) error {
	values["entity_id"] = entity
	values["brightness"] = 255
	body, err := json.Marshal(values)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/services/light/turn_on", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Home Assistant request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("call Home Assistant light: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	message, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	return fmt.Errorf("call Home Assistant light: %s: %s", res.Status, strings.TrimSpace(string(message)))
}

func (c Client) client() *http.Client {
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if c.HTTP != nil {
		client = *c.HTTP
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return &client
}

func ValidURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("Home Assistant needs a complete URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("Home Assistant URL must use HTTP or HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("Home Assistant URL cannot contain credentials, a query, or a fragment")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}
