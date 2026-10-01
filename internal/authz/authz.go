// Package authz is DozLab's access control: which role may do which action on which kind of
// data. Every rule is in the table below, and routes and handlers ask Can instead of comparing
// role names.
//
// A permission is "<data type>:<action>". Permissions that act on one record (a lab, a session)
// cover the caller's own records; the matching "manage_any" permission lifts that limit.
// Scoping by team or organisation isn't built: the schema has no such concept yet
// (docs/decision.md, "Enterprise readiness").
package authz

// Role is a user's role (models.User.Role).
type Role string

const (
	Admin      Role = "admin"
	Instructor Role = "instructor"
	Student    Role = "student"
)

// Permission names one action on one kind of data.
type Permission string

const (
	LabsRead            Permission = "labs:read"             // published labs
	LabsReadUnpublished Permission = "labs:read_unpublished" // any lab, published or not
	LabsCreate          Permission = "labs:create"
	LabsUpdate          Permission = "labs:update" // own labs
	LabsDelete          Permission = "labs:delete" // own labs
	LabsManageAny       Permission = "labs:manage_any"
	LabsEstimate        Permission = "labs:estimate" // what a VM of the lab reserves and stores

	LabSpecsRead  Permission = "lab_specs:read"
	LabSpecsWrite Permission = "lab_specs:write"

	SessionsCreate     Permission = "sessions:create"
	SessionsSetOptions Permission = "sessions:set_options"
	SessionsRead       Permission = "sessions:read"   // own sessions
	SessionsDelete     Permission = "sessions:delete" // own sessions
	SessionsManageAny  Permission = "sessions:manage_any"

	ProfileRead   Permission = "profile:read"
	ProfileUpdate Permission = "profile:update"
	ProgressRead  Permission = "progress:read" // own progress

	HostCheckRun      Permission = "host_check:run"
	NotificationsSend Permission = "notifications:send"
	ServiceStatsRead  Permission = "service_stats:read"

	UsersList         Permission = "users:list"
	UsersUpdateRole   Permission = "users:update_role"
	UsersUpdateStatus Permission = "users:update_status"

	AuditRead Permission = "audit:read"
)

// What every logged-in user may do.
var everyone = []Permission{
	LabsRead, LabSpecsRead,
	SessionsCreate, SessionsRead, SessionsDelete,
	ProfileRead, ProfileUpdate, ProgressRead,
	HostCheckRun, NotificationsSend, ServiceStatsRead,
}

// What an instructor may do on top of that: create labs and manage their own, and set
// session options (docs/decision.md, "What phase 1 needs in the API").
var instructorOnly = []Permission{
	LabsCreate, LabsUpdate, LabsDelete, LabsEstimate, LabSpecsWrite, SessionsSetOptions,
}

// What only an admin may do: act on other people's records, manage users, read the audit log.
var adminOnly = []Permission{
	LabsReadUnpublished, LabsManageAny, SessionsManageAny,
	UsersList, UsersUpdateRole, UsersUpdateStatus,
	AuditRead,
}

var grants = map[Role]map[Permission]bool{
	Student:    set(everyone),
	Instructor: set(everyone, instructorOnly),
	Admin:      set(everyone, instructorOnly, adminOnly),
}

func set(lists ...[]Permission) map[Permission]bool {
	m := map[Permission]bool{}
	for _, list := range lists {
		for _, p := range list {
			m[p] = true
		}
	}
	return m
}

// Can reports whether the role has the permission. An unknown or empty role has none.
func Can(role Role, p Permission) bool {
	return grants[role][p]
}

// Permissions returns every permission the role has, for showing a role's rights.
func Permissions(role Role) []Permission {
	var out []Permission
	for _, list := range [][]Permission{everyone, instructorOnly, adminOnly} {
		for _, p := range list {
			if Can(role, p) {
				out = append(out, p)
			}
		}
	}
	return out
}
