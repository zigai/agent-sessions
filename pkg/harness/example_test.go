package harness_test

import (
	"fmt"

	"github.com/zigai/aht/pkg/harness"
)

func ExampleParse() {
	harnessID, err := harness.Parse("OpenCode")
	if err != nil {
		panic(err)
	}

	fmt.Println(harnessID)
	// Output: opencode
}
