package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// The read side of the gateway that the window needs beyond its status.
//
// The tray shows a summary and nothing else, so the client it started with read
// three endpoints. The window is the console, and a console that cannot list the
// models, show the log or act on an account is a readout rather than a console —
// which is what it was, and what this file fixes.

// Model is one entry of the gateway's model catalogue.
//
// The fields are the ones the gateway publishes and the ones a person choosing an
// effort needs: what it costs, how long a context it takes, and whether it can
// think. Everything the gateway sends is here rather than a subset, because a
// field dropped now is a view that has to be revisited later.
type Model struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Vendor              string   `json:"vendor"`
	Description         string   `json:"description"`
	Credits             string   `json:"credits"`
	ContextLength       int64    `json:"context_length"`
	MaxOutputTokens     int64    `json:"max_output_tokens"`
	SupportsImages      bool     `json:"supports_images"`
	SupportsReasoning   bool     `json:"supports_reasoning"`
	SupportsToolCall    bool     `json:"supports_tool_call"`
	OnlyReasoning       bool     `json:"only_reasoning"`
	CanDisableThinking  bool     `json:"can_disable_thinking"`
	DefaultEffort       string   `json:"default_effort"`
	ReasoningEffort     string   `json:"reasoning_effort"`
	SupportedEfforts    []string `json:"supported_efforts"`
	IsDefault           bool     `json:"is_default"`
	MaxAllowedInputSize int64    `json:"max_allowed_size"`
}

// LogEntry is one line of the gateway's log ring.
type LogEntry struct {
	TS   string `json:"ts"`
	Ch   string `json:"ch"`
	Text string `json:"text"`
}

// modelsBody is the models response.
//
// It carries its own decoder because the client's request path hands each response
// to a decoder rather than to a reader, which is what keeps the three endpoints it
// reads in one shape.
type modelsBody struct {
	OK     bool    `json:"ok"`
	Models []Model `json:"models"`
}

func (b *modelsBody) decode(data []byte) error { return json.Unmarshal(data, b) }

// logsListBody is the logs response.
//
// It is named apart from the status snapshot's own logsBody because the two answer
// different questions: that one reduces the log to counts for the tooltip, and this
// one keeps the lines for the window to show.
type logsListBody struct {
	Entries []LogEntry `json:"entries"`
}

func (b *logsListBody) decode(data []byte) error { return json.Unmarshal(data, b) }

// Models reads the model catalogue.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var v modelsBody
	if err := c.get(ctx, "/panel/api/models", &v); err != nil {
		return nil, err
	}
	return v.Models, nil
}

// Logs reads the log ring.
//
// The gateway keeps the newest five hundred lines and answers oldest first, which
// is the order a log is read in, so it is passed through rather than reversed. A
// limit above zero keeps the newest n; zero means all of them.
func (c *Client) Logs(ctx context.Context, limit int) ([]LogEntry, error) {
	var v logsListBody
	if err := c.get(ctx, "/panel/api/logs", &v); err != nil {
		return nil, err
	}
	if limit > 0 && len(v.Entries) > limit {
		v.Entries = v.Entries[len(v.Entries)-limit:]
	}
	return v.Entries, nil
}

// PackageBreakdown is what one account's credits are made of.
type PackageBreakdown struct {
	UID      string `json:"uid"`
	Nickname string `json:"nickname"`
	Realm    string `json:"realm"`
	Remain   int64  `json:"remain"`
	Size     int64  `json:"size"`
	// Packages is the gateway's own rendering of the breakdown, which is a table
	// rather than a list: the column widths are baked into the string and it is
	// passed through because parsing it back apart would be inventing structure the
	// gateway did not send.
	Packages string `json:"packages"`
}

// Packages reads the per-account credit breakdown.
func (c *Client) Packages(ctx context.Context) ([]PackageBreakdown, error) {
	var v packagesBody
	if err := c.get(ctx, "/panel/api/packages", &v); err != nil {
		return nil, err
	}
	return v.Accounts, nil
}

// packagesBody is the packages response.
type packagesBody struct {
	Accounts []PackageBreakdown `json:"accounts"`
}

func (b *packagesBody) decode(data []byte) error { return json.Unmarshal(data, b) }

// AccountAction runs one of the per-account maintenance endpoints.
//
// The path is built here rather than passed in, so a caller cannot ask for an
// account action that does not exist: the set is the gateway's, and it is spelled
// out in one place.
func (c *Client) AccountAction(ctx context.Context, uid, action string) (string, error) {
	switch action {
	case "checkin", "balance", "revive", "disable", "remove":
	default:
		return "", fmt.Errorf("panel: unknown account action %q", action)
	}
	return c.post(ctx, "/panel/api/accounts/"+uid+"/"+action)
}

// post issues a POST with no body, which is every one of the gateway's
// maintenance endpoints, and returns what it said.
func (c *Client) post(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, nil)
	if err != nil {
		return "", err
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode == http.StatusUnauthorized {
		return "", ErrUnauthorized
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return summarise(body), nil
}

// summarise turns a gateway response into one line for the window's notice area.
//
// The gateway answers with an object of counts, and the console shows a sentence.
// The object is preferred when it carries one, because the gateway's own wording is
// the one that matches what it actually did.
func summarise(body []byte) string {
	var v struct {
		OK      *bool  `json:"ok"`
		Message string `json:"message"`
		Summary string `json:"summary"`
		Error   string `json:"error"`
		Done    *int   `json:"done"`
		Failed  *int   `json:"failed"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return strings.TrimSpace(string(body))
	}
	switch {
	case v.Error != "":
		return v.Error
	case v.Summary != "":
		return v.Summary
	case v.Message != "":
		return v.Message
	case v.Done != nil || v.Failed != nil:
		done, failed := 0, 0
		if v.Done != nil {
			done = *v.Done
		}
		if v.Failed != nil {
			failed = *v.Failed
		}
		return "完成 " + strconv.Itoa(done) + " 个，失败 " + strconv.Itoa(failed) + " 个"
	}
	return strings.TrimSpace(string(body))
}
