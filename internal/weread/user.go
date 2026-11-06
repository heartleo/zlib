package weread

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// nameFields are the response fields that may carry the nickname, in order
// of preference. "user.name" is a nested object field.
var nameFields = []string{"name", "nickName", "nick", "user.name"}

// UserInfo is the signed-in account's public profile.
type UserInfo struct {
	Name string
	// Keys lists the response's field names (one level of nesting as
	// "parent.child"), never values, so a reply can be inspected safely.
	Keys []string
}

// UserInfo fetches the signed-in account's profile from GET /user.
func (c *Client) UserInfo(ctx context.Context) (UserInfo, error) {
	vid := c.Credentials().VID
	if vid == "" {
		return UserInfo{}, ErrNotLoggedIn
	}
	var raw map[string]json.RawMessage
	if err := c.call(ctx, http.MethodGet, "/user", url.Values{"userVid": {vid}}, nil, &raw); err != nil {
		return UserInfo{}, err
	}
	fields := flattenFields(raw)
	info := UserInfo{}
	for name := range fields {
		info.Keys = append(info.Keys, name)
	}
	sort.Strings(info.Keys)
	for _, field := range nameFields {
		var s string
		if v, ok := fields[field]; ok && json.Unmarshal(v, &s) == nil {
			if s = cleanText(strings.ReplaceAll(s, "\n", " ")); s != "" {
				info.Name = s
				break
			}
		}
	}
	return info, nil
}

// UpdateName fetches the nickname and keeps it in the client's credentials,
// saving them when it changed. It returns the current nickname.
func (c *Client) UpdateName(ctx context.Context) (string, error) {
	info, err := c.UserInfo(ctx)
	if err != nil {
		return "", err
	}
	if info.Name == "" {
		return c.Credentials().Name, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.creds.Name == info.Name {
		return info.Name, nil
	}
	c.creds.Name = info.Name
	if c.save != nil {
		if err := c.save(c.creds); err != nil {
			return info.Name, err
		}
	}
	return info.Name, nil
}

// flattenFields maps top-level fields, and the fields of top-level objects
// as "parent.child", to their raw values.
func flattenFields(raw map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(raw))
	for name, value := range raw {
		name = cleanText(name)
		out[name] = value
		var nested map[string]json.RawMessage
		if json.Unmarshal(value, &nested) == nil {
			for child, v := range nested {
				out[name+"."+cleanText(child)] = v
			}
		}
	}
	return out
}
