package main

import "testing"

func TestPerchScript(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"npm shim", []string{"node", "/Users/x/.nvm/versions/node/v22.20.0/bin/perch"}, true},
		{"node with flag", []string{"/Users/x/.nvm/versions/node/v22.20.0/bin/node", "--max-old-space-size=4096", "/Users/x/.nvm/versions/node/v22.20.0/bin/perch"}, true},
		{"perchai-cli dist", []string{"node", "/x/lib/node_modules/perchai-cli/dist/perch.mjs"}, true},
		{"plain node", []string{"node", "server.js"}, false},
		{"node mentioning perch", []string{"node", "/app/perch-server.js"}, false},
		{"empty", nil, false},
		{"claude", []string{"claude", "--resume", "abc"}, false},
	}
	for _, c := range cases {
		got := perchScript(c.args) != ""
		if got != c.want {
			t.Errorf("%s: perchScript(%v) = %v, want %v", c.name, c.args, got, c.want)
		}
	}
}

func TestPerchKind(t *testing.T) {
	if k := agentNamed("perch"); k != agentPerch {
		t.Errorf("agentNamed(perch) = %q", k)
	}
	if agentPerch.name() != "Perch" {
		t.Errorf("name = %q", agentPerch.name())
	}
	launch := agentPerch.launch()
	if len(launch) == 0 || launch[2] != "perch" {
		t.Errorf("launch = %v", launch)
	}
}
