package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrMeterBusy is returned when another before-yes process holds the
// meter's file.
var ErrMeterBusy = errors.New("another before-yes process (serve or check) is using this data_dir's spend.db; stop it, or run the check through the server")

// lockMeter takes an exclusive, non-blocking OS lock on the meter's file
// for this process's lifetime. Keel's cost/sqlitestore opens deferred
// transactions, so two processes on one spend.db can lose a booking to
// SQLITE_BUSY (nrynss/keel#100, BEGIN IMMEDIATE, is the upstream fix).
// Until then one process owns the meter: `check` refuses while `serve`
// runs, and a second `serve` refuses too.
func lockMeter(path string) (*os.File, error) {
	f, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("meter lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrMeterBusy
		}
		return nil, fmt.Errorf("meter lock: %w", err)
	}
	return f, nil
}
