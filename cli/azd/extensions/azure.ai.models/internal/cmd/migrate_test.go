// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import "testing"

func TestMigrateCommandLaunchFlags(t *testing.T) {
	cmd := newMigrateCommand()
	for _, name := range []string{"subscription", "port"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected --%s flag", name)
		}
	}
}
