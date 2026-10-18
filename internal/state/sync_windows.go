// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package state

// syncDir does nothing on Windows, where directories can't be flushed and
// the rename itself is what replaces the file.
func syncDir(string) error { return nil }
