package permission

// Mode selects the approval policy. Its zero value requires manual approval.
type Mode int32

const (
	ModeManual Mode = iota
	ModeAuto
	ModeFull
)
