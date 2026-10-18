// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package state

import "os"

// syncDir flushes a directory so a rename in it survives a power cut.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
