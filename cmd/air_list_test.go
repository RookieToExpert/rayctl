package cmd

import "testing"

func TestAIRJobListRejectsInvalidLimitBeforeQuery(t *testing.T) {
	for _, value := range []string{"0", "1001", "-1"} {
		cmd := newAIRJobListCmd()
		if err := cmd.ParseFlags([]string{"-n", value}); err != nil {
			t.Fatal(err)
		}
		if err := cmd.RunE(cmd, nil); err == nil {
			t.Fatalf("accepted invalid limit %s", value)
		}
	}
	cmd := newAIRJobListCmd()
	if cmd.Flags().Lookup("limit").DefValue != "50" || cmd.Flags().Lookup("long") == nil {
		t.Fatal("missing list flags")
	}
}
