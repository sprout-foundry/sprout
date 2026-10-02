package daemon

import (
	"errors"
	"net"
	"os"
)

func isClosedErr(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrClosed) ||
		(err != nil && err.Error() == "use of closed network connection")
}
