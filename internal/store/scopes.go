package store

import "slices"

// Permission is an independently grantable API operation.
type Permission string

const (
	ProjectsRead       Permission = "projects:read"
	ProjectsWrite      Permission = "projects:write"
	ProjectsDelete     Permission = "projects:delete"
	EnvironmentsRead   Permission = "environments:read"
	EnvironmentsWrite  Permission = "environments:write"
	EnvironmentsDelete Permission = "environments:delete"
	ConfigRead         Permission = "config:read"
	ConfigReadSecrets  Permission = "config:read-secrets"
	ConfigWrite        Permission = "config:write"
	ConfigDelete       Permission = "config:delete"
	ProposalsRead      Permission = "proposals:read"
	ProposalsWrite     Permission = "proposals:write"
	ProposalsReview    Permission = "proposals:review"
	KeysRead           Permission = "keys:read"
	KeysWrite          Permission = "keys:write"
	UsersRead          Permission = "users:read"
	UsersWrite         Permission = "users:write"
	UsersDelete        Permission = "users:delete"
	SettingsRead       Permission = "settings:read"
	SettingsWrite      Permission = "settings:write"
)

// KeyScope binds operations to one project; ProjectID zero represents all projects.
type KeyScope struct {
	ProjectID   uint         `json:"project_id"`
	Permissions []Permission `json:"permissions"`
}

// KeyPermissions returns the supported operations in display order.
func KeyPermissions() []Permission {
	return []Permission{
		ProjectsRead, ProjectsWrite, ProjectsDelete,
		EnvironmentsRead, EnvironmentsWrite, EnvironmentsDelete,
		ConfigRead, ConfigReadSecrets, ConfigWrite, ConfigDelete,
		ProposalsRead, ProposalsWrite, ProposalsReview, KeysRead, KeysWrite,
		UsersRead, UsersWrite, UsersDelete, SettingsRead, SettingsWrite,
	}
}

// Valid reports whether the permission is supported.
func (p Permission) Valid() bool {
	return slices.Contains(KeyPermissions(), p)
}

// GlobalOnly reports whether the operation requires a wildcard scope.
func (p Permission) GlobalOnly() bool {
	return p == UsersRead || p == UsersWrite || p == UsersDelete || p == SettingsRead || p == SettingsWrite
}

// Allows reports whether the key explicitly grants an operation in a project.
func (k APIKey) Allows(projectID uint, permission Permission) bool {
	for _, scope := range k.Scopes {
		if (scope.ProjectID == 0 || (!permission.GlobalOnly() && scope.ProjectID == projectID)) && slices.Contains(scope.Permissions, permission) {
			return true
		}
	}
	return false
}

// HasProject reports whether any scope grants access to a project.
func (k APIKey) HasProject(projectID uint) bool {
	for _, scope := range k.Scopes {
		if (scope.ProjectID == 0 || scope.ProjectID == projectID) && len(scope.Permissions) > 0 {
			return true
		}
	}
	return false
}

// Covers reports whether every requested operation is already granted to the key.
func (k APIKey) Covers(scopes []KeyScope) bool {
	for _, scope := range scopes {
		for _, permission := range scope.Permissions {
			if !k.Allows(scope.ProjectID, permission) {
				return false
			}
		}
	}
	return true
}

// HasPermission reports whether any project scope contains an operation.
func (k APIKey) HasPermission(permission Permission) bool {
	for _, scope := range k.Scopes {
		if slices.Contains(scope.Permissions, permission) {
			return true
		}
	}
	return false
}
