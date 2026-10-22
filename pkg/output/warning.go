package output

import (
	"fmt"
	"io"
)

func PrintWarning(writer io.Writer, message string) {
	if jsonSession != nil {
		jsonSession.Warnings = append(jsonSession.Warnings, message)
		return
	}
	fmt.Fprintln(writer, "Warning:", message)
}
