package launchd

import (
	"fmt"
	"os"
)

// Domain returns the rootless per-user GUI launchd domain (gui/$UID) (D25).
func Domain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

// Target is the full service target gui/$UID/<label>.
func Target(label string) string {
	return Domain() + "/" + label
}
