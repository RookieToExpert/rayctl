package output

import (
	"fmt"
	"strings"
	"testing"
)

func TestIdentifiersRemainCopyableInNarrowAndWideTerminals(t *testing.T) {
	name := "infer-glm-53-flash-" + strings.Repeat("long-model-name-", 12) + "unique-ending"
	image := "registry.example.test/team/" + strings.Repeat("image-name-", 20) + ":version"
	for _, columns := range []int{80, 200} {
		t.Run(fmt.Sprint(columns), func(t *testing.T) {
			t.Setenv("COLUMNS", fmt.Sprint(columns))
			for _, render := range []func(){
				func() {
					printBoxTableWithMaxWidths([]string{"NAME", "DESCRIPTION"}, [][]string{{name, "some long description"}}, []int{20, 20})
				},
				func() {
					printBoxTableWithMaxWidths([]string{"FIELD", "VALUE"}, [][]string{{"IMAGE", image}, {"PVC", name}}, []int{20, 20})
				},
			} {
				text := captureTableOutput(t, render)
				if !strings.Contains(text, name) {
					t.Fatalf("identifier was truncated or wrapped: %s", text)
				}
				if strings.Contains(text, "IMAGE") && !strings.Contains(text, image) {
					t.Fatalf("image was truncated: %s", text)
				}
			}
		})
	}
}
