package panel

import (
	"context"
	"io"
	"net/http"
)

// ConfigJSON reads the gateway's configuration endpoint and returns its body.
//
// It returns the raw JSON rather than a parsed struct because the window shows the
// settings an operator looks for and parses them itself, and a second struct here
// would be a second place for the gateway's field names to be spelled.
func (c *Client) ConfigJSON(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/panel/api/config", nil)
	if err != nil {
		return nil, err
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<10))
}
