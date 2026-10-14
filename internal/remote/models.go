package remote

import (
	"context"
	"fmt"
)

// writeModels puts the catalogue under the worker's own state directory.
const writeModels = `set -eu
umask 077
mkdir -p "$HOME/.ade"
cat > "$HOME/.ade/models.json.new"
mv -f "$HOME/.ade/models.json.new" "$HOME/.ade/models.json"`

// PrepareModels sends this laptop's synced catalogue to the worker. Without it
// the worker offers the built-in list and refuses every model synced since.
func (h *Host) PrepareModels(ctx context.Context, catalogue []byte) error {
	if len(catalogue) == 0 {
		return nil
	}
	if _, err := h.RunInput(ctx, catalogue, "docker", "exec", "-i", workerName, "sh", "-c", writeModels); err != nil {
		return fmt.Errorf("send the model list to %s: %w", h.Name, err)
	}
	return nil
}
