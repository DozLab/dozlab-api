package authz

import "testing"

func TestCan(t *testing.T) {
	tests := []struct {
		role Role
		perm Permission
		want bool
	}{
		// Everyone
		{Student, LabsRead, true},
		{Student, SessionsCreate, true},
		{Student, SessionsRead, true},
		{Student, SessionsDelete, true},
		{Student, ProfileUpdate, true},
		// Instructors and admins only
		{Student, LabsCreate, false},
		{Student, LabsUpdate, false},
		{Student, LabsDelete, false},
		{Student, LabSpecsWrite, false},
		{Student, SessionsSetOptions, false},
		{Instructor, LabsCreate, true},
		{Student, LabsEstimate, false},
		{Student, SessionsUsage, false},
		{Instructor, SessionsUsage, true},
		{Admin, SessionsUsage, true},
		{Instructor, LabsEstimate, true},
		{Admin, LabsEstimate, true},
		{Instructor, LabsUpdate, true},
		{Instructor, LabsDelete, true},
		{Instructor, LabSpecsWrite, true},
		{Instructor, SessionsSetOptions, true},
		// Admins only
		{Student, LabsManageAny, false},
		{Instructor, LabsManageAny, false},
		{Instructor, LabsReadUnpublished, false},
		{Instructor, SessionsManageAny, false},
		{Instructor, UsersList, false},
		{Instructor, UsersUpdateRole, false},
		{Instructor, UsersUpdateStatus, false},
		{Instructor, AuditRead, false},
		{Student, AuditRead, false},
		{Admin, LabsManageAny, true},
		{Admin, LabsReadUnpublished, true},
		{Admin, SessionsManageAny, true},
		{Admin, UsersUpdateRole, true},
		{Admin, AuditRead, true},
		{Admin, LabsCreate, true},
		{Admin, SessionsSetOptions, true},
		// Unknown and empty roles have nothing
		{Role("tutor"), LabsRead, false},
		{Role(""), LabsRead, false},
		{Role(""), SessionsCreate, false},
		// Unknown permission
		{Admin, Permission("labs:explode"), false},
	}
	for _, tt := range tests {
		if got := Can(tt.role, tt.perm); got != tt.want {
			t.Errorf("Can(%q, %q) = %v, want %v", tt.role, tt.perm, got, tt.want)
		}
	}
}

// A role higher up must have everything the role below it has.
func TestRolesAreNested(t *testing.T) {
	for _, p := range Permissions(Student) {
		if !Can(Instructor, p) {
			t.Errorf("instructor lacks the student permission %q", p)
		}
	}
	for _, p := range Permissions(Instructor) {
		if !Can(Admin, p) {
			t.Errorf("admin lacks the instructor permission %q", p)
		}
	}
	if len(Permissions(Role("tutor"))) != 0 {
		t.Error("an unknown role must have no permissions")
	}
}
