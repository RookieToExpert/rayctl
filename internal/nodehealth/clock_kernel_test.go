package nodehealth

import "testing"

func TestParseClockOutput(t *testing.T) {
	btime, uptime, now, err := parseClockOutput("btime 100\n20.5 0.0\n121\n")
	if err != nil || btime != 100 || uptime != 20.5 || now != 121 {
		t.Fatalf("parseClockOutput() = %d, %v, %d, %v", btime, uptime, now, err)
	}
}

func TestClassifyKernelLinesPanicFailsUpstream(t *testing.T) {
	lines := []kernelLine{
		{Monotonic: 990, Text: "kernel BUG at demo.c:10"},
		{Monotonic: 991, Text: "Call Trace detail 1"},
		{Monotonic: 992, Text: "trace detail 2"},
		{Monotonic: 993, Text: "trace detail 3"},
	}
	result := classifyKernelLines(lines, 1000, 2000)
	if len(result) == 0 || result[0].Category != "panic" || len(result[0].Lines) < 4 {
		t.Fatalf("classifyKernelLines() = %#v", result)
	}
}
