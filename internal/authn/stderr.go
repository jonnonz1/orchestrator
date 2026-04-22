package authn

import (
	"io"
	"os"
)

// authStderr is swappable by tests.
var authStderr io.Writer = os.Stderr
