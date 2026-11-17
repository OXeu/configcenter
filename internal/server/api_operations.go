package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/OXeu/configcenter/internal/auth"
	"github.com/OXeu/configcenter/internal/store"
)

type apiOperation struct {
	Name           string
	Description    string
	Method         string
	Path           string
	Permissions    []store.Permission
	Global         bool
	Agent          bool
	Handler        gin.HandlerFunc
	ProjectHandler func(*gin.Context, store.Project)
	Arguments      []mcpArgument
}

type mcpArgument struct {
	Name     string
	Location string
	Target   string
	Required bool
	Schema   map[string]any
}

func argument(name, location string, required bool) mcpArgument {
	return mcpArgument{Name: name, Target: name, Location: location, Required: required, Schema: map[string]any{"type": "string"}}
}

func typedArgument(name, kind string, required bool) mcpArgument {
	arg := argument(name, "body", required)
	arg.Schema = map[string]any{"type": kind}
	return arg
}

func enumArgument(name string, values ...string) mcpArgument {
	arg := argument(name, "body", true)
	arg.Schema["enum"] = values
	return arg
}

func (s *Server) apiOperations() []apiOperation {
	project := argument("project", "path", true)
	env := argument("environment", "path", true)
	env.Target = "env"
	key := argument("key", "path", true)
	id := mcpArgument{Name: "id", Target: "id", Location: "path", Required: true, Schema: map[string]any{"type": "integer", "minimum": 1}}
	name := argument("name", "body", true)
	description := argument("description", "body", false)
	slug := argument("slug", "body", true)
	prefix := argument("prefix", "query", false)
	newKey := argument("new_key", "body", false)
	newKey.Target = "key"
	keys := argument("keys", "query", false)
	keys.Schema = map[string]any{"type": "array", "minItems": 1, "maxItems": 500, "items": map[string]any{"type": "string"}}
	batch := typedArgument("items", "array", true)
	batch.Schema["minItems"], batch.Schema["maxItems"] = 1, 500
	batch.Schema["items"] = schema(map[string]any{
		"key":         map[string]any{"type": "string"},
		"value":       map[string]any{"type": "string"},
		"description": map[string]any{"type": "string"},
	}, "key", "value")
	scopes := typedArgument("scopes", "array", true)
	permissions := make([]string, 0, len(store.KeyPermissions()))
	for _, permission := range store.KeyPermissions() {
		permissions = append(permissions, string(permission))
	}
	scopes.Schema["maxItems"] = 100
	scopes.Schema["items"] = schema(map[string]any{
		"project": map[string]any{"type": "string", "description": "Project slug, or * for all projects and global operations."},
		"permissions": map[string]any{
			"type": "array", "minItems": 1, "maxItems": len(permissions),
			"items": map[string]any{"type": "string", "enum": permissions},
		},
	}, "project", "permissions")
	projectPath := "/projects/:project"
	envs := projectPath + "/environments"
	items := envs + "/:env/items"
	proposals := projectPath + "/proposals"
	return []apiOperation{
		{Name: "get_identity", Description: "Read the current user or API key identity.", Method: http.MethodGet, Path: "/me", Handler: s.apiMe},
		{Name: "get_key_scopes", Description: "List available projects and scope permissions for API keys.", Method: http.MethodGet, Path: "/key-scopes", Handler: s.apiKeyScopes},
		{Name: "list_keys", Description: "List API key metadata within your management permissions.", Method: http.MethodGet, Path: "/keys", Permissions: []store.Permission{store.KeysRead}, Handler: s.apiListKeys, Arguments: []mcpArgument{argument("project", "query", false)}},
		{Name: "create_key", Description: "Create a scoped API key. The token is returned only once; grants cannot exceed your permissions.", Method: http.MethodPost, Path: "/keys", Permissions: []store.Permission{store.KeysWrite}, Handler: s.apiCreateKey, Arguments: []mcpArgument{name, scopes, argument("expires_at", "body", false)}},
		{Name: "update_key", Description: "Replace an API key's name and scopes without changing its token. Empty scopes disable access.", Method: http.MethodPatch, Path: "/keys/:id", Permissions: []store.Permission{store.KeysWrite}, Handler: s.apiUpdateKey, Arguments: []mcpArgument{id, name, scopes}},
		{Name: "delete_key", Description: "Permanently revoke an API key.", Method: http.MethodDelete, Path: "/keys/:id", Permissions: []store.Permission{store.KeysWrite}, Handler: s.apiDeleteKey, Arguments: []mcpArgument{id}},
		{Name: "rotate_key", Description: "Replace an API key token, immediately invalidating its previous token.", Method: http.MethodPost, Path: "/keys/:id/rotate", Permissions: []store.Permission{store.KeysWrite}, Handler: s.apiRotateKey, Arguments: []mcpArgument{id}},
		{Name: "list_projects", Description: "List projects you may read.", Method: http.MethodGet, Path: "/projects", Permissions: []store.Permission{store.ProjectsRead}, Handler: s.apiListProjects, Arguments: []mcpArgument{argument("view", "query", false)}},
		{Name: "create_project", Description: "Create a project and its production environment. API keys require wildcard projects:write.", Method: http.MethodPost, Path: "/projects", Permissions: []store.Permission{store.ProjectsWrite}, Global: true, Handler: s.apiCreateProject, Arguments: []mcpArgument{slug, name, description}},
		{Name: "update_project", Description: "Update a project's name and description.", Method: http.MethodPatch, Path: projectPath, Permissions: []store.Permission{store.ProjectsWrite}, ProjectHandler: s.apiUpdateProject, Arguments: []mcpArgument{project, name, description}},
		{Name: "delete_project", Description: "Delete a project and its environments, configuration and proposals.", Method: http.MethodDelete, Path: projectPath, Permissions: []store.Permission{store.ProjectsDelete}, ProjectHandler: s.apiDeleteProject, Arguments: []mcpArgument{project}},
		{Name: "list_environments", Description: "List a project's environments.", Method: http.MethodGet, Path: envs, Permissions: []store.Permission{store.EnvironmentsRead}, ProjectHandler: s.apiListEnvironments, Arguments: []mcpArgument{project}},
		{Name: "create_environment", Description: "Create an environment in a project.", Method: http.MethodPost, Path: envs, Permissions: []store.Permission{store.EnvironmentsWrite}, ProjectHandler: s.apiCreateEnvironment, Arguments: []mcpArgument{project, slug, name}},
		{Name: "delete_environment", Description: "Delete an environment and its configuration.", Method: http.MethodDelete, Path: envs + "/:env", Permissions: []store.Permission{store.EnvironmentsDelete}, ProjectHandler: s.apiDeleteEnvironment, Arguments: []mcpArgument{project, env}},
		{Name: "list_config", Description: "Read configuration items, optionally filtered by keys or a key prefix.", Method: http.MethodGet, Path: items, Permissions: []store.Permission{store.ConfigRead}, ProjectHandler: s.apiListItems, Arguments: []mcpArgument{project, env, prefix, keys}},
		{Name: "get_config", Description: "Read one configuration item.", Method: http.MethodGet, Path: items + "/:key", Permissions: []store.Permission{store.ConfigRead}, ProjectHandler: s.apiGetItem, Arguments: []mcpArgument{project, env, key}},
		{Name: "set_config", Description: "Create or update a configuration item immediately; optionally rename its key.", Method: http.MethodPut, Path: items + "/:key", Permissions: []store.Permission{store.ConfigWrite}, ProjectHandler: s.apiPutItem, Arguments: []mcpArgument{project, env, key, argument("value", "body", true), description, newKey}},
		{Name: "set_configs", Description: "Atomically create or update multiple configuration items.", Method: http.MethodPut, Path: items, Permissions: []store.Permission{store.ConfigWrite}, ProjectHandler: s.apiPutItems, Arguments: []mcpArgument{project, env, batch}},
		{Name: "delete_config", Description: "Delete a configuration item immediately.", Method: http.MethodDelete, Path: items + "/:key", Permissions: []store.Permission{store.ConfigDelete}, ProjectHandler: s.apiDeleteItem, Arguments: []mcpArgument{project, env, key}},
		{Name: "list_proposals", Description: "List project change proposals, optionally filtered by status.", Method: http.MethodGet, Path: proposals, Permissions: []store.Permission{store.ProposalsRead}, ProjectHandler: s.apiListProposals, Arguments: []mcpArgument{project, argument("status", "query", false)}},
		{Name: "propose_config", Description: "Submit a configuration change for review. Set actions require a value.", Method: http.MethodPost, Path: proposals, Permissions: []store.Permission{store.ProposalsWrite}, ProjectHandler: s.apiCreateProposal, Arguments: []mcpArgument{project, argument("environment", "body", true), argument("key", "body", true), enumArgument("action", store.ActionSet, store.ActionDelete), argument("value", "body", false), argument("reason", "body", false)}},
		{Name: "review_proposal", Description: "Approve and apply, or reject, a pending proposal.", Method: http.MethodPost, Path: proposals + "/:id/review", Permissions: []store.Permission{store.ProposalsReview}, ProjectHandler: s.apiReviewProposal, Arguments: []mcpArgument{project, id, typedArgument("approve", "boolean", true)}},
		{Name: "list_users", Description: "List all users. Requires global users:read.", Method: http.MethodGet, Path: "/users", Permissions: []store.Permission{store.UsersRead}, Global: true, Handler: s.apiListUsers},
		{Name: "update_user_role", Description: "Change a user's role. Requires global users:write; you cannot demote your own account.", Method: http.MethodPatch, Path: "/users/:id", Permissions: []store.Permission{store.UsersWrite}, Global: true, Handler: s.apiSetRole, Arguments: []mcpArgument{id, enumArgument("role", string(store.RoleUser), string(store.RoleAdmin))}},
		{Name: "delete_user", Description: "Delete a user and their credentials, transferring projects to your account. Requires global users:delete; you cannot delete yourself.", Method: http.MethodDelete, Path: "/users/:id", Permissions: []store.Permission{store.UsersDelete}, Global: true, Handler: s.apiDeleteUser, Arguments: []mcpArgument{id}},
		{Name: "get_settings", Description: "Read system settings. Requires global settings:read.", Method: http.MethodGet, Path: "/settings", Permissions: []store.Permission{store.SettingsRead}, Global: true, Handler: s.apiSettings},
		{Name: "update_settings", Description: "Update system registration settings. Requires global settings:write.", Method: http.MethodPut, Path: "/settings", Permissions: []store.Permission{store.SettingsWrite}, Global: true, Handler: s.apiSaveSettings, Arguments: []mcpArgument{typedArgument("allow_registration", "boolean", true)}},
		{Name: "get_project", Description: "Read project metadata and environments using an API key.", Method: http.MethodGet, Path: "/agent/project", Permissions: []store.Permission{store.ProjectsRead, store.EnvironmentsRead}, Agent: true, Handler: s.apiProgrammaticProject, Arguments: []mcpArgument{argument("project", "query", true)}},
		{Name: "list_values", Description: "Read environment configuration using the SDK API.", Method: http.MethodGet, Path: "/agent/environments/:env/values", Permissions: []store.Permission{store.ConfigRead}, Agent: true, Handler: s.apiProgrammaticItems, Arguments: []mcpArgument{argument("project", "query", true), env, prefix, keys}},
	}
}

func (op apiOperation) allowed(id auth.Identity) bool {
	if id.APIKey == nil {
		if id.User == nil || (op.Agent && !id.MCP) {
			return false
		}
		for _, permission := range op.Permissions {
			if !id.Allows(0, permission) {
				return false
			}
		}
		return true
	}
	if len(op.Permissions) == 0 {
		return true
	}
	for _, scope := range id.APIKey.Scopes {
		if op.Global && scope.ProjectID != 0 {
			continue
		}
		allowed := true
		for _, permission := range op.Permissions {
			allowed = allowed && id.Allows(scope.ProjectID, permission)
		}
		if allowed {
			return true
		}
	}
	return false
}

func (op apiOperation) authorize(c *gin.Context) {
	id, _ := auth.FromContext(c.Request.Context())
	if !op.allowed(id) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient api key scope"})
		return
	}
	c.Next()
}
