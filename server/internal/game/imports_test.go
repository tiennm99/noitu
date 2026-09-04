package game_test

import (
	"os/exec"
	"strings"
	"testing"
)

// The engine must stay transport-free: it speaks its own Go types and knows
// nothing about WebSockets or protobuf. That is a property worth enforcing
// rather than remembering, since the temptation to reach for a wire type from
// inside the rules is exactly how the two get welded together.
func TestEngineHasNoTransportDependencies(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "github.com/tiennm99dev/noitu/server/internal/game").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}

	banned := []string{
		"google.golang.org/protobuf",
		"github.com/coder/websocket",
		"net/http",
		"github.com/tiennm99dev/noitu/server/internal/wsapi",
		"github.com/tiennm99dev/noitu/server/gen",
		// The rules layer must not reach for storage either: it takes a
		// Dictionary interface so it can be tested without one.
		"modernc.org/sqlite",
		"database/sql",
	}

	for _, dep := range strings.Fields(string(out)) {
		for _, bad := range banned {
			if dep == bad || strings.HasPrefix(dep, bad+"/") {
				t.Errorf("internal/game depends on %q, which it must not", dep)
			}
		}
	}
}
