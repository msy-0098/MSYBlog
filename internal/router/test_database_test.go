package router_test

import (
	"strings"
	"testing"
)

func testDatabaseDSN(t *testing.T) string {
	t.Helper()

	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	return "file:" + name + "?mode=memory&cache=shared"
}
