package gate

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// BadgeURLVar is where `ade badge` posts what an agent CLI's own hook said.
const BadgeURLVar = "ADE_BADGE_URL"

const badgeWait = 3 * time.Second

// Badge passes one hook report on: codex hands it as the last argument, claude
// on stdin. Outside an ade session there is nobody to tell, which is no error.
func Badge(ctx context.Context, in io.Reader, args []string) error {
	url, token := os.Getenv(BadgeURLVar), os.Getenv(TokenVar)
	if url == "" {
		return nil
	}
	var body []byte
	if len(args) > 0 {
		body = []byte(args[len(args)-1])
	} else {
		var err error
		if body, err = io.ReadAll(io.LimitReader(in, 1<<20)); err != nil {
			return fmt.Errorf("read the report: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, badgeWait)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("reach ade: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("ade answered %d", response.StatusCode)
	}
	return nil
}
