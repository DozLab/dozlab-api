package handlers

import "testing"

// Every status the handler writes must satisfy sessions_status_check.
var allowedSessionStatuses = map[string]bool{
	"pending": true, "running": true, "completed": true, "failed": true, "expired": true,
}

func TestConvertPhaseToDBStatus(t *testing.T) {
	tests := []struct {
		phase string
		want  string
	}{
		{"Pending", SessionStatusPending},
		{"Creating", SessionStatusPending},
		{"Running", SessionStatusRunning},
		{"Failed", SessionStatusFailed},
		{"Terminated", SessionStatusCompleted},
		{"Terminating", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := convertPhaseToDBStatus(tt.phase)
		if got != tt.want {
			t.Errorf("convertPhaseToDBStatus(%q) = %q, want %q", tt.phase, got, tt.want)
		}
		if got != "" && !allowedSessionStatuses[got] {
			t.Errorf("convertPhaseToDBStatus(%q) = %q, not allowed by sessions_status_check", tt.phase, got)
		}
	}
}

func TestSessionStatusConstantsAllowed(t *testing.T) {
	for _, s := range []string{SessionStatusPending, SessionStatusRunning, SessionStatusCompleted, SessionStatusFailed, SessionStatusExpired} {
		if !allowedSessionStatuses[s] {
			t.Errorf("status %q not allowed by sessions_status_check", s)
		}
	}
}

func TestNeedsK8sDelete(t *testing.T) {
	for s, want := range map[string]bool{
		SessionStatusPending: true, SessionStatusRunning: true,
		SessionStatusFailed: true, SessionStatusExpired: true,
		SessionStatusCompleted: false,
	} {
		if got := needsK8sDelete(s); got != want {
			t.Errorf("needsK8sDelete(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestIsActiveSessionStatus(t *testing.T) {
	for s, want := range map[string]bool{
		SessionStatusPending: true, SessionStatusRunning: true,
		SessionStatusCompleted: false, SessionStatusFailed: false, SessionStatusExpired: false,
	} {
		if got := isActiveSessionStatus(s); got != want {
			t.Errorf("isActiveSessionStatus(%q) = %v, want %v", s, got, want)
		}
	}
}
