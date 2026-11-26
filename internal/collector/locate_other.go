//go:build !windows

package collector

import (
 "errors"
 "github.com/iPapik/Storavix/internal/telemetry"
)
func LocateDrive(c telemetry.LocateCommand) error {
 return errors.New("Locate пока доступен только на Windows")
}
