package boundary

import (
	"strings"
	"testing"
)

func TestRelayBootstrap(t *testing.T) {
	valid := `{"upstream":"http://provider/v1","model":"selected","run_token":"run","provider_key":"private-fixture"}`
	config, err := ReadRelayBootstrap(strings.NewReader(valid))
	if err != nil || config.ProviderKey != "private-fixture" || config.Model != "selected" {
		t.Fatal("valid bootstrap rejected")
	}
	for _, input := range []string{
		valid + ` {}`, `null`, `[]`, `{}`, strings.Replace(valid, `"model":"selected"`, `"model":"selected","model":"other"`, 1),
		strings.Replace(valid, `"model":`, `"unknown":`, 1), strings.Replace(valid, `"selected"`, `null`, 1),
		strings.Replace(valid, `"selected"`, `[]`, 1), strings.Replace(valid, `"selected"`, `""`, 1), strings.Repeat("private-fixture", 2000),
	} {
		_, err := ReadRelayBootstrap(strings.NewReader(input))
		if err == nil || strings.Contains(err.Error(), "private-fixture") {
			t.Fatal("unsafe bootstrap accepted or disclosed")
		}
	}
}
