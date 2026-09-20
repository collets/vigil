package boundary

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// RelayBootstrap travels over private stdin, never argv, environment, logs or
// the socket volume. The launcher must supply EOF and a bootstrap deadline.
type RelayBootstrap struct {
	Upstream    string
	Model       string
	RunToken    string
	ProviderKey string
}

func ReadRelayBootstrap(input io.Reader) (RelayBootstrap, error) {
	var config RelayBootstrap
	invalid := errors.New("invalid relay bootstrap")
	data, err := io.ReadAll(io.LimitReader(input, (16<<10)+1))
	if err != nil || len(data) > 16<<10 {
		return config, invalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return config, invalid
	}
	fields := map[string]*string{"upstream": &config.Upstream, "model": &config.Model, "run_token": &config.RunToken, "provider_key": &config.ProviderKey}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		name, ok := token.(string)
		if err != nil || !ok || fields[name] == nil || seen[name] {
			return RelayBootstrap{}, invalid
		}
		seen[name] = true
		if d.Decode(fields[name]) != nil || *fields[name] == "" {
			return RelayBootstrap{}, invalid
		}
	}
	last, err := d.Token()
	if err != nil || last != json.Delim('}') || len(seen) != len(fields) {
		return RelayBootstrap{}, invalid
	}
	if _, err := d.Token(); err != io.EOF {
		return RelayBootstrap{}, invalid
	}
	return config, nil
}
