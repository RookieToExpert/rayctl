package platform

import "fmt"

// IAMUserNotFoundError distinguishes an absent tenant user from a failed
// group/policy request whose error may also contain the text "not found".
type IAMUserNotFoundError struct{ Identifier string }

func (e *IAMUserNotFoundError) Error() string {
	return fmt.Sprintf("user %q not found in current tenant", e.Identifier)
}
