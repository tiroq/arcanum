package providers

// ModelRole represents a logical role that a caller requests when making an LLM call.
// Processors request roles, not raw model names. The provider layer resolves roles
// to concrete model names and timeouts based on configuration.
type ModelRole string

const (
	// RoleDefault is the balanced general-purpose model role.
	RoleDefault ModelRole = "default"
	// RoleFast is for low-latency lightweight tasks (classification, tagging, quick rewrite).
	RoleFast ModelRole = "fast"
	// RolePlanner is for heavier reasoning and decomposition tasks.
	RolePlanner ModelRole = "planner"
	// RoleReview is for critique, evaluation, and validation tasks.
	RoleReview ModelRole = "review"
)

// ValidModelRoles lists all valid model roles.
var ValidModelRoles = []ModelRole{RoleDefault, RoleFast, RolePlanner, RoleReview}

// IsValid returns true if the role is a recognized model role.
func (r ModelRole) IsValid() bool {
	switch r {
	case RoleDefault, RoleFast, RolePlanner, RoleReview:
		return true
	}
	return false
}

// String returns the string representation of the model role.
func (r ModelRole) String() string {
	return string(r)
}
