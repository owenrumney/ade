package transcripts

import (
	"encoding/json"
	"sort"
)

// ContextPart is one contributor to a session's context window: what it is,
// and roughly how many of the window's tokens it is holding.
type ContextPart struct {
	Label  string `json:"label"`
	Tokens int    `json:"tokens"`
}

// contextLine is what a breakdown needs from a transcript line beyond what
// claudeLine already reads: the identity of the line, so a compaction can say
// which lines survived it, and the two markers of a compaction itself.
type contextLine struct {
	UUID             string `json:"uuid"`
	Subtype          string `json:"subtype"`
	IsCompactSummary bool   `json:"isCompactSummary"`
	Meta             struct {
		Preserved struct {
			HeadUUID string `json:"headUuid"`
		} `json:"preservedSegment"`
	} `json:"compactMetadata"`
}

// contextBlock is one content block, across the shapes the roles use: text
// and thinking from the assistant, a tool call and the result that answers it.
type contextBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	ID       string          `json:"id"`
	UseID    string          `json:"tool_use_id"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"`
}

// share is one line's contribution, held until a compaction can rule on
// whether the line survived.
type share struct {
	uuid  string
	label string
	chars int
}

// charsPerToken is the rule of thumb this estimate rests on. English prose
// runs a little over four characters a token and code a little under, so a
// transcript of both lands near enough for a proportion.
const charsPerToken = 4

// ContextBreakdown estimates what is filling a Claude Code session's context
// window, largest contributor first. Tool output is grouped by the tool that
// produced it, which is where a long session's window has usually gone.
//
// It is an estimate off the transcript, not a measurement: the agent reports
// one total per request and never says what made it up. Characters are counted
// and divided, so the parts are the right shape with the wrong numbers. Only
// what the model can still see is counted — a subagent's conversation is its
// own, and a compaction drops everything it replaced with a summary.
func ContextBreakdown(path string) []ContextPart {
	var shares []share
	tools := map[string]string{}

	err := scanJSONL(path, func(raw []byte) error {
		var line claudeLine
		if json.Unmarshal(raw, &line) != nil {
			return nil
		}
		var extra contextLine
		if json.Unmarshal(raw, &extra) != nil {
			return nil
		}
		// A subagent's conversation is its own. Only its answer comes back,
		// and that arrives as a tool result on this one.
		if line.IsSidechain {
			return nil
		}
		if line.Type == "system" && extra.Subtype == "compact_boundary" {
			shares = survivors(shares, extra.Meta.Preserved.HeadUUID)
			return nil
		}
		if line.Message == nil {
			return nil
		}
		add := func(label string, chars int) {
			if label != "" && chars > 0 {
				shares = append(shares, share{uuid: extra.UUID, label: label, chars: chars})
			}
		}
		switch line.Type {
		case "user":
			if line.IsMeta {
				return nil
			}
			if extra.IsCompactSummary {
				add("compacted summary", contentChars(line.Message.Content))
				return nil
			}
			for _, block := range contextBlocks(line.Message.Content) {
				switch block.Type {
				case "tool_result":
					add(toolLabel(tools[block.UseID]), contentChars(block.Content))
				default:
					add("your messages", len(block.Text))
				}
			}
			// A plain string message is the common shape of something typed.
			if line.Message.Content != nil && line.Message.Content[0] == '"' {
				add("your messages", contentChars(line.Message.Content))
			}
		case "assistant":
			for _, block := range contextBlocks(line.Message.Content) {
				switch block.Type {
				case "tool_use":
					tools[block.ID] = block.Name
					add(toolLabel(block.Name), len(block.Input))
				case "thinking":
					add("thinking", len(block.Thinking))
				case "text":
					add("replies", len(block.Text))
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return rank(shares)
}

// survivors is the shares a compaction left in the window: those from the
// preserved segment onwards. A boundary that names no segment kept nothing —
// the summary that follows is the whole of what the model still sees.
func survivors(shares []share, head string) []share {
	if head == "" {
		return nil
	}
	for i, s := range shares {
		if s.uuid == head {
			return shares[i:]
		}
	}
	return nil
}

// rank totals the shares by label and orders them, biggest first, ties broken
// by name so a hover does not reshuffle between renders.
func rank(shares []share) []ContextPart {
	chars := map[string]int{}
	for _, s := range shares {
		chars[s.label] += s.chars
	}
	parts := make([]ContextPart, 0, len(chars))
	for label, n := range chars {
		if tokens := n / charsPerToken; tokens > 0 {
			parts = append(parts, ContextPart{Label: label, Tokens: tokens})
		}
	}
	sort.Slice(parts, func(i, j int) bool {
		if parts[i].Tokens != parts[j].Tokens {
			return parts[i].Tokens > parts[j].Tokens
		}
		return parts[i].Label < parts[j].Label
	})
	return parts
}

// toolLabel names a tool's bucket. A result whose call scrolled out of the
// transcript has no name to group under, so it goes in with the rest.
func toolLabel(name string) string {
	if name == "" {
		return "tool output"
	}
	return name
}

func contextBlocks(content json.RawMessage) []contextBlock {
	if len(content) == 0 || content[0] != '[' {
		return nil
	}
	var blocks []contextBlock
	if json.Unmarshal(content, &blocks) != nil {
		return nil
	}
	return blocks
}

// contentChars is how much text a content field holds, whether it was written
// as a string or as blocks.
func contentChars(content json.RawMessage) int {
	if len(content) == 0 {
		return 0
	}
	var text string
	if json.Unmarshal(content, &text) == nil {
		return len(text)
	}
	total := 0
	for _, block := range contextBlocks(content) {
		total += len(block.Text) + len(block.Thinking)
	}
	return total
}
