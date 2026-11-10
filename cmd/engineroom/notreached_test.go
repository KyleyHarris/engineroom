package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/source"
)

// What the menu bar app is told about an engine not reached (connection obligation O10): engineDown,
// which makes it offer retry, work locally or offline, only for a cause a retry may clear; a changed
// host key is said as one and gets no retry; any other error is not the engine down. Fails if the
// app would offer a retry for a changed host key or a missing ssh, or stop offering one for an
// engine that did not answer.
func TestTheAppIsToldWhetherARetryCanHelp(t *testing.T) {
	cases := []struct {
		name          string
		err           error
		down, keyFlag bool
	}{
		{"no answer", &source.Unreached{Cause: source.NoAnswer, Host: "engine.example", Said: "Operation timed out"}, true, false},
		{"a name that does not resolve", &source.Unreached{Cause: source.NoName, Host: "engine.example"}, true, false},
		{"a changed host key", &source.Unreached{Cause: source.HostKeyChanged, Host: "engine.example"}, false, true},
		{"an unknown host key", &source.Unreached{Cause: source.HostKeyUnknown, Host: "engine.example"}, false, false},
		{"no ssh", &source.Unreached{Cause: source.NoSSH, Host: "engine.example"}, false, false},
		{"a refusal", errors.New("project-a is not registered"), false, false},
	}
	for _, c := range cases {
		var out bytes.Buffer
		if code := apiError(&out, c.err); code != 3 {
			t.Fatalf("%s: exit %d", c.name, code)
		}
		var reply struct {
			Error          string `json:"error"`
			EngineDown     bool   `json:"engineDown"`
			HostKeyChanged bool   `json:"hostKeyChanged"`
		}
		if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
			t.Fatalf("%s: %v: %s", c.name, err, out.String())
		}
		if reply.EngineDown != c.down || reply.HostKeyChanged != c.keyFlag || reply.Error != c.err.Error() {
			t.Errorf("%s: %s", c.name, out.String())
		}
	}
}
