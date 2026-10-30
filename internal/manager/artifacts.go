package manager

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cbspire/raymond/internal/control"
	"github.com/cbspire/raymond/internal/install"
)

// Complete records can outlive the worker if it died between Finish and cleanup.
// Cleanup is permitted only after the exact journal result has become terminal.
func cleanupCompleted(j *control.Operations) error {
	if j.Active() != nil {
		return nil
	}
	transaction, e := install.LoadTransaction()
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	operation, e := j.Get(transaction.Operation)
	if e != nil {
		return e
	}
	if operation.Status != "succeeded" && operation.Status != "failed" {
		return fmt.Errorf("rollback evidence has no terminal operation")
	}
	if transaction.NewInstall && transaction.Phase == "rolled-back" {
		return nil
	}
	if transaction.Phase != "committed" && transaction.Phase != "rolled-back" {
		return fmt.Errorf("unfinished rollback evidence cannot be discarded")
	}
	if e = install.ClearTransaction(); e != nil {
		return e
	}
	return clearStaging()
}
func clearStaging() error {
	for _, name := range []string{"staging", "enrollment"} {
		path := filepath.Join(control.RootDir, name)
		if e := install.Trusted(path); e != nil {
			return e
		}
		if e := os.RemoveAll(path); e != nil {
			return e
		}
	}
	return nil
}
